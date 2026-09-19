# Flowix — Open-Source Video Streaming Platform

> YouTube-like MVP with adaptive HLS, JIT transcoding and microservices architecture

[![Go](https://img.shields.io/badge/Go-1.27-%2300ADD8?logo=go)](https://go.dev)
[![Python](https://img.shields.io/badge/Python-3.14-%233770A8?logo=python)](https://www.python.org)
[![FastAPI](https://img.shields.io/badge/FastAPI-0.11-%23009688?logo=fastapi)](https://fastapi.tiangolo.com)
[![Next.js](https://img.shields.io/badge/Next.js-14-black?logo=next.js)](https://nextjs.org)
[![Docker](https://img.shields.io/badge/Docker-Compose-%232496ED?logo=docker)](deploy/docker-compose.yml)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](#license)

**Flowix** is an open-source, self-hostable alternative to YouTube/Vimeo focused on **adaptive bitrate streaming (HLS)**, seamless quality switching and scalable video processing. Upload → transcode (FFmpeg) → stream via `nginx-vod` — all orchestrated with RabbitMQ and S3-compatible storage.

---

## ✨ Features

- **Adaptive HLS streaming** — 360p / 720p / 1080p renditions, aligned segments (`-force_key_frames`, `-sc_threshold 0`), one codec / framerate / shared audio track, `maxBufferLength 4` for seamless quality switch
- **JIT packaging** — only 3 MP4s stored, HLS/DASH segmented on-the-fly by `nginx-vod` (`vod_mode mapped`, `vod_segment_duration 2000`)
- **Async pipeline** — `upload → RabbitMQ (video.uploaded) → pika + FFmpeg (sequential, -threads 2 -preset veryfast, heartbeat 600) → MinIO → metadata (ready) → HLS`
- **Secure & scalable** — `INTERNAL_TOKEN` for `/internal/*`, `MaxBytesReader 5GB` (`UPLOAD_MAX_BYTES`), `private` raw bucket (`renditions/thumbnails` download), owner attribution (`owner_email`)
- **Microservices** — Go (`chi`) for gateway/metadata/upload, Python (`FastAPI` + `pika`) for auth/transcoder
- **Modern frontend** — Next.js 14 App Router + `hls.js` + `zustand` + `tailwind`, `playbackRate 0.5–2×`, owner badges, delete (`DELETE /api/v1/videos/:id` owner only + S3 cleanup)
- **Production-ready infra** — Postgres 16 + `golang-migrate` (`deploy/migrations`), MinIO, RabbitMQ, Docker Compose, healthchecks, JWT (HS256, Argon2)

## 🏗 Architecture

```
[ Next.js :3000 ] ──► [ Gateway :8080 (Go/chi) ] ─┬─► [ Auth :8001 (FastAPI) ] ──► Postgres
                                                   ├─► [ Metadata :8002 (Go) ] ──► Postgres (+ MinIO delete)
                                                   ├─► [ Upload :8003 (Go) ] ──► MinIO raw/ + RabbitMQ
                                                   └─► [ nginx-vod :8081 ] ──► MinIO (renditions) + metadata /internal/.../vod (X-Internal-Token)

[RabbitMQ] video.uploaded ──► [Transcoder (pika + FFmpeg, sequential, heartbeat 600)] ──► MinIO renditions/{id}/{360,720,1080}.mp4 + audio.m4a
                                                     └─► PATCH /internal/videos/:id/status (X-Internal-Token) → metadata
```

> Full breakdown: [`docs/services-pipeline.md`](docs/services-pipeline.md) (services & pipeline, RU) · Spec: [`docs/spec.md`](docs/spec.md)

## 🧩 Tech Stack

| Layer | Tech |
|-------|------|
| Gateway / Metadata / Upload | Go 1.27, `go-chi/chi/v5`, `pgx`, `zerolog`, `amqp091-go`, `minio-go` (metadata delete) |
| Auth / Transcoder | Python 3.14, FastAPI, `pika`, `uv`, `argon2`, `minio`, `ffmpeg` (`-threads 2 -preset veryfast`, `heartbeat 600`) |
| Infra | Postgres 16 + `golang-migrate` (`deploy/migrations`), MinIO (private raw, download renditions), RabbitMQ 3, `nginx-vod-module` (`envsubst` INTERNAL_TOKEN), FFmpeg |
| Frontend | Next.js 14, `hls.js` 1.5 (`maxBufferLength 4`), `zustand` 4, Tailwind |
| Tooling | Docker Compose, `golangci-lint`, `black`/`ruff`/`mypy`, `eslint`/`prettier` |

## 📁 Project Structure

```
.
├── services/
│   ├── gateway/        # Go — routing, JWT, CORS, rate-limit, MaxBytesReader 5GB, X-Internal-Token → :8080
│   ├── metadata/       # Go — CRUD /api/v1/videos + owner_email JOIN, DELETE + S3 cleanup, /internal/* InternalAuth → :8002
│   ├── upload/         # Go — multipart upload (MaxBytesReader 5GB, video/* allowlist) → MinIO raw/ + RabbitMQ → :8003
│   ├── auth/           # Python FastAPI — register/login/refresh/me → :8001
│   └── transcoder/     # Python pika + FFmpeg — sequential, fps preserve, audio.m4a, heartbeat 600 → :9000
├── frontend/           # Next.js 14 — /, /watch/[id] (owner badge, delete, hls.js seamless), /upload
├── deploy/
│   ├── docker-compose.yml          # dev + healthchecks (service_healthy) + migrate (golang-migrate)
│   ├── docker-compose.prod.yml     # CDN cache headers, resource limits cpus 2/mem 4G, nginx.prod.conf
│   ├── migrations/000001_init.up.sql # golang-migrate source of truth
│   ├── postgres/init.sql           # legacy fallback
│   └── nginx/{nginx.conf,nginx.prod.conf,Dockerfile} # envsubst INTERNAL_TOKEN
├── docs/               # spec.md, services-pipeline.md, ZED.md, SWAGGER.md
├── scripts/e2e.sh      # upload → poll ready → master.m3u8 → ffprobe aligned segments → gateway HLS
└── Makefile            # migrate-up/down/create, lint/test, e2e, swagger
```

## 🚀 Quick Start

**Prereqs:** Docker (OrbStack on macOS), `uv` for Python, `go` 1.27, `node` 20, `ffmpeg`/`ffprobe` for e2e

```bash
cp .env.example .env   # fill JWT_SECRET, INTERNAL_TOKEN, UPLOAD_MAX_BYTES=5GB, FFMPEG_THREADS/PRESET
make up                # docker compose --env-file .env -f deploy/docker-compose.yml up --build -d (migrate runs automatically)
make ps                # all services must be (healthy) + migrate completed
make logs              # follow logs
# production overrides (CDN headers, limits cpus 2/mem 4G):
# docker compose --env-file .env -f deploy/docker-compose.yml -f deploy/docker-compose.prod.yml up --build -d
# migrations (manual): make migrate-up / migrate-create name=add_visibility
```

Infra: `postgres :5432` · `minio :9000/:9001` (private raw, download renditions) · `rabbitmq :5672/:15672` · `nginx-vod :8081` · `gateway :8080` — all with `healthcheck` (`service_healthy`), `migrate` one-shot

**Without Docker (local dev):**

```bash
uv sync --project services/auth
uv sync --project services/transcoder

make migrate-up      # golang-migrate up (if postgres is running without compose)
make dev-auth        # uvicorn src.main:app --reload --port 8001
make dev-transcoder  # python -m app.consumer (pika, heartbeat 600)
make dev-metadata    # :8002 (needs MINIO_ENDPOINT, INTERNAL_TOKEN)
make dev-upload      # :8003
make dev-gateway     # :8080
make dev-frontend    # :3000
```

Verify: `curl http://localhost:8080/healthz` · open `http://localhost:3000` · upload via `POST /api/v1/videos/upload`

## 🔧 Development

| Command | Description |
|---------|-------------|
| `make lint-py && make lint-go && make lint-front` | lint all |
| `make fmt-py && make fmt-go` | format |
| `make test-go && make test-py` | tests |
| `make swagger` | generate OpenAPI (`swag init`) |
| `make e2e` | `scripts/e2e.sh` — upload → poll ready → master.m3u8 → ffprobe aligned segments → gateway HLS |

Zed IDE auto-fix on save — see [`.zed/settings.json`](.zed/settings.json) and [`docs/ZED.md`](docs/ZED.md) (`ruff`+`black` for Python, `gopls`+`gofumpt` for Go, `eslint`+`prettier` for TS).

## 🗺 Roadmap

Roadmap and tickets are tracked in the GitHub project.

## 🤝 Contributing

PRs welcome! Please use [Conventional Commits](https://www.conventionalcommits.org/) (`feat(auth): ...`, `fix(transcoder): ...`) and run lints/tests before pushing. Never commit `.env`, `__pycache__/`, `node_modules/`, `data/`.

## 📄 License

MIT — see [LICENSE](LICENSE) (add if missing).

---

<p align="center">
  Built with Go + Python · Star ⭐ if you like it!
</p>
