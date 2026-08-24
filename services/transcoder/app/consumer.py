import json
import logging
import os
import time

import pika  # type: ignore[import-untyped]
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
QUEUE = "video.uploaded"


def get_minio():
    return Minio(
        MINIO_ENDPOINT,
        access_key=MINIO_ACCESS,
        secret_key=MINIO_SECRET,
        secure=MINIO_SECURE,
    )


def update_status(video_id: str, status: str, renditions=None):
    url = f"{METADATA_URL}/internal/videos/{video_id}/status"
    payload: dict = {"status": status}
    if renditions:
        payload["renditions"] = renditions
    try:
        r = requests.patch(url, json=payload, timeout=5)
        r.raise_for_status()
        log.info("updated %s -> %s", video_id, status)
    except Exception as e:
        log.error("metadata update %s failed: %s", video_id, e)


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

    # Simulate transcoding: mark processing, then create renditions via MinIO copy (stub)
    update_status(video_id, "processing")
    time.sleep(2)  # simulate FFmpeg

    # Stub: copy original to renditions (real impl would run FFmpeg)
    try:
        mc = get_minio()
        # ensure object exists
        mc.stat_object(BUCKET, s3_key)
        renditions = []
        for q, w, h, br in [
            ("360p", 640, 360, 800),
            ("720p", 1280, 720, 2500),
            ("1080p", 1920, 1080, 5000),
        ]:
            rk = f"renditions/{video_id}/{q}.mp4"
            # copy object via copy
            from minio.commonconfig import CopySource

            mc.copy_object(BUCKET, rk, CopySource(BUCKET, s3_key))
            renditions.append({"quality": q, "bitrate": br, "width": w, "height": h, "s3_key": rk})
            log.info("rendition %s -> %s", q, rk)
    except Exception as e:
        log.error("minio copy failed for %s: %s", video_id, e)
        update_status(video_id, "failed")
        return

    update_status(video_id, "ready", renditions)


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
        except pika.exceptions.AMQPConnectionError as e:
            log.error("rabbitmq connection failed: %s, retry in 5s", e)
            time.sleep(5)
        except KeyboardInterrupt:
            break
        except Exception as e:
            log.exception("consumer error: %s", e)
            time.sleep(5)


if __name__ == "__main__":
    main()
