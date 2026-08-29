# Flowix — сервисы и пайплайн приложения

> Для ИИ-агентов: это главный документ про «зачем каждый сервис и как всё связано». ТЗ и стек — `docs/spec.md:1`, план по фазам — `docs/PLAN.md:1`. Гайдлайны поведения — `AGENTS.md:1`.

## 1. Зачем вообще такая архитектура

Flowix — MVP видеоплатформы с адаптивным стримингом (как YouTube: качество переключается без паузы, можно менять скорость). Чтобы это работало надёжно и масштабировалось, бэкенд разбит на микросервисы: каждый делает одну задачу хорошо, общается через HTTP/gRPC и очереди.

**Ключевые требования из ТЗ:**
- HLS/DASH с бесшовным переключением (выровненные сегменты, один кодек/фреймрейт)
- Загрузка → транскодинг → стриминг без блокировок (асинхронно)
- Масштабируемость (можно поднять N воркеров-транскодеров)
- Единая точка входа для фронтенда

---

## 2. Карта сервисов — кто за что отвечает

| Сервис | Язык / Фреймворк | Порт | За что отвечает | Хранит / читает |
|---|---|---|---|---|
| **gateway** | Go + `chi` | `:8080` | Единая точка входа. Роутит `/api/v1/auth/*` → auth, `/api/v1/videos/*` → metadata/upload, `/hls/*` → nginx-vod. CORS, rate-limit, JWT-проверка, `healthz` | Ничего не хранит, прокси |
| **auth** | Python FastAPI | `:8001` | Регистрация, логин, refresh, `GET /me`. Выпускает JWT (`HS256`, access 15м / refresh 7д, Argon2) | `postgres.users` |
| **metadata** | Go + `chi` | `:8002` | CRUD видео: `GET/POST /api/v1/videos`, `GET/PATCH/DELETE /api/v1/videos/:id`. Внутренний `PATCH /internal/videos/:id/status` для transcoder'а. Валидация, пагинация | `postgres.videos`, `video_renditions` |
| **upload** | Go + `chi` | `:8003` | Принимает `multipart/form-data` на `POST /api/v1/videos/upload`, льёт оригинал в MinIO `raw/{id}/original.mp4`, создаёт запись `status=uploaded` через metadata, публикует `video.uploaded` в RabbitMQ | MinIO + RabbitMQ + metadata |
| **transcoder** | Python Celery + FFmpeg | — (воркер) | Слушает `video.uploaded`, качает оригинал, `ffprobe` → 3× FFmpeg параллельно (360p/720p/1080p), льёт `renditions/{id}/{quality}.mp4` в MinIO, `PATCH metadata status=ready`, публикует `video.transcoded`. Тут же делает превью (`-ss 1 -vframes 1`) | MinIO, RabbitMQ, metadata |
| **streaming (nginx-vod)** | nginx + `kaltura/nginx-vod-module` | `:8081` | Отдаёт HLS/DASH на лету: `GET /hls/{id}/master.m3u8` склеивает 3 MP4 в мастер-манифест, сегменты режет по ключевым кадрам. JIT — храним только MP4, сегменты не прегенерим. `vod_mode mapped; proxy_pass minio:9000` | Читает MinIO |
| **frontend** | Next.js 14 + `hls.js` + `zustand` + `tailwind` | `:3000` | Лента `/`, просмотр `/watch/[id]` (`new Hls().loadSource(master.m3u8)` + `playbackRate 0.5–2x`), загрузка `/upload` (multipart + прогресс) | Gateway + HLS |
| **infra: Postgres** | `postgres:16-alpine` | `:5432` | `users`, `videos (status: uploaded/processing/ready/failed)`, `video_renditions` — см. `deploy/postgres/init.sql:1` | — |
| **infra: MinIO** | `minio/minio` | `:9000/:9001` | S3-совместимое хранилище: `raw/` оригиналы, `renditions/` готовые MP4, превью | — |
| **infra: RabbitMQ** | `rabbitmq:3-management` | `:5672/:15672` | Очередь `video.uploaded` / `video.transcoded`, Celery брокер. UI на `:15672` | — |

Каждый сервис — свой `Dockerfile` + `go.mod` / `pyproject.toml` (uv), собирается независимо. Локально — `make up` / `docker compose --env-file .env -f deploy/docker-compose.yml up --build -d`.

---

## 3. Пайплайн — как видео проходит от загрузки до просмотра

