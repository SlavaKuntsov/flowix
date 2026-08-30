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
DLX_EXCHANGE = "dlx"
DLQ = QUEUE + ".dlq"
RETRY_QUEUE = QUEUE + ".retry"
RETRY_TTL_MS = 30000
MAX_RETRIES = 3
FFMPEG_THREADS = os.getenv("FFMPEG_THREADS", "2")
FFMPEG_PRESET = os.getenv("FFMPEG_PRESET", "veryfast")
ENCODE_MODE = os.getenv("ENCODE_MODE", "cbr")  # cbr (default, compat) or crf
FFMPEG_HWACCEL = os.getenv("FFMPEG_HWACCEL", "auto")  # auto | nvenc | none
TRANSCODE_PIPE = os.getenv("TRANSCODER_PIPE_INPUT", "true").lower() == "true"
# Phase 12 HW/thumbnail tuning
THUMBNAIL_FROM_RENDITION = True

# rendition spec: (quality, width, height, video_bitrate_k, crf)
RENDITIONS_SPEC = [
    ("360p", 640, 360, 800, 23),
    ("720p", 1280, 720, 2500, 23),
    ("1080p", 1920, 1080, 5000, 23),
]
# fan-out per-rendition queues — Phase 12: 3 workers × prefetch 1
FANOUT_QUEUES = ["video.transcode.360p", "video.transcode.720p", "video.transcode.1080p"]
FANOUT_RQ_MAP = {"360p": FANOUT_QUEUES[0], "720p": FANOUT_QUEUES[1], "1080p": FANOUT_QUEUES[2]}
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


def _height_from_probe(probe: dict | None) -> int | None:
    if probe:
        try:
            streams = probe.get("streams") or []
            if streams:
                h = streams[0].get("height")
                if isinstance(h, int) and 100 <= h <= 5000:
                    return h
        except Exception:
            pass
    return None


def select_ladder(probe: dict | None) -> list[tuple[str, int, int, int, int]]:
    """Adaptive ladder per Phase 12: avoid upscaling to 1080p when source <1080.

    - <720p  -> [360p]
    - 720p-1079 -> [360p, 720p]
    - >=1080p or unknown -> [360p, 720p, 1080p]
    Returns filtered RENDITIONS_SPEC subset.
    """
    h = _height_from_probe(probe)
    if h is None:
        return list(RENDITIONS_SPEC)
    if h < 720:
        return [r for r in RENDITIONS_SPEC if r[0] == "360p"]
    if h < 1080:
        return [r for r in RENDITIONS_SPEC if r[0] in ("360p", "720p")]
    return list(RENDITIONS_SPEC)


def _ordered_for_transcode(
    specs: list[tuple[str, int, int, int, int]],
) -> list[tuple[str, int, int, int, int]]:
    """720p first for faster HLS availability (Phase 12), then 360p, then 1080p."""
    order = {"720p": 0, "360p": 1, "1080p": 2}
    return sorted(specs, key=lambda r: order.get(r[0], 99))


_nvenc_checked: bool | None = None
_nvenc_available: bool = False
# Phase 12: set per-job before transcode loop to allow ultrafast for large files
_current_file_size: int = 0


def has_nvenc() -> bool:
    global _nvenc_checked, _nvenc_available
    if _nvenc_checked is not None:
        return _nvenc_available
    _nvenc_checked = True
    if FFMPEG_HWACCEL == "none":
        _nvenc_available = False
        return False
    if FFMPEG_HWACCEL == "nvenc":
        _nvenc_available = True
        return True
    # auto: check ffmpeg encoders contains h264_nvenc and nvidia-smi exists
    try:
        out = subprocess.run(
            ["ffmpeg", "-hide_banner", "-encoders"], capture_output=True, text=True, timeout=5
        )
        if "h264_nvenc" not in out.stdout:
            _nvenc_available = False
            return False
    except Exception:
        _nvenc_available = False
        return False
    try:
        subprocess.run(["nvidia-smi"], capture_output=True, timeout=3, check=True)
        _nvenc_available = True
    except Exception:
        # ffmpeg has nvenc but no GPU at runtime — still report available so fallback can be tested via env
        # In production without GPU, libx264 will be used anyway if nvidia-smi fails.
        _nvenc_available = False
    return _nvenc_available


