import asyncio
import os
from logging.config import fileConfig

from alembic import context
from sqlalchemy import pool
from sqlalchemy.ext.asyncio import async_engine_from_config

# Alembic Config object
config = context.config
if config.config_file_name is not None:
    fileConfig(config.config_file_name)

# Import models' Base for autogenerate
from src.models import Base  # noqa: E402

target_metadata = Base.metadata

# DATABASE_URL comes from env; fallback to sync url for alembic
# Alembic needs sync driver, but we reuse async url stripped to psycopg
DATABASE_URL = os.getenv("DATABASE_URL", "postgres://flowix:flowix@localhost:5432/flowix?sslmode=disable")


def _sync_url(url: str) -> str:
    # Convert postgres:// -> postgresql:// and drop sslmode (asyncpg/psycopg handles differently)
    from urllib.parse import parse_qs, urlencode, urlparse, urlunparse

    if url.startswith("postgres://"):
        url = url.replace("postgres://", "postgresql://", 1)
    parsed = urlparse(url)
    if parsed.query:
        qs = parse_qs(parsed.query)
        qs.pop("sslmode", None)
        query = urlencode({k: v[0] for k, v in qs.items()}, doseq=False)
        url = urlunparse(parsed._replace(query=query))
    # use psycopg driver for sync migrations
    if url.startswith("postgresql://") and "+psycopg" not in url:
        url = url.replace("postgresql://", "postgresql+psycopg://", 1)
    return url


def run_migrations_offline() -> None:
    url = _sync_url(DATABASE_URL)
    context.configure(
        url=url,
        target_metadata=target_metadata,
        literal_binds=True,
        dialect_opts={"paramstyle": "named"},
    )
    with context.begin_transaction():
        context.run_migrations()


def do_run_migrations(connection):
    context.configure(connection=connection, target_metadata=target_metadata)
    with context.begin_transaction():
        context.run_migrations()


async def run_async_migrations():
    configuration = config.get_section(config.config_ini_section, {})
    configuration["sqlalchemy.url"] = _sync_url(DATABASE_URL)
    connectable = async_engine_from_config(
        configuration,
        prefix="sqlalchemy.",
        poolclass=pool.NullPool,
    )
    async with connectable.connect() as connection:
        await connection.run_sync(do_run_migrations)
    await connectable.dispose()


def run_migrations_online() -> None:
    asyncio.run(run_async_migrations())


if context.is_offline_mode():
    run_migrations_offline()
else:
    run_migrations_online()