```
[Пользователь / Frontend :3000]
        │
        ▼
   ┌─────────┐  POST /api/v1/auth/register|login
   │  auth   │◄────── gateway :8080 ──────┐
   │  :8001  │  ──► JWT (access+refresh) ─┘
   └─────────┘
        │
        │  JWT в Authorization: Bearer
        ▼
   ┌─────────┐  POST /api/v1/videos/upload (multipart: file+title)
   │ gateway │ ──► upload :8003
   └─────────┘         │
                       ├─1 validate JWT → X-User-ID
                       ├─2 POST metadata /internal/videos → videos.status=uploaded
                       ├─3 PutObject MinIO raw/{video_id}/original.mp4
                       ├─4 Publish RabbitMQ video.uploaded {video_id, s3_key, owner_id}
                       └─5 201 {id, status: uploaded} → gateway → frontend
                               │
                               ▼
                        ┌──────────┐ RabbitMQ video.uploaded
                        │transcoder│ Celery worker
                        └──────────┘   │
                               ├─ download raw/{id}/original.mp4
                               ├─ ffprobe (fps/duration проверка)
                               ├─ 3× FFmpeg параллельно (см. §4)
                               │    -vf scale=-2:360 -b:v 800k ...
                               │    -vf scale=-2:720 -b:v 2500k
                               │    -vf scale=-2:1080 -b:v 5000k
                               ├─ PutObject renditions/{id}/360.mp4, 720.mp4, 1080.mp4
                               ├─ thumbnail -ss 1 -vframes 1 → thumbs/{id}.jpg
                               ├─ PATCH metadata /internal/videos/:id/status → ready
                               └─ Publish video.transcoded {video_id, renditions[], status}
                                        │
                                        ▼
                               ┌──────────────────┐
                               │    metadata      │  videos.status=ready
                               │    :8002         │  video_renditions (quality, bitrate, s3_key)
                               └──────────────────┘
                                        │
                                        ▼
   ┌──────────┐  GET /hls/{id}/master.m3u8
   │  nginx   │◄── frontend hls.js (через gateway /hls/*)
   │  -vod    │  ──► vod_mode mapped → proxy_pass minio:9000
   │  :8081   │  склеивает 3 MP4 → HLS манифест + сегменты .m4s
   └──────────┘
        │
        ▼
   frontend /watch/[id]: Hls.js переключает 360↔720↔1080 без паузы,
   playbackRate 0.5–2x, лента берёт GET /api/v1/videos из metadata
```

### Статусы видео

`uploaded` → (transcoder забрал) → `processing` → `ready` | `failed`. Frontend поллит `GET /api/v1/videos/:id` пока не `ready`.

### События (контракты)

- `video.uploaded {video_id: UUID, s3_key: string, owner_id: UUID}`
- `video.transcoded {video_id: UUID, renditions: [{quality, s3_key, bitrate}], status: ready|failed}`

Определены как Go struct + Pydantic — см. `docs/PLAN.md:73` и `deploy/postgres/init.sql:1`.

---

## 4. Почему транскодинг именно такой — бесшовное переключение

Чтобы HLS мог переключать качество без склейки, все рипы должны иметь **одинаковый фреймрейт, кодек и выровненные ключевые кадры**:

```bash
-c:v libx264 -profile:v high -pix_fmt yuv420p -r 30 -g 60 -keyint_min 60 \
-sc_threshold 0 -force_key_frames "expr:gte(t,n_forced*2)" -c:a aac
# 360p: -vf scale=-2:360 -b:v 800k -maxrate 856k -bufsize 1200k -b:a 96k
# 720p: -b:v 2500k
# 1080p: -b:v 5000k
```

- `-g 60 -keyint_min 60` + `-force_key_frames ...*2` = ключевой кадр каждые 2 сек — сегменты HLS по 2с совпадают на всех качествах.
- `-sc_threshold 0` отключает автовставку ключевых кадров на сцене.
- Храним 3 MP4, nginx-vod режет их на сегменты JIT — не нужно прегенерить `.ts` заранее.

---

## 5. Как фронтенд проигрывает

1. `GET /api/v1/videos` → лента.
2. `GET /api/v1/videos/:id` → мета + `status`.
3. Если `ready`, `hls.js` грузит `/hls/{id}/master.m3u8` (через gateway). Внутри `EXT-X-STREAM-INF` на каждое качество.
4. Плеер сам выбирает битрейт по сети; ручное переключение — без перезагрузки плеера. `video.playbackRate` для скорости.

---

## 6. Где что лежит в репо

```
services/gateway|metadata|upload  — Go chi (1.27-alpine), см. их README.md
services/auth                     — FastAPI + uv, src/routers/auth.py
services/transcoder               — Celery app/{celery_app.py,tasks.py}
frontend/                         — Next 14 App Router
deploy/docker-compose.yml         — infra + все сервисы (healthcheck service_healthy)
deploy/docker-compose.prod.yml    — prod overrides (CDN headers, limits)
deploy/nginx/{nginx.conf,nginx.prod.conf,Dockerfile} — vod_mode mapped, CDN cache
deploy/postgres/init.sql          — DDL users/videos/renditions
docs/PLAN.md                      — 8 фаз план (0–8 завершены)
docs/spec.md                      — полный архив ТЗ (бывший AGENTS.md)
docs/SWAGGER.md                   — как генерить OpenAPI для Go
docs/ZED.md                       — настройки Zed IDE
scripts/e2e.sh                    — upload→ready→master.m3u8→ffprobe→gateway HLS
```

---

## 7. Типичные сценарии (для отладки)

- **Загрузка падает**: проверь `upload` логи, MinIO `:9001`, RabbitMQ `:15672` очереди, `metadata` — создалась ли запись `uploaded`.
- **Транскодер не берёт задачу**: `make dev-transcoder` логи, `RABBITMQ_URL` в `.env`, FFmpeg установлен (`ffmpeg -version`), права MinIO.
- **HLS 404 / нет сегментов**: `curl :8081/hls/{id}/master.m3u8` должен вернуть `EXT-X-STREAM-INF` ×3. Проверь `renditions/` в MinIO и `nginx.conf` `vod_upstream_location`.
- **Gateway 502**: сервис за ним не поднят — `make ps`, `make logs`.

См. также `scripts/e2e.sh` — прогоняет `upload → poll ready → curl master.m3u8 → ffprobe aligned segments (EXTINF cross-check, h264) → gateway /hls` (фаза 8). Prod-пайплайн — `docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.prod.yml up --build -d` (CDN `Cache-Control` для manifests/segments).
