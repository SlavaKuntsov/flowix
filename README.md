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

- **Adaptive HLS streaming** — 360p / 720p / 1080p renditions, aligned segments (`-force_key_frames`, `-sc_threshold 0`), one codec / framerate
- **JIT packaging** — only 3 MP4s stored, HLS/DASH segmented on-the-fly by `nginx-vod` (`vod_mode mapped`)
- **Async pipeline** — `upload → RabbitMQ (video.uploaded) → Celery + FFmpeg → MinIO → metadata (ready) → HLS`
- **Microservices** — Go (`chi`) for gateway/metadata/upload, Python (`FastAPI` + `Celery`) for auth/transcoder
- **Modern frontend** — Next.js 14 App Router + `hls.js` + `zustand` + `tailwind`, `playbackRate 0.5–2×`
- **Production-ready infra** — Postgres 16, MinIO, RabbitMQ, Docker Compose, healthchecks, JWT (HS256, Argon2)

## 🏗 Architecture

```
[ Next.js :3000 ] ──► [ Gateway :8080 (Go/chi) ] ─┬─► [ Auth :8001 (FastAPI) ] ──► Postgres
                                                  ├─► [ Metadata :8002 (Go) ] ──► Postgres
                                                  ├─► [ Upload :8003 (Go) ] ──► MinIO + RabbitMQ
                                                  └─► [ nginx-vod :8081 ] ──► MinIO (renditions)

[RabbitMQ] video.uploaded ──► [Transcoder (Celery + FFmpeg)] ──► MinIO renditions/{id}/{360,720,1080}.mp4
                                                     └─► PATCH /internal/videos/:id/status → metadata
                                                     └─► video.transcoded
```

> Full breakdown: [`docs/services-pipeline.md`](docs/services-pipeline.md) (services & pipeline, RU) · Spec: [`docs/spec.md`](docs/spec.md) · Plan: [`docs/PLAN.md`](docs/PLAN.md)

## 🧩 Tech Stack

| Layer | Tech |
|-------|------|
| Gateway / Metadata / Upload | Go 1.27, `go-chi/chi/v5`, `pgx`, `zerolog`, `amqp091-go` |
| Auth / Transcoder | Python 3.14, FastAPI, Celery, `uv`, `argon2`, `aiobotocore`, `ffmpeg-python` |
| Infra | Postgres 16, MinIO, RabbitMQ 3, `nginx-vod-module`, FFmpeg |
| Frontend | Next.js 14, `hls.js` 1.5, `zustand` 4, Tailwind |
| Tooling | Docker Compose, `golangci-lint`, `black`/`ruff`/`mypy`, `eslint`/`prettier` |

## 📁 Project Structure

```
.
├── services/
│   ├── gateway/        # Go — routing, JWT, CORS, rate-limit → :8080
│   ├── metadata/       # Go — CRUD /api/v1/videos → :8002
│   ├── upload/         # Go — multipart upload → MinIO raw/ + RabbitMQ → :8003
│   ├── auth/           # Python FastAPI — register/login/refresh/me → :8001
│   └── transcoder/     # Python Celery + FFmpeg — video.uploaded consumer
├── frontend/           # Next.js 14 — /, /watch/[id], /upload
├── deploy/
│   ├── docker-compose.yml          # dev + healthchecks (service_healthy)
│   ├── docker-compose.prod.yml     # CDN cache headers, resource limits, nginx.prod.conf
│   ├── postgres/init.sql
│   └── nginx/{nginx.conf,nginx.prod.conf,Dockerfile}
├── docs/               # spec.md, services-pipeline.md, PLAN.md, ZED.md, SWAGGER.md
├── scripts/e2e.sh      # upload → poll ready → master.m3u8 → ffprobe aligned segments → gateway HLS
└── Makefile
```

## 🚀 Quick Start

**Prereqs:** Docker (OrbStack on macOS), `uv` for Python, `go` 1.27, `node` 20, `ffmpeg`/`ffprobe` for e2e

```bash
cp .env.example .env   # fill JWT_SECRET etc.
make up                # docker compose --env-file .env -f deploy/docker-compose.yml up --build -d
make ps                # all services must be (healthy)
make logs              # follow logs
# production overrides (CDN headers, limits):
# docker compose --env-file .env -f deploy/docker-compose.yml -f deploy/docker-compose.prod.yml up --build -d
```

Infra: `postgres :5432` · `minio :9000/:9001` · `rabbitmq :5672/:15672` · `nginx-vod :8081` · `gateway :8080` — all with `healthcheck` (`service_healthy`)

**Without Docker (local dev):**

```bash
uv sync --project services/auth
uv sync --project services/transcoder

make dev-auth        # uvicorn src.main:app --reload --port 8001
make dev-transcoder  # celery -A app.celery_app worker --loglevel=info
make dev-metadata    # :8002
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

- [x] Phase 0–1 — infra & DB schema (`deploy/postgres/init.sql`)
- [x] Phase 2 — Auth + Metadata (chi + FastAPI, JWT HS256/Argon2)
- [x] Phase 3 — Upload (multipart → MinIO raw + `video.uploaded`)
- [x] Phase 4 — Transcoder (Celery + FFmpeg, aligned GOP `-g 60 -force_key_frames`, `-sc_threshold 0`)
- [x] Phase 5 — Streaming nginx-vod JIT (`vod_mode mapped`, 3 MP4 → HLS)
- [x] Phase 6 — Gateway (reverse proxy, CORS, rate-limit, JWT)
- [x] Phase 7 — Frontend (Next 14, `hls.js`, `/watch/[id]` + `playbackRate`)
- [x] Phase 8 — Integration & hardening (`scripts/e2e.sh` + `healthcheck` + `docker-compose.prod.yml` CDN)
- See [`docs/PLAN.md`](docs/PLAN.md) — 8 phases, event contracts `video.uploaded` / `video.transcoded`

## 🤝 Contributing

PRs welcome! Please use [Conventional Commits](https://www.conventionalcommits.org/) (`feat(auth): ...`, `fix(transcoder): ...`) and run lints/tests before pushing. Never commit `.env`, `__pycache__/`, `node_modules/`, `data/`.

## 📄 License

MIT — see [LICENSE](LICENSE) (add if missing).

---

<p align="center">
  Built with Go + Python · Star ⭐ if you like it!
</p>
