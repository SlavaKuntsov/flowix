from fastapi import FastAPI

from .routers.auth import router

app = FastAPI(title="flowix-auth", version="0.1.0")
# CORS handled by gateway (single entry point) to avoid duplicate
# Access-Control-Allow-Origin headers (gateway sets origin, upstream must not).
# Auth is behind gateway; direct :8001 access is internal/Swagger only.
app.include_router(router)


@app.get("/health")
def health():
    return {"status": "ok", "service": "auth"}


@app.get("/")
def root():
    return {"service": "auth", "docs": "/docs"}
