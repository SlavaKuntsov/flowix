import concurrent.futures
import json
import logging
import os
import subprocess
import tempfile
import time

import pika  # type: ignore[import-untyped]
from pika.exceptions import AMQPConnectionError  # type: ignore[import-untyped]
import requests
from minio import Minio

log = logging.getLogger(__name__)
logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")

RABBITMQ_URL = os.getenv("RABBITMQ_URL", "amqp://flowix:flowix@rabbitmq:5672/")
MINIO_ENDPOINT = os.getenv("MINIO_ENDPOINT", "minio:9000")
MINIO_ACCESS = os.getenv("MINIO_ACCESS_KEY", "minioadmin")
MINIO_SECRET = os.getenv("MINIO_SECRET_KEY", "minioadmin")
MINIO_SECURE = os.getenv("MINIO_SECURE", "false").lower() == "true"
BUCKET = os.getenv("VIDEO_STORAGE_BUCKET", "videos")
METADATA_URL = os.getenv("METADATA_URL", "http://metadata:8002")
INTERNAL_TOKEN = os.getenv("INTERNAL_TOKEN", "")
QUEUE = "video.uploaded"

# rendition spec: (quality, width, height, video_bitrate_k, audio_bitrate)
RENDITIONS_SPEC = [
    ("360p", 640, 360, 800, "96k"),
    ("720p", 1280, 720, 2500, "128k"),
    ("1080p", 1920, 1080, 5000, "192k"),
]


def get_minio():
    return Minio(
        MINIO_ENDPOINT,
        access_key=MINIO_ACCESS,
        secret_key=MINIO_SECRET,
        secure=MINIO_SECURE,
    )


def update_status(video_id: str, status: str, renditions=None, thumbnail_s3_key: str | None = None):
    url = f"{METADATA_URL}/internal/videos/{video_id}/status"
    payload: dict = {"status": status}
    if renditions:
        payload["renditions"] = renditions
    if thumbnail_s3_key:
        payload["thumbnail_s3_key"] = thumbnail_s3_key
    headers = {}
    if INTERNAL_TOKEN:
        headers["X-Internal-Token"] = INTERNAL_TOKEN
    try:
        r = requests.patch(url, json=payload, headers=headers, timeout=5)
        r.raise_for_status()
        log.info("updated %s -> %s", video_id, status)
    except Exception as e:
        log.error("metadata update %s failed: %s", video_id, e)


def probe_video(path: str) -> dict | None:
    """ffprobe fps/duration — non-fatal, falls back to defaults."""
    try:
        out = subprocess.run(
            [
                "ffprobe",
                "-v",
                "error",
                "-select_streams",
                "v:0",
                "-show_entries",
                "stream=avg_frame_rate,r_frame_rate,width,height",
                "-show_entries",
                "format=duration",
                "-of",
                "json",
                path,
            ],
            capture_output=True,
            text=True,
            timeout=15,
            check=True,
        )
        data = json.loads(out.stdout)
        log.info("probe %s: %s", path, data)
        return data
    except Exception as e:
        log.warning("ffprobe failed for %s: %s (using defaults)", path, e)
        return None


def transcode_one(
    input_path: str,
    output_path: str,
    width: int,
    height: int,
    bitrate_k: int,
    abitrate: str,
):
    """Single rendition with aligned GOP for JIT HLS (phase 4 spec)."""
    # Common aligned params: -r 30 -g 60 -keyint_min 60 -sc_threshold 0 -force_key_frames expr:gte(t,n_forced*2)
    # + high profile, faststart for mp4
    vf = f"scale=-2:{height}:flags=lanczos"
    cmd = [
        "ffmpeg",
        "-y",
        "-i",
        input_path,
        "-c:v",
        "libx264",
        "-profile:v",
        "high",
        "-pix_fmt",
        "yuv420p",
        "-r",
        "30",
        "-g",
        "60",
        "-keyint_min",
        "60",
        "-sc_threshold",
        "0",
        "-force_key_frames",
        "expr:gte(t,n_forced*2)",
        "-vf",
        vf,
        "-b:v",
        f"{bitrate_k}k",
        "-maxrate",
        f"{int(bitrate_k * 1.07)}k",
        "-bufsize",
        f"{bitrate_k * 2}k",
        "-c:a",
        "aac",
        "-b:a",
        abitrate,
        "-movflags",
        "+faststart",
        output_path,
    ]
    log.info("ffmpeg %dx%d %dk: %s", width, height, bitrate_k, " ".join(cmd))
    subprocess.run(cmd, check=True, capture_output=True, timeout=300)


