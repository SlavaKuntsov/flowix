import json
import logging
import sys
import time
import uuid

from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse, Response

from .core.limiter import limiter
from .routers.auth import router

# Unified JSON logging with trace_id (Phase 14)
_auth_logger = logging.getLogger("auth")
if not _auth_logger.handlers:
    _h = logging.StreamHandler(sys.stdout)
    _h.setFormatter(logging.Formatter("%(message)s"))
    _auth_logger.addHandler(_h)
_auth_logger.setLevel(logging.INFO)

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
if limiter is not None:
    app.state.limiter = limiter  # type: ignore[attr-defined]
    try:
        from slowapi.errors import RateLimitExceeded
        from slowapi.middleware import SlowAPIMiddleware

        app.add_middleware(SlowAPIMiddleware)  # type: ignore[arg-type]

        @app.exception_handler(RateLimitExceeded)
        async def _rate_limit_handler(request: Request, exc: RateLimitExceeded):  # type: ignore[no-redef]
            return JSONResponse(
                status_code=429, content={"detail": f"rate limit exceeded: {exc.detail}"}
            )

    except ImportError:
        pass

app.include_router(router)


@app.middleware("http")
async def trace_middleware(request: Request, call_next):
    trace_id = (
        request.headers.get("x-request-id")
        or request.headers.get("X-Request-ID")
        or str(uuid.uuid4())
    )
    request.state.trace_id = trace_id
    start = time.time()
    response = await call_next(request)
    duration = time.time() - start
    level = (
        "info" if response.status_code < 400 else "warn" if response.status_code < 500 else "error"
    )
    log_data = {
        "level": level,
        "msg": "request",
        "service": "auth",
        "method": request.method,
        "path": request.url.path,
        "status": response.status_code,
        "duration": duration,
        "trace_id": trace_id,
        "request_id": trace_id,
    }
    try:
        _auth_logger.info(json.dumps(log_data))
    except Exception:
        pass
    response.headers["X-Request-ID"] = trace_id
    return response


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
