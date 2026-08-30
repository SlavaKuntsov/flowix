import json
import logging
import os
import shutil
import signal
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
FFMPEG_THREADS = os.getenv("FFMPEG_THREADS", "2")
FFMPEG_PRESET = os.getenv("FFMPEG_PRESET", "veryfast")

# rendition spec: (quality, width, height, video_bitrate_k)
RENDITIONS_SPEC = [
    ("360p", 640, 360, 800),
    ("720p", 1280, 720, 2500),
    ("1080p", 1920, 1080, 5000),
]
# One shared audio bitrate — see encode_audio() for why it must not vary per rendition.
AUDIO_BITRATE = "128k"


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


def _parse_fps(s: str | None) -> float | None:
    if not s or "/" not in s:
        return None
    try:
        n, d = s.split("/")
        fps = float(n) / float(d) if float(d) != 0 else None
        if fps and 1 <= fps <= 120:
            return fps
    except Exception:
        pass
    return None


def _fps_from_probe(probe: dict | None) -> int:
    """Phase 10: preserve original fps if ≤30, cap >30 to 30, fallback 30."""
    if probe:
        try:
            streams = probe.get("streams") or []
            if streams:
                s = streams[0]
                for k in ("avg_frame_rate", "r_frame_rate"):
                    fps = _parse_fps(s.get(k))
                    if fps:
                        if fps <= 30:
                            return int(round(fps)) if fps >= 1 else 30
                        return 30
        except Exception:
            pass
    return 30


def encode_audio(input_path: str, output_path: str):
    """Encode the audio track once, to be stream-copied into every rendition.

    Encoding audio separately per rendition gives each variant its own AAC
    stream with its own encoder priming and padding. Segments then stop being
    interchangeable: switching quality mid-playback hands the decoder a
    different audio timeline, which is audible as a click plus a small A/V
    resync. A single shared track keeps the audio bytes identical across all
    three renditions, so a switch only changes the video.
    """
    cmd = [
        "ffmpeg",
        "-y",
        "-i",
        input_path,
        "-vn",
        "-c:a",
        "aac",
        "-b:a",
        AUDIO_BITRATE,
        "-ar",
        "48000",
        "-ac",
        "2",
        "-movflags",
        "+faststart",
        output_path,
    ]
    log.info("ffmpeg audio %s: %s", AUDIO_BITRATE, " ".join(cmd))
    subprocess.run(cmd, check=True, capture_output=True, timeout=300)


