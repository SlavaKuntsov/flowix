import os

from .config import settings

try:
    from slowapi import Limiter
    from slowapi.util import get_remote_address

    _redis_url = os.getenv("REDIS_URL") or settings.redis_url

    def _make_limiter(url: str | None):  # type: ignore[no-untyped-def]
        try:
            if url and url.startswith("redis"):
                # probe redis connectivity — fallback to memory if unreachable
                lim = Limiter(key_func=get_remote_address, storage_uri=url, default_limits=[])  # type: ignore[no-untyped-call]
                # quick storage check: try to get reset without network if possible
                try:
                    # try a dummy operation; if redis not reachable, this will raise
                    import socket

                    # parse host/port from url
                    from urllib.parse import urlparse

                    p = urlparse(url)
                    host = p.hostname or "redis"
                    port = p.port or 6379
                    s = socket.create_connection((host, port), timeout=0.5)
                    s.close()
                except Exception:
                    # redis not reachable — use memory
                    return Limiter(key_func=get_remote_address, default_limits=[])  # type: ignore[no-untyped-call]
                return lim
            return Limiter(key_func=get_remote_address, default_limits=[])  # type: ignore[no-untyped-call]
        except Exception:
            return Limiter(key_func=get_remote_address, default_limits=[])  # type: ignore[no-untyped-call]

    limiter: Limiter | None = _make_limiter(_redis_url)
except ImportError:
    limiter = None  # type: ignore[assignment]
