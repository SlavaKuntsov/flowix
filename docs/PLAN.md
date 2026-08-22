# Flowix — План реализации (утвержден)

> Дата: 2026-08-22
> Стек утвержден: RabbitMQ + chi (Go) + nginx-vod JIT + Next.js 14
> Источник ТЗ: `AGENTS.md`

## Утвержденные решения

| Вопрос | Выбор | Обоснование |
|---|---|---|
| Очередь | **RabbitMQ** | Celery + Go `amqp091-go` из коробки, management UI `:15672`, предпочтительно в `AGENTS.md:21` |
| Go-фреймворк | **chi** `go-chi/chi/v5` | Легковесный, единообразно для gateway/metadata/upload, соответствует `AGENTS.md:56` |
| HLS упаковка | **JIT nginx-vod-module** | `AGENTS.md:84 preferred: store single MP4 per rendition`. Храним 3 MP4, nginx режет на сегменты по keyframes. Fallback — pre-segment через FFmpeg |
| Frontend | **Next.js 14 App Router** | Без Vite (`Next` уже с bundler), `hls.js` + `zustand` |

## Граф зависимостей

```
[Infra: Postgres, MinIO, RabbitMQ, Docker] 
        ↓
[Auth (Py)] ←→ [Metadata (Go)]  — независимы
        ↓         ↓
     [Upload (Go)] → MQ → [Transcoder (Py)] → MinIO
                            ↓
                     [Streaming nginx-vod] ← MinIO
                            ↓
                       [Gateway (Go)] ← Auth/Metadata/Upload
                            ↓
                       [Frontend (Next)] ← Gateway + /hls
```

Порядок = снизу вверх, чтобы не мокать зависимости.

---

## Фаза 0 — Фундамент монорепо (0.5 дня)

**Цель:** скелет `AGENTS.md:32-46`, который разблокирует всех.

**Создать:**
```
.
├── services/gateway|metadata|upload|auth|transcoder
├── frontend/
├── deploy/{docker-compose.yml, postgres/init.sql, nginx/nginx.conf, nginx/Dockerfile}
├── scripts/
├── docs/
├── .env.example
├── .gitignore
└── Makefile
```

**`deploy/docker-compose.yml` infra:**
- `postgres:16-alpine` `:5432`, `minio/minio` `:9000/:9001`, `rabbitmq:3-management` `:5672/:15672`
- `nginx` (заглушка `nginx:alpine`, позже заменится на `nginx-vod`)

**`.env.example` (`AGENTS.md:131-136`):**
`DATABASE_URL`, `MINIO_ENDPOINT/ACCESS_KEY/SECRET_KEY`, `RABBITMQ_URL`, `JWT_SECRET`, `VIDEO_STORAGE_BUCKET`, `POSTGRES_DB`

**Проверка:** `docker compose -f deploy/docker-compose.yml up -d && curl :9000/minio/health/live && curl :15672 && psql $DATABASE_URL -c "select 1"`


## Фаза 1 — Контракты и БД

**Таблицы (`deploy/postgres/init.sql`):**
```sql
CREATE TYPE video_status AS ENUM ('uploaded','processing','ready','failed');
CREATE TABLE users (id UUID PRIMARY KEY, email TEXT UNIQUE NOT NULL, password_hash TEXT NOT NULL, created_at TIMESTAMPTZ DEFAULT now());
CREATE TABLE videos (id UUID PRIMARY KEY, owner_id UUID REFERENCES users(id), title TEXT, description TEXT, duration INT, status video_status NOT NULL, created_at TIMESTAMPTZ DEFAULT now());
CREATE TABLE video_renditions (video_id UUID REFERENCES videos(id) ON DELETE CASCADE, quality TEXT, bitrate INT, width INT, height INT, s3_key TEXT, PRIMARY KEY(video_id, quality));
```

**События (Go struct + Pydantic):**
- `video.uploaded {video_id: UUID, s3_key: string, owner_id: UUID}`
- `video.transcoded {video_id: UUID, renditions: {quality, s3_key, bitrate}[], status: ready|failed}`

**Миграции:** `golang-migrate` или `alembic` для Python-части (единый `DATABASE_URL`).

## Фаза 2a — Auth Service (Python, FastAPI) `AGENTS.md:70-75`

**Структура:** `services/auth/src/{main.py, routers/auth.py, models.py, schemas.py, core/{security.py,config.py}}`, `pyproject.toml`, `Dockerfile` `python:3.11-slim`

**Эндпоинты:** `POST /api/v1/auth/register`, `POST /api/v1/auth/login`, `POST /api/v1/auth/refresh`, `GET /api/v1/auth/me` (защищен)

**JWT:** `HS256`, `JWT_SECRET`, access 15m / refresh 7d, `passlib[argon2]`

