import logging

from .celery_app import app

log = logging.getLogger(__name__)


@app.task(name="transcoder.ping")
def ping() -> str:
    log.info("transcoder ping")
    return "pong"


@app.task(name="transcoder.transcode")
def transcode(video_id: str, s3_key: str) -> dict:
    """Stub for Phase 4 — real FFmpeg logic will be here (aligned segments)."""
    log.info("transcode stub: video_id=%s s3_key=%s", video_id, s3_key)
    return {"video_id": video_id, "status": "stub"}
