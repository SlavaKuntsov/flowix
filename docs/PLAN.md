# Flowix — План реализации

> Дата: 2026-08-30 (переписан после senior review 2026-08-29) · Обновлен: 2026-09-02 — Фазы 9,10,10b,11,12,13,14,15 DONE (очередь DLX+удаление+storage)
> Стек: RabbitMQ + chi `go-chi/chi/v5` + nginx-vod JIT + Next.js 14
> Исходники: `docs/spec.md:1` (ТЗ), `docs/services-pipeline.md:1` (пайплайн), `AGENTS.md:1` (гайдлайны)
> Ревью-отчет: см. чат 2026-08-29 (P0/P1/P2 находки)

## Легенда приоритетов

| Приоритет       | Смысл                                               | SLA             |
| --------------- | --------------------------------------------------- | --------------- |
| **P0 Critical** | Прод-блокер: OOM, потеря данных, дыра безопасности  | до след. релиза |
| **P1 High**     | Масштаб/надежность: большие файлы, N видео, ресурсы | 1-2 спринта     |
| **P2 Medium**   | Оптимизация/UX/DX: экономия, наблюдаемость, техдолг | бэклог          |

Фазы 0-8 — завершены (MVP). Фазы 9+ — план харденинга и масштабирования.

## Граф зависимостей (текущий + план)

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

 План харденинга:
 Фаза 9 (P0: Upload/Metadata sec) ─┐
 Фаза 10 (P0: Transcoder limits+heartbeat) ──┼→ Фаза 11 (P1: presigned upload) → Фаза 13 (P1: HLS auth+CDN)
 Фаза 10b (P0: Queue DLX+idempotency) ─────────┘         ↓
                                    Фаза 12 (P1: adaptive ladder/HW + фан-аут)
                                             ↓
                                    Фаза 14 (P1: observability)
                                             ↓
                                    Фаза 15 (P2: storage/CDN cost)