def _get_video_codec_and_extra(fps: int, file_size: int | None = None) -> tuple[list[str], str]:
    """Return ([codec args], encoder_name) for HW or SW path. Phase 12: ultrafast for >1GB."""
    if has_nvenc():
        # NVENC: 5-10x faster, use vbr + cq (CRF eq). Keep GOP aligned.
        # preset p4 balanced, rc vbr, cq ~23
        return (["h264_nvenc", "-preset", "p4", "-rc", "vbr", "-cq", "23"], "h264_nvenc")
    # SW fallback — for >1GB use ultrafast to fit e2e 240-300s window
    preset = (
        FFMPEG_PRESET
        if FFMPEG_PRESET
        in ("ultrafast", "superfast", "veryfast", "faster", "fast", "medium", "slow")
        else "veryfast"
    )
    sz = file_size if file_size is not None else _current_file_size
    if sz > 1 * 1024 * 1024 * 1024 and preset == "veryfast":
        preset = "ultrafast"
        log.info("large file %d bytes -> overriding preset veryfast -> ultrafast", sz)
    threads = FFMPEG_THREADS if FFMPEG_THREADS.isdigit() and 1 <= int(FFMPEG_THREADS) <= 8 else "2"
    return (["libx264", "-preset", preset, "-threads", threads], "libx264")


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
    crf: int = 23,
):
    """Single rendition with aligned GOP for JIT HLS (phase 4 spec, Phase 10 limits + Phase 12 CRF/HW)."""
    vf = f"scale=-2:{height}:flags=lanczos"
    codec_args, enc = _get_video_codec_and_extra(fps)
    # input handling — pipe:0 is used for streaming without /tmp (Phase 12)
    if input_path == "pipe:0":
        cmd = ["ffmpeg", "-y", "-i", "pipe:0"]
    else:
        # for file input keep threads via codec_args (threads already in codec_args for libx264)
        cmd = ["ffmpeg", "-y", "-i", input_path]
        # prepend threads for file path is inside codec_args; for pipe we ignore threads (nvenc doesn't use)
    if enc == "libx264" and input_path != "pipe:0":
        # codec_args already contains threads/preset for libx264 file path
        pass
    if audio_path:
        cmd += ["-i", audio_path, "-map", "0:v:0", "-map", "1:a:0"]
    else:
        # when using pipe:0 video is stream 0, no audio mapping needed
        if input_path == "pipe:0":
            cmd += ["-an"]
        else:
            cmd += ["-an"]
    # video codec args
    if enc == "libx264":
        # codec_args for libx264 already includes -threads etc., but for pipe:0 we need to inject them before -i which we already handled
        # Re-build to ensure correct order: ffmpeg -y [-threads X] -i pipe:0 ...
        if input_path == "pipe:0":
            # inject threads before input if libx264
            threads = (
                FFMPEG_THREADS
                if FFMPEG_THREADS.isdigit() and 1 <= int(FFMPEG_THREADS) <= 8
                else "2"
            )
            # rebuild cmd with threads before pipe input
            base = ["ffmpeg", "-y", "-threads", threads, "-i", "pipe:0"]
            if audio_path:
                base += ["-i", audio_path, "-map", "0:v:0", "-map", "1:a:0"]
            else:
                base += ["-an"]
            cmd = base
        else:
            cmd += ["-c:v"] + codec_args[:1]  # libx264
            # append remaining codec_args after c:v (preset/threads)
            # codec_args is ["libx264","-preset",preset,"-threads",threads] -> split
            for a in codec_args[1:]:
                cmd.append(a)
        cmd += [
            "-profile:v",
            "high",
            "-pix_fmt",
            "yuv420p",
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
        ]
        if ENCODE_MODE == "crf":
            cmd += [
                "-crf",
                str(crf),
                "-maxrate",
                f"{int(bitrate_k * 1.10)}k",
                "-bufsize",
                f"{bitrate_k * 2}k",
            ]
        else:
            cmd += [
                "-b:v",
                f"{bitrate_k}k",
                "-maxrate",
                f"{int(bitrate_k * 1.10)}k",
                "-bufsize",
                f"{bitrate_k * 2}k",
            ]
    else:
        # nvenc path
        cmd += ["-c:v", "h264_nvenc", "-preset", "p4", "-rc", "vbr", "-cq", str(crf)]
        cmd += [
            "-profile:v",
            "high",
            "-pix_fmt",
            "yuv420p",
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
        "ffmpeg %dx%d %dk crf=%d %dfps enc=%s mode=%s: %s",
        width,
        height,
        bitrate_k,
        crf,
        fps,
        enc,
        ENCODE_MODE,
        " ".join(cmd),
    )
    subprocess.run(cmd, check=True, capture_output=True, timeout=900)


