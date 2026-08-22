FROM ghcr.io/astral-sh/uv:0.7-python3.11-bookworm-slim AS builder
WORKDIR /app
ENV UV_COMPILE_BYTECODE=1 UV_LINK_MODE=copy
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg curl && rm -rf /var/lib/apt/lists/*
COPY pyproject.toml uv.lock* ./
RUN --mount=type=cache,target=/root/.cache/uv uv sync --frozen --no-install-project || uv sync --no-install-project
COPY app ./app
RUN --mount=type=cache,target=/root/.cache/uv uv sync --frozen || uv sync

FROM python:3.11-slim
WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg curl && rm -rf /var/lib/apt/lists/*
COPY --from=builder /app/.venv /app/.venv
COPY --from=builder /app/app ./app
COPY pyproject.toml ./
ENV PATH="/app/.venv/bin:$PATH" PYTHONPATH=/app VIRTUAL_ENV=/app/.venv
CMD ["celery", "-A", "app.celery_app", "worker", "--loglevel=info"]
HEALTHCHECK --interval=30s CMD celery -A app.celery_app inspect ping | grep -q pong || exit 1
