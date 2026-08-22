
# flowix — video streaming platform MVP

> План: `docs/PLAN.md` | ТЗ: `AGENTS.md`
> Стек: Go `chi` + Python `FastAPI` + RabbitMQ + MinIO + nginx-vod JIT + Next.js

## Быстрый старт (OrbStack + uv)
```bash
# OrbStack уже активен: context orbstack (docker ps работает)
# Если нет: orb start && docker context use orbstack

cp .env.example .env
make up      # docker compose --env-file .env -f deploy/docker-compose.yml up --build -d (через OrbStack)
make logs
make ps
```

Infra: postgres `:5432`, minio `:9000/:9001`, rabbitmq `:5672/:15672`, nginx-vod `:8081`, gateway `:8080`

### Python через uv
```bash
uv sync --project services/auth         # установка
uv sync --project services/transcoder
make dev-auth        # uv run --project services/auth uvicorn src.main:app --reload --port 8001
make dev-transcoder  # uv run --project services/transcoder celery -A app.celery_app worker --loglevel=info
# Docker тоже на uv: см. services/auth/Dockerfile:1 (ghcr.io/astral-sh/uv)
```

## Структура
См. `docs/PLAN.md` — 8 фаз. Zed настройки: `.zed/settings.json:1`, доки `docs/ZED.md:1`.

## Команды по проектам

| Сервис | Запуск | Линт/формат/фикс | Доки |
|---|---|---|---|
| Auth (uv) | `make dev-auth` / `docker compose up auth` | `make lint-py` / `make fix-py` | `services/auth/README.md:1` |
| Transcoder (uv) | `make dev-transcoder` | `make lint-py` | `services/transcoder/README.md:1` |
| Metadata (Go) | `docker compose up metadata` / `go run ./services/metadata/cmd/server` | `make lint-go` / `make fmt-go` | `services/metadata/README.md:1` |
| Upload (Go) | `docker compose up upload` | `make lint-go` | `services/upload/README.md:1` |
| Gateway (Go) | `docker compose up gateway` | `make lint-go` | `services/gateway/README.md:1` |
| Frontend (Next) | `cd frontend && npm run dev` | `make lint-front` / `make fmt-front` | `frontend/README.md:1` |
| Zed IDE | авто-формат при `Cmd+S` | — | `docs/ZED.md:1` |

## Zed IDE — авто-фикс при сохранении
Включено в `.zed/settings.json:1`: `Python` (pyright+ruff+black), `Go` (gopls+gofumpt), `TS` (eslint+prettier). При `Cmd+S` — формат, сортировка импортов, fix `F/E/W`. Рефактор: `F2` rename, `Cmd+.` quick fix. См. `docs/ZED.md:1`.

## Текущий статус
- [x] Фаза 0 — скелет, `deploy/docker-compose.yml:1`, `.env.example:1`
- [x] Фаза 1 — `deploy/postgres/init.sql:1`
- [x] Фаза 2 — Auth (FastAPI+uv) + Metadata/Upload/Gateway stubs (chi 1.23) — все линты зеленые
- [ ] Фаза 3-7 — реализация бизнес-логики

## Проверка всех проектов (последний прогон 2026-08-22)
```bash
make lint-py   # black --check + flake8 + mypy — PASS (services/auth/src:10, services/transcoder/app:1)
make lint-go   # gofmt + go vet — PASS (3 сервиса, golang:1.23)
docker run nginx:alpine nginx -t  # PASS (deploy/nginx/nginx.conf:1)
docker compose --env-file .env -f deploy/docker-compose.yml config  # PASS
cat .zed/settings.json | python3 -m json.tool  # PASS
```