def transcode_thumbnail(input_path: str, output_path: str):
    """Single thumbnail at 1s — non-fatal."""
    try:
        cmd = [
            "ffmpeg",
            "-y",
            "-ss",
            "1",
            "-i",
            input_path,
            "-vframes",
            "1",
            "-vf",
            "scale=-2:360",
            output_path,
        ]
        subprocess.run(cmd, check=True, capture_output=True, timeout=30)
        log.info("thumbnail %s", output_path)
    except Exception as e:
        log.warning("thumbnail failed: %s", e)


def process_message(body: bytes):
    try:
        data = json.loads(body)
        video_id = data["video_id"]
        s3_key = data["s3_key"]
        owner_id = data.get("owner_id", "")
        log.info("processing video_id=%s s3_key=%s owner=%s", video_id, s3_key, owner_id)
    except Exception as e:
        log.error("invalid message: %s %s", body, e)
        return

    update_status(video_id, "processing")

    # Real pipeline: download raw → ffprobe → 3× ffmpeg parallel → upload → PATCH ready
    try:
        mc = get_minio()
        # ensure object exists (stat will raise if missing)
        mc.stat_object(BUCKET, s3_key)

        with tempfile.TemporaryDirectory() as tmp:
            raw_path = os.path.join(tmp, "original.mp4")
            log.info("downloading s3://%s/%s -> %s", BUCKET, s3_key, raw_path)
            # fget_object is preferred over copy for local ffmpeg input
            try:
                mc.fget_object(BUCKET, s3_key, raw_path)
            except AttributeError:
                # fallback for fake/mocked clients without fget_object (tests)
                log.warning("fget_object not available, falling back to copy test stub")
                raise

            _ = probe_video(raw_path)

            # prepare output paths
            outputs: dict[str, str] = {}
            for q, w, h, br, abr in RENDITIONS_SPEC:
                outputs[q] = os.path.join(tmp, f"{q}.mp4")

            # 3× ffmpeg in parallel (I/O bound + CPU, ThreadPool is enough; CPU-heavy but keeps simplicity)
            errors: list[Exception] = []

            def _run(spec):
                q, w, h, br, abr = spec
                try:
                    transcode_one(raw_path, outputs[q], w, h, br, abr)
                except Exception as e:
                    log.error("transcode %s failed: %s", q, e)
                    errors.append(e)
                    raise

            with concurrent.futures.ThreadPoolExecutor(max_workers=3) as ex:
                futs = [ex.submit(_run, spec) for spec in RENDITIONS_SPEC]
                # wait and collect errors
                for f in concurrent.futures.as_completed(futs):
                    try:
                        f.result()
                    except Exception:
                        pass

            if errors:
                raise errors[0]

            # thumbnail (non-blocking for ready, but upload if exists)
            thumb_key: str | None = None
            thumb_path = os.path.join(tmp, "thumb.jpg")
            transcode_thumbnail(raw_path, thumb_path)
            if os.path.exists(thumb_path):
                try:
                    thumb_key = f"thumbnails/{video_id}/thumb.jpg"
                    mc.fput_object(BUCKET, thumb_key, thumb_path, content_type="image/jpeg")
                    log.info("uploaded thumbnail %s", thumb_key)
                except Exception as e:
                    log.warning("thumbnail upload failed: %s", e)
                    thumb_key = None

            # upload renditions
            renditions = []
            for q, w, h, br, abr in RENDITIONS_SPEC:
                rk = f"renditions/{video_id}/{q}.mp4"
                log.info("uploading %s -> s3://%s/%s", outputs[q], BUCKET, rk)
                mc.fput_object(BUCKET, rk, outputs[q], content_type="video/mp4")
                renditions.append(
                    {"quality": q, "bitrate": br, "width": w, "height": h, "s3_key": rk}
                )
                log.info("rendition %s -> %s", q, rk)

    except Exception as e:
        log.error("transcode pipeline failed for %s: %s", video_id, e)
        update_status(video_id, "failed")
        return

    update_status(video_id, "ready", renditions, thumb_key)


def main():
    params = pika.URLParameters(RABBITMQ_URL)
    while True:
        try:
            conn = pika.BlockingConnection(params)
            channel = conn.channel()
            channel.queue_declare(queue=QUEUE, durable=True)
            channel.basic_qos(prefetch_count=1)

            def on_message(ch, method, properties, body):
                try:
                    process_message(body)
                    ch.basic_ack(delivery_tag=method.delivery_tag)
                except Exception as e:
                    log.exception("process failed: %s", e)
                    ch.basic_nack(delivery_tag=method.delivery_tag, requeue=False)

            channel.basic_consume(queue=QUEUE, on_message_callback=on_message)
            log.info("consumer ready, waiting for %s", QUEUE)
            channel.start_consuming()
        except AMQPConnectionError as e:
            log.error("rabbitmq connection failed: %s, retry in 5s", e)
            time.sleep(5)
        except KeyboardInterrupt:
            break
        except Exception as e:
            log.exception("consumer error: %s", e)
            time.sleep(5)


if __name__ == "__main__":
    main()