def transcode_one(
    input_path: str,
    audio_path: str | None,
    output_path: str,
    width: int,
    height: int,
    bitrate_k: int,
    fps: int = 30,
):
    """Single rendition with aligned GOP for JIT HLS (phase 4 spec, Phase 10 limits)."""
    vf = f"scale=-2:{height}:flags=lanczos"
    # Phase 10: limit threads and preset to avoid OOM/CPU starvation on large files
    preset = (
        FFMPEG_PRESET
        if FFMPEG_PRESET
        in ("ultrafast", "superfast", "veryfast", "faster", "fast", "medium", "slow")
        else "veryfast"
    )
    threads = FFMPEG_THREADS if FFMPEG_THREADS.isdigit() and 1 <= int(FFMPEG_THREADS) <= 8 else "2"
    cmd = ["ffmpeg", "-y", "-threads", threads, "-i", input_path]
    if audio_path:
        cmd += ["-i", audio_path, "-map", "0:v:0", "-map", "1:a:0"]
    else:
        cmd += ["-an"]
    cmd += [
        "-c:v",
        "libx264",
        "-profile:v",
        "high",
        "-pix_fmt",
        "yuv420p",
        "-preset",
        preset,
        "-r",
        str(fps),
        "-g",
        str(fps * 2),
        "-keyint_min",
        str(fps * 2),
        "-sc_threshold",
        "0",
        "-force_key_frames",
        "expr:gte(t,n_forced*2)",
        "-vf",
        vf,
        "-b:v",
        f"{bitrate_k}k",
        "-maxrate",
        f"{int(bitrate_k * 1.10)}k",
        "-bufsize",
        f"{bitrate_k * 2}k",
    ]
    if audio_path:
        cmd += ["-c:a", "copy"]
    cmd += ["-movflags", "+faststart", output_path]
    log.info(
        "ffmpeg %dx%d %dk %dfps threads=%s preset=%s: %s",
        width,
        height,
        bitrate_k,
        fps,
        threads,
        preset,
        " ".join(cmd),
    )
    subprocess.run(cmd, check=True, capture_output=True, timeout=900)


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

    # Idempotency: if already ready, don't flip back to processing (redelivery after ack timeout)
    cur = _get_status(video_id)
    if cur == "ready":
        log.info("video %s already ready, skipping re-transcode (idempotent ack)", video_id)
        return
    update_status(video_id, "processing")

    # Real pipeline: download raw → ffprobe → 3× ffmpeg parallel → upload → PATCH ready
    try:
        mc = get_minio()
        # ensure object exists (stat will raise if missing)
        mc.stat_object(BUCKET, s3_key)

        with tempfile.TemporaryDirectory() as tmp:
            # Phase 10: disk check before download (avoid filling /tmp on large files)
            try:
                free = shutil.disk_usage(tmp).free
                # need at least 2× raw size free (raw + renditions); conservative 500MB min
                if free < 500 * 1024 * 1024:
                    log.error(
                        "low disk space in %s: free=%d bytes, aborting %s", tmp, free, video_id
                    )
                    raise RuntimeError(f"low disk space: {free} bytes free")
            except Exception as e:
                if "low disk space" in str(e):
                    raise
                log.warning("disk_usage check failed: %s", e)

            raw_path = os.path.join(tmp, "original.mp4")
            log.info("downloading s3://%s/%s -> %s", BUCKET, s3_key, raw_path)
            try:
                mc.fget_object(BUCKET, s3_key, raw_path)
            except AttributeError:
                log.warning("fget_object not available, falling back to copy test stub")
                raise

            probe = probe_video(raw_path)
            fps = _fps_from_probe(probe)

            # prepare output paths
            outputs: dict[str, str] = {}
            for q, w, h, br in RENDITIONS_SPEC:
                outputs[q] = os.path.join(tmp, f"{q}.mp4")

            # Shared audio track for all renditions (see encode_audio). Sources
            # without a usable audio stream stay video-only rather than failing.
            audio_path: str | None = None
            try:
                candidate = os.path.join(tmp, "audio.m4a")
                encode_audio(raw_path, candidate)
                audio_path = candidate
            except Exception as e:
                log.warning("no usable audio track (%s), renditions will be video-only", e)

            # Phase 10: sequential transcode to limit CPU/RAM (3× parallel caused OOM)
            for q, w, h, br in RENDITIONS_SPEC:
                log.info("transcoding %s sequentially (fps=%d)", q, fps)
                transcode_one(raw_path, audio_path, outputs[q], w, h, br, fps=fps)

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
            for q, w, h, br in RENDITIONS_SPEC:
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


_shutdown = False


def _handle_sigterm(signum, frame):
    global _shutdown
    log.info("received signal %s, graceful shutdown", signum)
    _shutdown = True


def _get_status(video_id: str) -> str | None:
    """Fetch current status to avoid reprocessing already-ready videos (idempotency)."""
    if not INTERNAL_TOKEN:
        return None
    try:
        url = f"{METADATA_URL}/internal/videos/{video_id}"
        headers = {"X-Internal-Token": INTERNAL_TOKEN}
        r = requests.get(url, headers=headers, timeout=5)
        if r.ok:
            return r.json().get("status")
    except Exception:
        pass
    return None


def main():
    signal.signal(signal.SIGTERM, _handle_sigterm)
    signal.signal(signal.SIGINT, _handle_sigterm)
    params = pika.URLParameters(RABBITMQ_URL)
    # Phase 10 fix: long transcoding (2GB ~5min) blocks heartbeat thread → broker closes connection (104).
    # Default heartbeat 60s is too short; set to 600s (10min) to cover 5-6GB files. For larger files Phase 11 will use chunked.
    params.heartbeat = 600
    params.blocked_connection_timeout = 300
    while not _shutdown:
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
            log.info(
                "consumer ready, waiting for %s (threads=%s preset=%s)",
                QUEUE,
                FFMPEG_THREADS,
                FFMPEG_PRESET,
            )
            channel.start_consuming()
        except AMQPConnectionError as e:
            if _shutdown:
                break
            log.error("rabbitmq connection failed: %s, retry in 5s", e)
            time.sleep(5)
        except KeyboardInterrupt:
            break
        except SystemExit:
            break
        except Exception as e:
            if _shutdown:
                break
            log.exception("consumer error: %s", e)
            time.sleep(5)


if __name__ == "__main__":
    main()
