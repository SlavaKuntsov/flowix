FROM ghcr.io/astral-sh/uv:0.7-python3.11-bookworm-slim AS builder
WORKDIR /app
ENV UV_COMPILE_BYTECODE=1 UV_LINK_MODE=copy
COPY pyproject.toml uv.lock* ./
RUN --mount=type=cache,target=/root/.cache/uv uv sync --frozen --no-install-project || uv sync --no-install-project
COPY src ./src
COPY alembic.ini ./
COPY migrations ./migrations
RUN --mount=type=cache,target=/root/.cache/uv uv sync --frozen || uv sync

FROM python:3.11-slim
WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends curl && rm -rf /var/lib/apt/lists/*
COPY --from=builder /app/.venv /app/.venv
COPY --from=builder /app/src ./src
COPY --from=builder /app/alembic.ini ./
COPY --from=builder /app/migrations ./migrations
COPY pyproject.toml ./
ENV PATH="/app/.venv/bin:$PATH" PYTHONPATH=/app VIRTUAL_ENV=/app/.venv
EXPOSE 8001
CMD ["uvicorn", "src.main:app", "--host", "0.0.0.0", "--port", "8001"]
HEALTHCHECK --interval=10s --timeout=3s --retries=5 CMD curl -f http://localhost:8001/health || exit 1