**Проверка:** `pytest` + `curl :8001/api/v1/auth/login → {access_token}`

## Фаза 2b — Metadata Service (Go, chi) `AGENTS.md:60-65`

**Структура:** `services/metadata/cmd/server/main.go`, `internal/{handler,service,repository,model}`, `go.mod` + `chi`, `pgx`, `zerolog`, `validator`

**API:**
- `GET /api/v1/videos` `POST /api/v1/videos` `GET/PATCH/DELETE /api/v1/videos/:id` (JWT)
- `PATCH /internal/videos/:id/status` (без auth, для transcoder)

**Dockerfile** multi-stage `golang:1.22-alpine`

## Фаза 3 — Upload Service (Go, chi) `AGENTS.md:66-68`

**Зависит от:** MinIO + RabbitMQ + Metadata + Auth

**Логика `POST /api/v1/videos/upload` (multipart):**
1. validate JWT → `X-User-ID`
2. `POST metadata /internal/videos` → `status=uploaded`
3. `PutObject` `raw/{video_id}/original.mp4` в MinIO
4. `Publish video.uploaded` в RabbitMQ
5. `201 {id, status}`

**Resumable (`Content-Range`) — отложить после MVP.**

## Фаза 4 — Transcoder Worker (Celery + RabbitMQ) `AGENTS.md:76-85`

**Структура:** `services/transcoder/app/{celery_app.py,tasks.py,schemas.py,minio_client.py}`, `Dockerfile` `python:3.11 + ffmpeg`

**Воркер:**
`consume video.uploaded → download raw → ffprobe (fps/duration) → 3× ffmpeg параллельно → upload renditions/{id}/{360,720,1080}.mp4 → PATCH metadata ready → publish video.transcoded`

**FFmpeg обязательно (`AGENTS.md:82-83`):**
```bash
-c:v libx264 -profile:v high -pix_fmt yuv420p -r 30 -g 60 -keyint_min 60 -sc_threshold 0 -force_key_frames "expr:gte(t,n_forced*2)" -c:a aac
# + -vf scale=-2:360 -b:v 800k -maxrate 856k -bufsize 1200k -b:a 96k
# + 720p: -b:v 2500k / 1080p: -b:v 5000k
```
Одинаковый `-r` и `-c:v` для seamless switching.

**Thumbnails** в этом же воркере (`-ss 1 -vframes 1`), отдельный сервис не нужен для MVP.

## Фаза 5 — Streaming nginx-vod JIT `AGENTS.md:86-90`

**`deploy/nginx/nginx.conf`:**
- `vod_mode mapped; vod_upstream_location /minio; proxy_pass http://minio:9000;`
- Роуты: `GET /hls/{video_id}/master.m3u8` → nginx склеивает 3 MP4, `GET /hls/{id}/{quality}/segment.m4s`

**`deploy/nginx/Dockerfile`** — сборка `nginx` с `kaltura/nginx-vod-module`

**Проверка:** `curl :8080/hls/{id}/master.m3u8` содержит 3× `EXT-X-STREAM-INF`, `hls.js` переключает без паузы.

## Фаза 6 — Gateway (Go, chi) `AGENTS.md:55-58`

**`services/gateway`:** `chi` + `httputil.ReverseProxy` + middleware `CORS`, `RateLimit (golang.org/x/time/rate)`, `JWT Auth`, `zerolog`, `healthz`

**Роутинг:**
- `/api/v1/auth/*` → `auth:8000`
- `/api/v1/videos/*` → `metadata:8002` / `upload:8003`
- `/hls/*` → `nginx:8080`

Все через `:8080`, версионирование `AGENTS.md:155` `/api/v1`.

## Фаза 7 — Frontend (Next 14) `AGENTS.md:10,26`

**`frontend`:** `Next 14 App Router + TypeScript`, `hls.js`, `zustand`, `tailwind`, `eslint/prettier`

**Страницы:** `/` (лента), `/watch/[id]` (`new Hls().loadSource(master.m3u8)` + `playbackRate 0.5-2x`), `/upload` (multipart + прогресс)

## Фаза 8 — Интеграция и харденинг

- `scripts/e2e.sh`: `upload → poll status=ready → curl master.m3u8 → ffprobe проверка aligned segments`
- Линт `AGENTS.md:223-238`: `gofmt`, `golangci-lint`, `black`, `flake8`, `mypy`, `npm run lint`
- `docker compose up --build` полный пайплайн, `healthcheck` для каждого сервиса, `deploy/docker-compose.prod.yml` (CDN заголовки)

---

## Ближайший шаг

Фаза 0-1: скелет + infra + БД. После этого 2a и 2b параллельно.
