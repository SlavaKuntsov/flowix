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


engine = create_async_engine(_async_url(settings.database_url), echo=False, pool_pre_ping=True)
SessionLocal = async_sessionmaker(engine, expire_on_commit=False)


async def get_db():
    async with SessionLocal() as s:
        yield s
