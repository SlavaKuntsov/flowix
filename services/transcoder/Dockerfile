FROM ghcr.io/astral-sh/uv:0.9-python3.14-bookworm-slim AS builder
WORKDIR /app
ENV UV_COMPILE_BYTECODE=1 UV_LINK_MODE=copy
# FFmpeg CPU build; for NVENC HW accel use jrottenberg/ffmpeg:6.1-nvidia or install nvidia runtime (Phase 12)
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg curl && rm -rf /var/lib/apt/lists/*
COPY pyproject.toml uv.lock* ./
RUN --mount=type=cache,target=/root/.cache/uv uv sync --frozen --no-install-project || uv sync --no-install-project
COPY app ./app
RUN --mount=type=cache,target=/root/.cache/uv uv sync --frozen || uv sync

FROM python:3.14-slim
WORKDIR /app
# Phase 12: ffmpeg with fallback to libx264; if FFMPEG_HWACCEL=nvenc and nvidia runtime present, h264_nvenc is used
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg curl procps && rm -rf /var/lib/apt/lists/*
COPY --from=builder /app/.venv /app/.venv
COPY --from=builder /app/app ./app
COPY pyproject.toml ./
ENV PATH="/app/.venv/bin:$PATH" PYTHONPATH=/app VIRTUAL_ENV=/app/.venv
CMD ["python", "-m", "app.consumer"]
HEALTHCHECK --interval=30s CMD pgrep -f "app.consumer" >/dev/null || exit 1