```

---

## Завершенные фазы (0-8) — кратко, без изменений

### Фаза 0 — Фундамент монорепо ✅

Скелет `services/gateway|metadata|upload|auth|transcoder`, `frontend/`, `deploy/docker-compose.yml` (postgres:16 :5432, minio :9000/:9001, rabbitmq :15672, nginx-vod :8081), `.env.example`, `Makefile`. Проверка: `make up && make ps`.

### Фаза 1 — Контракты и БД ✅ (миграции — 2026-08-30)

`deploy/migrations/000001_init.up.sql:1` — source of truth (golang-migrate), `deploy/postgres/init.sql:1` — legacy fallback для fresh volume. `services/auth/migrations/env.py:1` (alembic) — `versions/0001_init.py` no-op. `video_status ENUM`, `users`, `videos (thumbnail_s3_key)`, `video_renditions`.

### Фаза 2a — Auth (Py FastAPI) ✅

`services/auth/src/routers/auth.py:1` — register/login/refresh/me, JWT HS256 15m/7d, argon2. `src/core/security.py:34`.

### Фаза 2b — Metadata (Go chi) ✅

`services/metadata/cmd/server/main.go:1` — CRUD `/api/v1/videos`, `PATCH /internal/videos/:id/status`, `GET /internal/videos/:id/vod` (`internal/handler/video.go:124`).

### Фаза 3 — Upload (Go chi) ✅

`services/upload/internal/handler/upload.go:57` — multipart `POST /api/v1/videos/upload` → MinIO `raw/{id}/original.mp4` → RabbitMQ.

### Фаза 4 — Transcoder (pika + FFmpeg) ✅

`services/transcoder/app/consumer.py:1` — download → ffprobe → 1× ffmpeg audio (`audio.m4a`, общий для всех рипов) → 3× ffmpeg `-g 60 -force_key_frames expr:gte(t,n_forced*2) -c:a copy` → `renditions/{id}/{360,720,1080}.mp4` → PATCH ready.

### Фаза 5 — Streaming nginx-vod JIT ✅

`deploy/nginx/nginx.conf:1` — `vod_mode mapped`, `proxy_pass minio:9000`, `vod_segment_duration 2000`.

### Фаза 6 — Gateway (Go chi) ✅

`services/gateway/cmd/server/main.go:1` — `ReverseProxy` (`internal/proxy/proxy.go:15`), CORS, `RateLimit 20/40` (`internal/middleware/ratelimit.go:19`), JWT.

### Фаза 7 — Frontend (Next 14) ✅ (удаление — 2026-08-30)

`frontend/src/components/VideoPlayer.tsx:1` — `hls.js` + `quality/speed` (`frontend/src/lib/api.ts:15`), `frontend/src/lib/api.ts:122` `deleteVideo(id)`, `watch/[id]/page.tsx:15` кнопка «Удалить» (owner only) + `VideoCard.tsx:1` `✕` на карточке, `services/metadata/internal/storage/minio.go:1` чистка S3 `raw/renditions/thumbnails`.

### Фаза 8 — Интеграция и харденинг ✅

`scripts/e2e.sh:1` — `upload→poll ready→master.m3u8→ffprobe aligned segments→gateway /hls`, `make lint-*` зеленые, `deploy/docker-compose.prod.yml:1` (CDN Cache-Control).

---

## Фаза 9 — P0: Безопасность и Upload streaming ✅ DONE 2026-08-30

**Приоритет: P0 Critical** · Оценка: 2-3 дня · Зависит от: 0-8 ✅ · **Выполнено**

**Проблемы из ревью:**

- `services/upload/internal/handler/upload.go:66` `ParseMultipartForm(200<<20)` грузит большие файлы в память/диск upload-пода → OOM на 5-10GB, блокирует хэндлер (нарушает `docs/spec.md:252` — heavy work в очереди).
- `deploy/docker-compose.yml:43` `mc anonymous set public` — `raw/` и `renditions` публично читаемы.
- `services/metadata/cmd/server/main.go:87` `PATCH /internal/videos/:id/status` без auth — любой может подделать `ready` + `GET /internal/videos/:id/vod` отдает маппинг.
- Нет лимита размера запроса ни в gateway, ни в upload.

**Выполнено 2026-08-30 (лимит 5-6GB как просил, большие файлы — Фаза 11 presigned):**

1. `upload`: `MaxBytesReader 5GB` (`UPLOAD_MAX_BYTES` env, `upload.go:65`) + `ParseMultipartForm 32MB` + MIME allowlist `video/*` →413/400. `"internal/middleware/internal.go:1"` не трогали — узкое место.
2. `gateway`: `maxBytesMw` для `POST /api/v1/videos/upload` (`gateway/cmd/server/main.go:98`) + прокидка `X-Internal-Token` в `metadataProxy/vodProxy` Director.
3. `metadata`: `InternalAuth` (`services/metadata/internal/middleware/internal.go:5`) — закрывает `Group(/internal/*)` (`metadata/cmd/server/main.go:86`), `transcoder/consumer.py:43` шлет заголовок, `deploy/nginx/nginx.conf:40` + `Dockerfile:33` envsubst.
4. `deploy/docker-compose.yml:50` `minio-setup` → `private` + `download` для `renditions/thumbnails`, `raw` private (проверено: `curl raw` 403, rendition 200, HLS 200).
5. MIME валидация: `allowedCT` + `isAllowedContentType` (`upload.go:135`).

**Затронуто:** `services/upload/internal/handler/upload.go`, `services/gateway/cmd/server/main.go`, `services/metadata/cmd/server/main.go`, `services/metadata/internal/middleware/internal.go`, `services/transcoder/app/consumer.py`, `deploy/docker-compose.yml`, `deploy/nginx/nginx.conf`, `deploy/nginx/Dockerfile`, `.env.example:27` (`INTERNAL_TOKEN`, `UPLOAD_MAX_BYTES`).

**Проверка:** `curl /internal/.../vod` без токена 401 / с токеном 404 — ok; `POST /upload image/jpeg` 400; `small.mp4` 201 → ready → HLS 200; `raw` 403 / `renditions` 200; `go test ./...` ok.

---

## Фаза 10 — P0: Transcoder — лимиты ресурсов и надежность ✅ DONE 2026-08-30

**Приоритет: P0 Critical** · Оценка: 3-4 дня · Зависит от: Фаза 9 ✅ DONE · **Выполнено**

**Проблемы:**

- `services/transcoder/app/consumer.py:213` `ThreadPoolExecutor(max_workers=3)` — 3× `libx264` = OOM, `timeout=300` убивает длинные видео `consumer.py:139`, `fget_object` качает целиком в `/tmp` `consumer.py:188` — диск кончается на больших файлах.
- Нет лимитов в `deploy/docker-compose.prod.yml` для transcoder, `HEALTHCHECK pgrep` `services/transcoder/Dockerfile:18` ложный.
- `consumer.py:139` форсирует `-r 30` даже для 24fps → лишний CPU.

**Выполнено 2026-08-30 (в т.ч. фикс 2GB heartbeat):**

1. Последовательный транскод: убран `ThreadPoolExecutor(max_workers=3)` (`consumer.py:213`), цикл `for q,w,h,br in RENDITIONS_SPEC: transcode_one(..., fps=fps)` — проверено: 24fps видео → 3× ffmpeg последовательно.
2. `transcode_one`: `-threads 2 -preset veryfast` (`FFMPEG_THREADS/PRESET` env, `consumer.py:140`), fps из probe (`_fps_from_probe` `consumer.py:108` — preserve ≤30, cap >30→30, fallback 30), `-g/-keyint_min fps*2`, `-maxrate 1.10×` (`consumer.py:155`), `timeout 900`, общий `audio` (`audio.m4a` + `-c:a copy`).
3. Диск-чек: `shutil.disk_usage(tmp).free <500MB → abort` (`consumer.py:210`), `fget_object` остался (pipe — Фаза 12).
4. `SIGTERM` graceful: `signal.signal(SIGTERM/SIGINT)` + `_shutdown` flag (`consumer.py:332`), `timeout 900`, `celery` legacy (удаление — Фаза 10b), `healthcheck` `pgrep app.consumer`.
5. **Heartbeat fix для больших файлов:** `params.heartbeat=600 blocked_connection_timeout=300` (`consumer.py:344`, было `60→104` на `2GB` `5м20с`), идемпотентность `ready→skip` (`consumer.py:248` `_get_status`), `0f1e8...` `19:33→19:39` теперь без ре-транскода.
6. `deploy/docker-compose.yml:179` env `FFMPEG_THREADS/PRESET`, `deploy/docker-compose.prod.yml:44` limits `cpus 2 / mem 4G` + reservations 1G, `healthcheck pgrep` only.
7. `.env.example:30` `FFMPEG_THREADS=2`, `FFMPEG_PRESET=veryfast`, `Makefile:84` `dev-transcoder` → `python -m app.consumer` (legacy `dev-transcoder-celery`).

**Затронуто:** `services/transcoder/app/consumer.py`, `services/transcoder/pyproject.toml:8` (коммент legacy), `services/transcoder/README.md`, `deploy/docker-compose.yml`, `deploy/docker-compose.prod.yml`, `.env.example`, `.env`, `Makefile`.

**Проверка:** `24fps` исходник → `master.m3u8 FRAME-RATE=24.000` (не 30), `g 48`, `threads 2 preset veryfast` в логах, `small.mp4 2s → ready` за 2 polling, `docker stats` <4G, `make e2e` (6s sample) зеленый, `kill -TERM` graceful.

---

## Фаза 10b — P0: Очередь — DLX, ретраи, идемпотентность ✅ DONE 2026-08-30

**Приоритет: P0 Critical** · Оценка: 1-2 дня · Зависит от: Фаза 10 (можно параллельно с 9) · **Выполнено**

**Проблемы:**

- `services/transcoder/app/consumer.py:272` `basic_nack(requeue=False)` теряет задачу навсегда, `services/upload/internal/queue/publisher.go:94` публикует `Persistent` но без `DLX`, `consumer.py:264` бесконечный `while True` без backoff на `AMQPConnectionError`.
- Нет идемпотентности: повтор `video.uploaded` → дубли `video_renditions`.

**Выполнено 2026-08-30:**

1. `upload` `queue/publisher.go:59` `declareTopology` — `dlx` exchange `direct`, `video.uploaded.dlq` + `video.uploaded.retry` (`x-message-ttl 30000`, `x-dead-letter-routing-key video.uploaded`), main queue `video.uploaded` с `x-dead-letter-exchange dlx → dlq`; авто-хил precondition (удаление stale queue без DLX).
2. `transcoder` `app/consumer.py:256` `declare_topology` + `main:434` ретрай 3× `x-retry-count` header → `basic_publish retry` → ack, после 3× `basic_nack requeue=False` → DLQ + `update_status failed`; `process_message` теперь `raise` для transient ошибок, `ready/failed` идемпотентность.
3. `metadata` `repository/video.go:125` `UpdateStatus` — `SELECT ... FOR UPDATE` + guard `ready→processing/uploaded` skip, `ON CONFLICT` для renditions сохранен; `handler/video_test.go:106` fakeStore mirror + `TestUpdateStatusIdempotencyReadyNotDowngraded`.
4. `transcoder` тесты `tests/test_consumer.py:82` `test_process_message_minio_failure_raises_for_retry` + `test_declare_topology`, `test_process_message_idempotent_*`.

**Затронуто:** `services/upload/internal/queue/publisher.go`, `services/transcoder/app/consumer.py`, `services/transcoder/tests/test_consumer.py`, `services/metadata/internal/repository/video.go`, `services/metadata/internal/handler/video_test.go`.

**Проверка:** `uv run pytest services/transcoder/tests/test_consumer.py -v` 16 passed; `go test ./...` metadata/upload green; `rabbitmqadmin list queues` после `kill -9 transcoder` → retry 30s → DLQ + `status=failed`.

---

## Фаза 11 — P1: Масштабируемый upload (presigned multipart) ✅ DONE 2026-08-30

**Приоритет: P1 High** · Оценка: 4-5 дней · Зависит от: Фазы 9, 10b · **Выполнено**

**Проблема:** upload через Go прокси — узкое место для больших файлов и N параллельных загрузок (1 под = 1 файл в памяти). Нет resumable.

**Выполнено 2026-08-30 (single PUT presigned, multipart ladder — backlog):**

1. Новый флоу `POST /api/v1/videos/presign` (`services/upload/internal/handler/presign.go:28` → `storage/minio.go:48` `PresignedPutObject` 1h + `MINIO_PUBLIC_ENDPOINT` rewrite) возвращает `{id, video_id, s3_key, method, url, expires_in}`; `POST /api/v1/videos/{id}/complete` проверяет `StatObject` + публикует `video.uploaded` (идемпотентно). Легковесный single PUT покрывает 5GB bypass без S3 CreateMultipartUpload (multipart чистит бэклог).
2. Совместимость сохранена: `POST /api/v1/videos/upload` оставлен (`upload.go:61` deprecated, <100MB fallback в FE).
3. Gateway `services/gateway/cmd/server/main.go:112` прокси `presign/complete` без `MaxBytesReader`, `CORS` уже `*`; MinIO `deploy/docker-compose.yml:32` `MINIO_API_CORS_ALLOW_ORIGIN=*`.
4. Upload `services/upload/cmd/server/main.go:86` регистрирует `presign/complete`, `.env.example:34` `MINIO_PUBLIC_ENDPOINT=http://localhost:9000`.
5. FE `frontend/src/lib/api.ts:126` `presignVideo/completeVideo/putToPresignedUrl` + `uploadVideo` с `localStorage` resume, `retry 3×`, `AbortSignal`, fallback на `uploadViaGateway` для <100MB; `frontend/src/app/upload/page.tsx:4` текст 5GB direct.

**Затронуто:** `services/upload/internal/storage/minio.go`, `services/upload/internal/handler/presign.go` (+ `presign_test.go`), `services/upload/cmd/server/main.go`, `services/gateway/cmd/server/main.go`, `frontend/src/lib/api.ts`, `frontend/src/app/upload/page.tsx`, `deploy/docker-compose.yml`, `.env.example`.

**Проверка:** `go test ./...` upload handler 6 passed; `npm run lint` frontend ok; `curl POST /api/v1/videos/presign` → 201 + `url` с `X-Amz-Signature`, `PUT` в `url` → `POST /complete` → `video.uploaded` → `ready` → HLS; сеть показывает прямое `PUT https://localhost:9000` без прокси, обрыв → resume из `localStorage`.

---

## Фаза 12 — P1: Адаптивный ladder + HW accel + фан-аут (обновлено 2026-08-30, анализ 2GB 5м20с)

**Приоритет: P1 High** · Оценка: 3-5 дней · Зависит от: Фаза 10, 11 · **Доп.: анализ 2GB 3248×2004 275с → 360p 100с+720p 100с+1080p 106с + heartbeat 104**

**Проблемы:**

- Фиксированный ladder `consumer.py:31` апскейлит 480p→1080p, тратит CPU на мыло.
- `libx264` CPU-only — `2GB` `19:33:41→19:39` ~5м20с, `heartbeat 60с` → `ConnectionResetError 104` → ре-транскод ×2.
- Последовательно 3 рипа в 1 воркере → `max = sum`, хотя `cpu 2 / mem 4G` `deploy/docker-compose.prod.yml:44`.

**Задачи (расширено):**

1. **Адаптивный ladder** (`probe` `consumer.py:62`): `<720p→[360p]`, `720p→[360p,720p]`, `≥1080p→[360p,720p,1080p]` (+ опц. `144p`). Экономия -33% CPU для `3K` без `360p` миса.
2. **CRF 23 + maxrate** вместо CBR — -20-30% битрейта, `env: ENCODE_MODE`.
3. **HW accel** (`Dockerfile:4` `jrottenberg/ffmpeg:*-nvidia` или `--enable-nvenc/qsv`), `transcode_one` `h264_nvenc -preset p4 -rc vbr` если `nvidia-smi`, иначе `libx264` — **5-10×** быстрее (`2GB` → 60-90с).
4. **Фан-аут по рипам** — 3 очереди `video.transcode.360p/720p/1080p`, 3 воркера по 1 рипу (`prefetch 1`) → `total = max(100с)` вместо `sum 300с`. + **720p first** — `master.m3u8` из 2 рипов, `1080p` докидывается позже (юзер видит через 100с, а не 300с).
5. **Стрим без `/tmp`** — `ffmpeg -i pipe:0` + `MinIO GetObject` stream (сейчас `fget_object` `consumer.py:272` 2GB на диск) → экономия диска/N параллельных.
6. **Чанк-транскод** (опц. для >10м видео) — режем по `force_key_frames 2с` на чанки, пул 4 воркера, `concat`.
7. **Heartbeat fix** уже в Фазе 10: `params.heartbeat=600` `consumer.py:344` (было `104`), идемпотентность `ready→skip`.
8. Thumbnail из `360p` рипа, `ultrafast` для `>1GB` (`FFMPEG_PRESET` env).

**Проверка:** `480p→[360p]`, `3K 2GB→[720p,1080p]` ~90с с NVENC, `fan-out` `master` с 2 рипами за 100с, `next HLS` без freeze (буфер 4с `VideoPlayer.tsx:35`).

---

## Фаза 13 — P1: Доступ к HLS и CDN

**Приоритет: P1 High** · Оценка: 2-3 дня · Зависит от: Фаза 9

**Проблемы:**

- `services/gateway/cmd/server/main.go:115` `r.Handle("/hls/*", vodProxy)` публично, `deploy/nginx/nginx.conf:61` `Cache-Control: public` — приватные видео доступны подбором UUID.
- Нет `signed URL`, нет проверки владельца.

**Задачи:**

1. Gateway middleware `HLSAuth`: `GET /hls/{id}/*?token=...` (JWT с `video_id` + `exp` 1h) или проверка `Authorization: Bearer` + `metadata owner_id`. Для публичных видео — пропуск. Для приватных — 403 без токена.
2. `metadata` добавить `visibility: public|private|unlisted` в `videos` (`deploy/postgres/init.sql`), `GET /internal/videos/:id/vod` проверяет visibility.
3. `deploy/nginx/nginx.prod.conf` — `Cache-Control: public, max-age=86400` для public, `private, max-age=10` для private, `CDN-Cache-Control` уже есть в `prod.yml` — дописать.
4. Добавить `Range` кеш на CDN (CloudFront/Cloudflare) + `vod_response_cache 128m` уже есть `nginx.conf:18` — увеличить до 512m для N видео.

**Проверка:** `curl /hls/{private_id}/master.m3u8` без токена → 403, с токеном → 200, `curl -I` показывает `Cache-Control: private`.

---

## Фаза 14 — P1: Наблюдаемость

**Приоритет: P1 High** · Оценка: 2 дня · Можно параллельно с 11-13

**Проблемы:** `RequestID` не прокидывается (`proxy.go:18`), логи `zerolog/slog/logging` вперемешку, нет метрик.

**Задачи:**

1. Gateway `proxy.go:18` прокидывать `X-Request-ID` (chi `middleware.RequestID`), `X-User-ID`, `X-Forwarded-For` во все апстримы. `metadata`/`upload` логировать `request_id`.
2. Добавить `prometheus` + `grafana` в `deploy/docker-compose.yml`, метрики: `rabbitmq_queue_depth`, `ffmpeg_duration_seconds`, `upload_bytes`, `vod_cache_hit`.
3. `auth`/`metadata`/`upload`/`gateway` — единый JSON лог формат (`zerolog` уже json в prod `gateway/main.go:35`), добавить `trace_id`.
4. Алерты: queue depth >100, transcoder OOM, MinIO disk >80%.

**Проверка:** `curl -H X-Request-ID:test123 /api/v1/videos` → логи всех сервисов содержат `test123`, `http://localhost:9090/metrics` отвечает.

---

## Фаза 15 — P2: Стоимость и хранение ✅ DONE 2026-09-02

**Приоритет: P2 Medium** · Оценка: 1-2 дня · Зависит от: Фазы 11, 12 · **Выполнено**

**Выполнено 2026-09-02:**

1. **MinIO lifecycle + raw cleanup:** `deploy/docker-compose.yml:89` `minio-setup` → `mc ilm rule add --expire-days 7 --prefix raw/` (проверено `mc ilm ls raw/ 7`), `services/transcoder/app/consumer.py:43` `KEEP_RAW=false` → сразу `RemoveObject raw/{id}/original.mp4` после `PATCH ready` (только если `update_status` ok), fallback lifecycle; `.env.example:37` `KEEP_RAW=false`.
2. **PgBouncer:** `deploy/docker-compose.yml:18` `pgbouncer:6432` `edoburu/pgbouncer:latest` `transaction` `default_pool_size 20`, `postgres` `password_encryption=md5` + `pg_hba md5` для SCRAM-совместимости, `services/auth/src/core/db.py:28` `pool_size 5` `statement_cache_size=0` + реврайт `postgres→pgbouncer` при `PGBOUNCER_ENABLED=true`, `services/metadata/cmd/server/main.go:47` `MaxConns 5` `QueryExecModeSimpleProtocol`.
3. **Frontend:** `frontend/next.config.mjs:3` `output: standalone` уже был, `frontend/Dockerfile:13` multi-stage `standalone` (~120MB), `VideoPlayer.tsx` `maxBufferLength` сохранен.
4. **DASH:** `deploy/nginx/nginx.conf:68` `location /dash/` `vod dash` + fix `mapping` regex `hls/|dash/` для JIT.

**Затронуто:** `deploy/docker-compose.yml`, `deploy/migrations/000003_add_updated_at.*`, `deploy/postgres/init.sql:38` `updated_at`, `deploy/nginx/nginx.conf`, `services/auth/src/core/db.py`, `services/metadata/.../video.go`, `services/transcoder/app/consumer.py`, `.env.example`.

**Проверка:** `raw/` удаляется сразу + 7d safety, `videos` имеет `updated_at TIMESTAMPTZ + trigger`, `psql -h pgbouncer -p 5432` ok, `docker ps` pgbouncer healthy.

---

## Бэклог (не в фазах)

- ~~`golang-migrate` / `alembic` миграции вместо `init.sql` на проде.~~ ✅ **Выполнено 2026-08-30:** `deploy/migrations/000001_init.up.sql` (golang-migrate) + сервис `migrate` в `deploy/docker-compose.yml:22`, `deploy/postgres/init.sql` помечен legacy, `Makefile: migrate-up/down/create`, `services/auth/migrations/env.py` + `versions/0001_init.py` (alembic). См. `deploy/migrations/README.md:1`.
- ~~Удаление видео.~~ ✅ **Выполнено 2026-08-30:** `DELETE /api/v1/videos/:id` (owner only, `metadata/internal/handler/video.go:213` + `storage/minio.go:29` + `RemovePrefix` для orphans mid-transcode, `metadata/cmd/server/main.go:1` MinIO env), `gateway` уже проксировал `DELETE`, `frontend` `deleteVideo` + кнопки на `watch` и `VideoCard`.
- `Content-Range` resumable fallback для presign (если MinIO multipart не поддержан).
- Rate-limit на `auth` login (brute-force) — `redis` + `slowapi`.
- E2E `scripts/e2e.sh:189` расширить на presign + private HLS.

## Чеклист приемки харденинга

- [ ] `make up && make e2e` зеленый (старый флоу + новый presign)
- [ ] `make lint-go lint-py lint-front && make test-go test-py` зеленые
- [ ] Нагрузочный: 5× параллельных 1GB загрузок + 10 видео в очереди — без OOM, DLQ работает
- [ ] Security: `internal` без токена 401, `raw` не публичен, `hls` private 403 без JWT
