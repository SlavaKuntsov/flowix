import os

from celery import Celery  # type: ignore[import-untyped]

broker_url = os.getenv("RABBITMQ_URL", "amqp://flowix:flowix@localhost:5672//")
result_backend = os.getenv("CELERY_RESULT_BACKEND", "rpc://")

app = Celery("transcoder", broker=broker_url, backend=result_backend)
celery = app  # alias for `celery -A app.celery_app` (expects `celery` attr)

app.conf.update(
    task_serializer="json",
    accept_content=["json"],
    result_serializer="json",
    timezone="UTC",
    enable_utc=True,
    task_track_started=True,
    worker_prefetch_multiplier=1,
)

# Import tasks so Celery discovers them (stub for Phase 4)
try:
    import app.tasks  # noqa: F401
except ImportError:
    pass
