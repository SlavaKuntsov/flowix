from fastapi import FastAPI, Request
from fastapi.responses import Response

from .routers.auth import router

try:
    from prometheus_client import CONTENT_TYPE_LATEST, Counter, Gauge, Histogram, generate_latest

    auth_requests = Counter("auth_requests_total", "Auth requests", ["method", "endpoint"])
    rabbitmq_queue_depth = Gauge("rabbitmq_queue_depth", "RabbitMQ queue depth")
    upload_bytes = Counter("upload_bytes", "Upload bytes")
    vod_cache_hit = Counter("vod_cache_hit", "VOD cache hits")
    ffmpeg_duration = Histogram("ffmpeg_duration_seconds", "FFmpeg duration", ["quality"])
    METRICS_ENABLED = True
except ImportError:
    METRICS_ENABLED = False

app = FastAPI(title="flowix-auth", version="0.1.0")
# CORS handled by gateway (single entry point) to avoid duplicate
# Access-Control-Allow-Origin headers (gateway sets origin, upstream must not).
# Auth is behind gateway; direct :8001 access is internal/Swagger only.
app.include_router(router)


@app.get("/health")
def health():
    return {"status": "ok", "service": "auth"}


if METRICS_ENABLED:

    @app.get("/metrics")
    def metrics():
        return Response(generate_latest(), media_type=CONTENT_TYPE_LATEST)

    @app.middleware("http")
    async def metrics_middleware(request: Request, call_next):
        response = await call_next(request)
        try:
            auth_requests.labels(method=request.method, endpoint=request.url.path).inc()
        except Exception:
            pass
        return response


@app.get("/")
def root():
    return {"service": "auth", "docs": "/docs"}