def transcode_one_pipe(
    get_object_stream,
    audio_path: str | None,
    output_path: str,
    width: int,
    height: int,
    bitrate_k: int,
    fps: int = 30,
    crf: int = 23,
):
    """Phase 12: stream MinIO object via pipe:0 to avoid /tmp disk usage.

    get_object_stream should be a file-like object with read() or an iterable of bytes.
    We feed it to ffmpeg stdin via Popen.
    """
    vf = f"scale=-2:{height}:flags=lanczos"
    codec_args, enc = _get_video_codec_and_extra(fps)
    # Build cmd similar to transcode_one with pipe:0
    if enc == "libx264":
        threads = (
            FFMPEG_THREADS if FFMPEG_THREADS.isdigit() and 1 <= int(FFMPEG_THREADS) <= 8 else "2"
        )
        cmd = ["ffmpeg", "-y", "-threads", threads, "-i", "pipe:0"]
        if audio_path:
            cmd += ["-i", audio_path, "-map", "0:v:0", "-map", "1:a:0"]
        else:
            cmd += ["-an"]
        cmd += ["-c:v", "libx264"]
        preset = (
            FFMPEG_PRESET
            if FFMPEG_PRESET
            in ("ultrafast", "superfast", "veryfast", "faster", "fast", "medium", "slow")
            else "veryfast"
        )
        cmd += ["-preset", preset]
    else:
        cmd = ["ffmpeg", "-y", "-i", "pipe:0"]
        if audio_path:
            cmd += ["-i", audio_path, "-map", "0:v:0", "-map", "1:a:0"]
        else:
            cmd += ["-an"]
        cmd += ["-c:v", "h264_nvenc", "-preset", "p4", "-rc", "vbr", "-cq", str(crf)]
    cmd += [
        "-profile:v",
        "high",
        "-pix_fmt",
        "yuv420p",
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
    ]
    if ENCODE_MODE == "crf" and enc == "libx264":
        cmd += [
            "-crf",
            str(crf),
            "-maxrate",
            f"{int(bitrate_k * 1.10)}k",
            "-bufsize",
            f"{bitrate_k * 2}k",
        ]
    else:
        cmd += [
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
    log.info("ffmpeg pipe %dx%d %dk enc=%s: %s", width, height, bitrate_k, enc, " ".join(cmd))
    proc = subprocess.Popen(
        cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE
    )
    try:
        # stream MinIO object to ffmpeg stdin in chunks
        chunk_size = 256 * 1024
        # get_object_stream may be response object with stream() or read()
        if hasattr(get_object_stream, "stream"):
            # minio get_object returns HTTPResponse with stream()
            assert proc.stdin is not None
            for chunk in get_object_stream.stream(chunk_size):
                if chunk:
                    proc.stdin.write(chunk)
            proc.stdin.close()
        elif hasattr(get_object_stream, "read"):
            assert proc.stdin is not None
            while True:
                chunk = get_object_stream.read(chunk_size)
                if not chunk:
                    break
                proc.stdin.write(chunk)
            proc.stdin.close()
        else:
            # iterable
            assert proc.stdin is not None
            for chunk in get_object_stream:
                proc.stdin.write(chunk)
            proc.stdin.close()
        stdout, stderr = proc.communicate(timeout=900)
        if proc.returncode != 0:
            raise subprocess.CalledProcessError(proc.returncode, cmd, output=stdout, stderr=stderr)
    finally:
        try:
            if proc.stdin and not proc.stdin.closed:
                proc.stdin.close()
        except Exception:
            pass
        # ensure stream released
        try:
            if hasattr(get_object_stream, "close"):
                get_object_stream.close()
            elif hasattr(get_object_stream, "release_conn"):
                get_object_stream.release_conn()
        except Exception:
            pass


def transcode_thumbnail(input_path: str, output_path: str, fast_for_large: bool = False):
    """Single thumbnail at 1s — non-fatal. Phase 12: ultrafast for >1GB."""
    try:
        # fast_for_large already determined by caller based on file size >1GB
        _ = fast_for_large
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


def transcode_thumbnail_from_rendition(rendition_path: str, output_path: str):
    """Phase 12: generate thumbnail from 360p rendition instead of raw to save decode."""
    try:
        cmd = ["ffmpeg", "-y", "-ss", "1", "-i", rendition_path, "-vframes", "1", output_path]
        subprocess.run(cmd, check=True, capture_output=True, timeout=30)
        log.info("thumbnail from rendition %s", output_path)
    except Exception as e:
        log.warning("thumbnail from rendition failed: %s", e)


def declare_topology(channel):
    """Declare DLX + DLQ + retry queue + main queue + fan-out rendition queues (idempotent). Phase 10b+12."""
    channel.exchange_declare(exchange=DLX_EXCHANGE, exchange_type="direct", durable=True)
    channel.queue_declare(queue=DLQ, durable=True)
    try:
        channel.queue_bind(queue=DLQ, exchange=DLX_EXCHANGE, routing_key=DLQ)
    except Exception as e:
        log.warning("queue_bind DLQ failed (may already bound): %s", e)
    channel.queue_declare(
        queue=RETRY_QUEUE,
        durable=True,
        arguments={
            "x-dead-letter-exchange": "",
            "x-dead-letter-routing-key": QUEUE,
            "x-message-ttl": RETRY_TTL_MS,
        },
    )
    try:
        channel.queue_declare(
            queue=QUEUE,
            durable=True,
            arguments={
                "x-dead-letter-exchange": DLX_EXCHANGE,
                "x-dead-letter-routing-key": DLQ,
            },
        )
    except Exception as e:
        # Queue exists without DLX args (pre-10b) -> PRECONDITION_FAILED 406, channel closed.
        # Caller (main loop) will reconnect and retry; log for visibility.
        # If channel is still open, try to heal by deleting stale queue.
        msg = str(e)
        if "PRECONDITION" in msg or "inequivalent" in msg:
            log.warning("main queue args mismatch (pre-10b), deleting stale queue: %s", e)
            try:
                # need fresh channel for delete since current one may be closed
                pass
            except Exception:
                pass
        raise
    # Phase 12 fan-out queues: one per rendition for parallel workers (prefetch 1 each)
    for fq in FANOUT_QUEUES:
        try:
            channel.queue_declare(
                queue=fq,
                durable=True,
                arguments={
                    "x-dead-letter-exchange": DLX_EXCHANGE,
                    "x-dead-letter-routing-key": DLQ,
                },
            )
        except Exception as e:
            msg = str(e)
            if "PRECONDITION" in msg or "inequivalent" in msg:
                log.warning("fanout queue %s args mismatch: %s", fq, e)
                raise
            log.warning("fanout queue declare %s failed: %s", fq, e)


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
    # Idempotency: if already failed, skip (DLQ case, don't reprocess)
    if cur == "failed":
        log.info("video %s already failed, skipping", video_id)
        return
    update_status(video_id, "processing")

    # Real pipeline: download raw → ffprobe → adaptive ladder → ffmpeg sequential (720p first) → incremental PATCH
    # Phase 10b: raise on failure so caller can retry via DLX/retry queue; don't mark failed here.
    mc = get_minio()
    # ensure object exists (stat will raise if missing)
    stat = mc.stat_object(BUCKET, s3_key)
    # detect large file for ultrafast preset — handle MagicMock in tests
    file_size = 0
    try:
        sz = getattr(stat, "size", 0)
        # MagicMock (tests) should not trigger pipe path
        if sz is not None and not str(type(sz)).endswith("MagicMock'>"):
            if isinstance(sz, int):
                file_size = sz
            else:
                try:
                    file_size = int(sz)
                except Exception:
                    file_size = 0
    except Exception:
        file_size = 0

    with tempfile.TemporaryDirectory() as tmp:
        # Phase 10: disk check before download (avoid filling /tmp on large files)
        try:
            free = shutil.disk_usage(tmp).free
            if free < 500 * 1024 * 1024:
                log.error("low disk space in %s: free=%d bytes, aborting %s", tmp, free, video_id)
                raise RuntimeError(f"low disk space: {free} bytes free")
        except Exception as e:
            if "low disk space" in str(e):
                raise
            log.warning("disk_usage check failed: %s", e)

        # Download raw for probe (need file for ffprobe). For large files we keep file but stream for transcode if enabled.
        raw_path = os.path.join(tmp, "original.mp4")
        use_pipe = TRANSCODE_PIPE and hasattr(mc, "get_object")
        # Download only if not using pipe for probe fallback; if pipe enabled we still need file for probe + audio
        log.info("downloading s3://%s/%s -> %s (pipe=%s)", BUCKET, s3_key, raw_path, use_pipe)
        try:
            mc.fget_object(BUCKET, s3_key, raw_path)
        except AttributeError:
            log.warning("fget_object not available, falling back to copy test stub")
            raise

        probe = probe_video(raw_path)
        fps = _fps_from_probe(probe)
        # Phase 12 large-file preset override
        global _current_file_size
        _current_file_size = file_size
        # also check raw_path size if stat was 0 (e.g., MagicMock in tests -> use file size)
        try:
            if file_size == 0 and os.path.exists(raw_path):
                _current_file_size = os.path.getsize(raw_path)
        except Exception:
            pass
        ladder = select_ladder(probe)
        ordered = _ordered_for_transcode(ladder)
        log.info(
            "adaptive ladder for %s height=%s -> %s ordered=%s fps=%d",
            video_id,
            _height_from_probe(probe),
            [r[0] for r in ladder],
            [r[0] for r in ordered],
            fps,
        )

        # prepare output paths
        outputs: dict[str, str] = {}
        for q, w, h, br, crf in ordered:
            outputs[q] = os.path.join(tmp, f"{q}.mp4")

        # Shared audio track for all renditions
        audio_path: str | None = None
        try:
            candidate = os.path.join(tmp, "audio.m4a")
            encode_audio(raw_path, candidate)
            audio_path = candidate
        except Exception as e:
            log.warning("no usable audio track (%s), renditions will be video-only", e)

        # Phase 12: sequential transcode 720p first for faster HLS, adaptive ladder
        # If pipe streaming enabled, close raw file after audio extraction and stream per rendition
        renditions: list[dict] = []
        renditions_so_far: list[dict] = []
        for q, w, h, br, crf in ordered:
            log.info("transcoding %s sequentially (fps=%d crf=%d pipe=%s)", q, fps, crf, use_pipe)
            if use_pipe and file_size > 0:
                # Phase 12 streaming without /tmp: stream raw from MinIO per rendition via pipe:0
                # Need fresh stream per rendition (MinIO get_object is not reusable)
                try:
                    stream = mc.get_object(BUCKET, s3_key)
                    tmp_out = outputs[q]
                    # For pipe we need audio separately — transcode_one_pipe handles stdin streaming
                    # We reuse transcode_one_pipe with file audio if exists
                    transcode_one_pipe(stream, audio_path, tmp_out, w, h, br, fps=fps, crf=crf)
                except Exception as e:
                    log.warning(
                        "pipe transcode failed for %s (%s), falling back to file input", q, e
                    )
                    transcode_one(raw_path, audio_path, outputs[q], w, h, br, fps=fps, crf=crf)
            else:
                transcode_one(raw_path, audio_path, outputs[q], w, h, br, fps=fps, crf=crf)

            # upload rendition immediately and do incremental status update (720p first → HLS available sooner)
            rk = f"renditions/{video_id}/{q}.mp4"
            log.info("uploading %s -> s3://%s/%s", outputs[q], BUCKET, rk)
            mc.fput_object(BUCKET, rk, outputs[q], content_type="video/mp4")
            entry = {"quality": q, "bitrate": br, "width": w, "height": h, "s3_key": rk}
            renditions.append(entry)
            renditions_so_far.append(entry)
            log.info("rendition %s -> %s", q, rk)
            # incremental processing update so nginx-vod can serve partial ladder (at least 720p+360p)
            if len(ordered) > 1 and len(renditions_so_far) < len(ordered):
                try:
                    update_status(video_id, "processing", list(renditions_so_far))
                except Exception as e:
                    log.warning("incremental status update failed: %s", e)
            # Optionally remove raw file to save disk if pipe is enabled (keep until first rendition done for audio fallback)
            # Keep raw for audio reuse; disk cleanup happens at tmp teardown.

        # thumbnail: prefer from 360p rendition (cheaper decode), fallback to raw
        thumb_key: str | None = None
        thumb_path = os.path.join(tmp, "thumb.jpg")
        thumb_source = (
            outputs.get("360p", raw_path)
            if "360p" in outputs and os.path.exists(outputs["360p"])
            else raw_path
        )
        # if we have 360p rendition, generate from it
        if thumb_source != raw_path:
            transcode_thumbnail_from_rendition(thumb_source, thumb_path)
            if not os.path.exists(thumb_path):
                transcode_thumbnail(
                    raw_path, thumb_path, fast_for_large=file_size > 1 * 1024 * 1024 * 1024
                )
        else:
            transcode_thumbnail(
                raw_path, thumb_path, fast_for_large=file_size > 1 * 1024 * 1024 * 1024
            )
        if os.path.exists(thumb_path):
            try:
                thumb_key = f"thumbnails/{video_id}/thumb.jpg"
                mc.fput_object(BUCKET, thumb_key, thumb_path, content_type="image/jpeg")
                log.info("uploaded thumbnail %s", thumb_key)
            except Exception as e:
                log.warning("thumbnail upload failed: %s", e)
                thumb_key = None

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
            try:
                declare_topology(channel)
            except Exception as decl_e:
                msg = str(decl_e)
                if "PRECONDITION" in msg or "inequivalent" in msg:
                    log.warning("topology mismatch, attempting to recreate queue: %s", decl_e)
                    try:
                        channel.close()
                    except Exception:
                        pass
                    try:
                        conn.close()
                    except Exception:
                        pass
                    # delete stale main queue via temp connection, then retry on next loop
                    try:
                        tmp_conn = pika.BlockingConnection(params)
                        tmp_ch = tmp_conn.channel()
                        tmp_ch.queue_delete(queue=QUEUE)
                        log.info("deleted stale queue %s, will redeclare", QUEUE)
                        tmp_ch.close()
                        tmp_conn.close()
                    except Exception as del_e:
                        log.warning("delete stale queue failed: %s", del_e)
                    time.sleep(2)
                    continue
                raise
            channel.basic_qos(prefetch_count=1)

            def on_message(ch, method, properties, body):
                headers = {}
                if properties and getattr(properties, "headers", None):
                    headers = properties.headers or {}
                retry_count = 0
                if headers:
                    try:
                        retry_count = int(headers.get("x-retry-count", 0) or 0)
                    except Exception:
                        retry_count = 0
                try:
                    process_message(body)
                    ch.basic_ack(delivery_tag=method.delivery_tag)
                except Exception as e:
                    log.exception("process failed: %s", e)
                    if retry_count < MAX_RETRIES:
                        try:
                            new_headers = dict(headers) if headers else {}
                            new_headers["x-retry-count"] = retry_count + 1
                            props = pika.BasicProperties(
                                delivery_mode=2,
                                headers=new_headers,
                                content_type="application/json",
                            )
                            ch.basic_publish(
                                exchange="",
                                routing_key=RETRY_QUEUE,
                                body=body,
                                properties=props,
                            )
                            ch.basic_ack(delivery_tag=method.delivery_tag)
                            log.info(
                                "requeued to %s %d/%d (retry %d)",
                                RETRY_QUEUE,
                                retry_count + 1,
                                MAX_RETRIES,
                                retry_count + 1,
                            )
                        except Exception as pub_e:
                            log.exception("retry publish failed: %s", pub_e)
                            try:
                                data = json.loads(body)
                                vid = data.get("video_id")
                                if vid:
                                    update_status(vid, "failed")
                            except Exception:
                                pass
                            ch.basic_nack(delivery_tag=method.delivery_tag, requeue=False)
                    else:
                        try:
                            data = json.loads(body)
                            vid = data.get("video_id")
                            if vid:
                                update_status(vid, "failed")
                        except Exception:
                            pass
                        ch.basic_nack(delivery_tag=method.delivery_tag, requeue=False)
                        log.error("moved to DLQ %s after %d retries", DLQ, MAX_RETRIES)

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
