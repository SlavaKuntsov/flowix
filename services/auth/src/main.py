from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from .routers.auth import router

app = FastAPI(title="flowix-auth", version="0.1.0")
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)
app.include_router(router)


@app.get("/health")
def health():
    return {"status": "ok", "service": "auth"}


@app.get("/")
def root():
    return {"service": "auth", "docs": "/docs"}
