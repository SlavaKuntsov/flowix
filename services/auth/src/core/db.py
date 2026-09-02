import os

from sqlalchemy.ext.asyncio import async_sessionmaker, create_async_engine

from .config import settings


# Convert postgres:// to postgresql+asyncpg:// and strip unsupported query params (asyncpg doesn't accept ?sslmode)
def _async_url(url: str) -> str:
    from urllib.parse import parse_qs, urlencode, urlparse, urlunparse

    if url.startswith("postgres://"):
        url = url.replace("postgres://", "postgresql+asyncpg://", 1)
    elif url.startswith("postgresql://"):
        url = url.replace("postgresql://", "postgresql+asyncpg://", 1)

    # asyncpg doesn't support sslmode; map it to ?ssl= if needed, otherwise drop
    parsed = urlparse(url)
    if parsed.query:
        qs = parse_qs(parsed.query)
        qs.pop("sslmode", None)
        # keep other params
        query = urlencode({k: v[0] for k, v in qs.items()}, doseq=False)
        url = urlunparse(parsed._replace(query=query))
    return url


def _resolve_url(raw: str) -> str:
    # Phase 15: allow PgBouncer via PGBOUNCER_ENABLED without changing DATABASE_URL in .env
    if os.getenv("PGBOUNCER_ENABLED", "").lower() in ("1", "true", "yes"):
        # rewrite postgres host -> pgbouncer service (inside docker network port 5432)
        if "pgbouncer" not in raw:
            raw = raw.replace("@postgres:", "@pgbouncer:").replace("@postgres/", "@pgbouncer/")
            raw = raw.replace("@localhost:", "@pgbouncer:").replace("localhost:", "pgbouncer:")
    return raw


def _pool_kwargs(url: str) -> dict:
    # Phase 15: tune pool for PgBouncer (transaction mode requires statement_cache_size=0)
    is_pgbouncer = "pgbouncer" in url or os.getenv("PGBOUNCER_ENABLED", "").lower() in (
        "1",
        "true",
        "yes",
    )
    kwargs: dict = {
        "echo": False,
        "pool_pre_ping": True,
        "pool_size": int(os.getenv("DATABASE_POOL_SIZE", "5")),
        "max_overflow": int(os.getenv("DATABASE_MAX_OVERFLOW", "10")),
    }
    if is_pgbouncer:
        kwargs["connect_args"] = {"statement_cache_size": 0}
    return kwargs


_resolved_url = _resolve_url(settings.database_url)
engine = create_async_engine(_async_url(_resolved_url), **_pool_kwargs(_resolved_url))
SessionLocal = async_sessionmaker(engine, expire_on_commit=False)


async def get_db():
    async with SessionLocal() as s:
        yield s
