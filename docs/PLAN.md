# Flowix — План реализации

> Дата: 2026-08-30 (переписан после senior review 2026-08-29)
> Стек: RabbitMQ + chi `go-chi/chi/v5` + nginx-vod JIT + Next.js 14
> Исходники: `docs/spec.md:1` (ТЗ), `docs/services-pipeline.md:1` (пайплайн), `AGENTS.md:1` (гайдлайны)
> Ревью-отчет: см. чат 2026-08-29 (P0/P1/P2 находки)

## Легенда приоритетов

| Приоритет | Смысл | SLA |
|---|---|---|
| **P0 Critical** | Прод-блокер: OOM, потеря данных, дыра безопасности | до след. релиза |
| **P1 High** | Масштаб/надежность: большие файлы, N видео, ресурсы | 1-2 спринта |
| **P2 Medium** | Оптимизация/UX/DX: экономия, наблюдаемость, техдолг | бэклог |

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
Фаза 10 (P0: Transcoder limits) ──┼→ Фаза 11 (P1: presigned upload) → Фаза 13 (P1: HLS auth+CDN)
Фаза 10b (P0: Queue DLX) ─────────┘         ↓
                                   Фаза 12 (P1: adaptive ladder/HW)
                                            ↓
                                   Фаза 14 (P1: observability)
                                            ↓
                                   Фаза 15 (P2: storage/CDN cost)
```

---

## Завершенные фазы (0-8) — кратко, без изменений

### Фаза 0 — Фундамент монорепо ✅
Скелет `services/gateway|metadata|upload|auth|transcoder`, `frontend/`, `deploy/docker-compose.yml` (postgres:16 :5432, minio :9000/:9001, rabbitmq :15672, nginx-vod :8081), `.env.example`, `Makefile`. Проверка: `make up && make ps`.

### Фаза 1 — Контракты и БД ✅
`deploy/postgres/init.sql:1` — `video_status ENUM`, `users`, `videos (thumbnail_s3_key)`, `video_renditions`, `S3 events video.uploaded/video.transcoded` (`docs/PLAN.md:73` old).

### Фаза 2a — Auth (Py FastAPI) ✅
`services/auth/src/routers/auth.py:1` — register/login/refresh/me, JWT HS256 15m/7d, argon2. `src/core/security.py:34`.

### Фаза 2b — Metadata (Go chi) ✅
`services/metadata/cmd/server/main.go:1` — CRUD `/api/v1/videos`, `PATCH /internal/videos/:id/status`, `GET /internal/videos/:id/vod` (`internal/handler/video.go:124`).

### Фаза 3 — Upload (Go chi) ✅
`services/upload/internal/handler/upload.go:57` — multipart `POST /api/v1/videos/upload` → MinIO `raw/{id}/original.mp4` → RabbitMQ.

### Фаза 4 — Transcoder (pika + FFmpeg) ✅
`services/transcoder/app/consumer.py:1` — download → ffprobe → 3× ffmpeg `-g 60 -force_key_frames expr:gte(t,n_forced*2)` (`consumer.py:119`) → `renditions/{id}/{360,720,1080}.mp4` → PATCH ready.

### Фаза 5 — Streaming nginx-vod JIT ✅
`deploy/nginx/nginx.conf:1` — `vod_mode mapped`, `proxy_pass minio:9000`, `vod_segment_duration 2000`.

### Фаза 6 — Gateway (Go chi) ✅
`services/gateway/cmd/server/main.go:1` — `ReverseProxy` (`internal/proxy/proxy.go:15`), CORS, `RateLimit 20/40` (`internal/middleware/ratelimit.go:19`), JWT.

### Фаза 7 — Frontend (Next 14) ✅
`frontend/src/components/VideoPlayer.tsx:1` — `hls.js` + `quality/speed` (`frontend/src/lib/api.ts:15`).

### Фаза 8 — Интеграция и харденинг ✅
`scripts/e2e.sh:1` — `upload→poll ready→master.m3u8→ffprobe aligned segments→gateway /hls`, `make lint-*` зеленые, `deploy/docker-compose.prod.yml:1` (CDN Cache-Control).

---

## Фаза 9 — P0: Безопасность и Upload streaming

**Приоритет: P0 Critical** · Оценка: 2-3 дня · Зависит от: 0-8 ✅

**Проблемы из ревью:**
- `services/upload/internal/handler/upload.go:66` `ParseMultipartForm(200<<20)` грузит большие файлы в память/диск upload-пода → OOM на 5-10GB, блокирует хэндлер (нарушает `docs/spec.md:252` — heavy work в очереди).
- `deploy/docker-compose.yml:43` `mc anonymous set public` — `raw/` и `renditions` публично читаемы.
- `services/metadata/cmd/server/main.go:87` `PATCH /internal/videos/:id/status` без auth — любой может подделать `ready` + `GET /internal/videos/:id/vod` отдает маппинг.
- Нет лимита размера запроса ни в gateway, ни в upload.

**Задачи:**
1. `upload`: `r.Body = http.MaxBytesReader(w, r.Body, 5<<30)` + стриминг в MinIO без буферизации всего файла (использовать `header.Size` только как hint, `minio-go` multipart). Пробросить `X-Request-ID`.
2. `gateway`: `MaxBytesReader` 5GB для `/api/v1/videos/upload`, `ReadTimeout/WriteTimeout` 10m для upload-роута, не резать остальное.
3. `metadata`: middleware `InternalAuth(sharedSecret=X-Internal-Token)` для всех `/internal/*` (`services/metadata/internal/middleware/auth.go`, `services/gateway` прокидывает header, `deploy/nginx/nginx.conf:42` тоже). Альтернатива: `allow 10.0.0.0/8; deny all;` в nginx.
4. `deploy/docker-compose.yml:34` убрать `anonymous set public`, заменить на `mc anonymous set private` или `download` только через gateway/MinIO presigned. MinIO bucket policy — явный `Deny`.
5. Добавить валидацию `Content-Type` через `ffprobe` а не `header.Header.Get("Content-Type")` (`upload.go:94`).

**Затронет:** `services/upload/internal/handler/upload.go`, `services/upload/internal/storage/minio.go`, `services/gateway/cmd/server/main.go`, `services/metadata/cmd/server/main.go`, `deploy/docker-compose.yml`, `deploy/nginx/nginx.conf`, `.env.example` (`INTERNAL_TOKEN`).

**Проверка:** `make lint-go && make test-go`; `curl` без `X-Internal-Token` на `/internal/videos/:id/status` → 401; `upload` 6GB файл → 413; `mc anonymous get` → `access denied`; `make e2e` зеленый.

---

## Фаза 10 — P0: Transcoder — лимиты ресурсов и надежность

**Приоритет: P0 Critical** · Оценка: 3-4 дня · Зависит от: Фаза 9

**Проблемы:**
- `services/transcoder/app/consumer.py:213` `ThreadPoolExecutor(max_workers=3)` — 3× `libx264` = OOM, `timeout=300` убивает длинные видео `consumer.py:139`, `fget_object` качает целиком в `/tmp` `consumer.py:188` — диск кончается на больших файлах.
- Нет лимитов в `deploy/docker-compose.prod.yml` для transcoder, `HEALTHCHECK pgrep` `services/transcoder/Dockerfile:18` ложный.
- `consumer.py:139` форсирует `-r 30` даже для 24fps → лишний CPU.

**Задачи:**
1. Последовательный транскод (или `max_workers=1`) + горизонтальный масштаб: `docker-compose.yml` `transcoder: deploy.replicas: 2-3` или KEDA по длине очереди. В `consumer.py` убрать `ThreadPoolExecutor`, цикл по `RENDITIONS_SPEC`.
2. `transcode_one`: добавить `-threads 2 -preset veryfast -movflags +faststart`, сохранять оригинальный fps если ≤30 (`ffprobe` `consumer.py:58` уже есть — использовать), не форсировать `-r` вслепую. `-maxrate` поправить на `1.1×` вместо `1.07×` для стабильности.
3. Потоковая обработка: два варианта — (a) `Minio.fget_object` → tmp с проверкой `shutil.disk_usage` и очисткой; (b) пайп `ffmpeg -i pipe:0` + range-GET (след. фаза). Для этой фазы — (a) + лимит диска.
4. `consumer.py:139` `timeout=900` (15m) + обработка `SIGTERM` (graceful ack/nack). `celery_app.py`/`tasks.py` — удалить или мигрировать на Celery (выбрать одно, сейчас два механизма). Если оставляем pika — удалить `celery` из `pyproject.toml:7` и `redis`.
5. `deploy/docker-compose.yml:156` и `prod.yml`: `resources.limits: cpus:2 memory:4G`, `restart: unless-stopped`, healthcheck `curl http://metadata:8002/health && pgrep`.
6. `.env.example`: `FFMPEG_THREADS`, `FFMPEG_PRESET`.

**Проверка:** загрузка 4GB 2h видео → один воркер не OOM, `docker stats` <4G, `make e2e` с `SAMPLE` 1080p 60s проходит, `kill -TERM transcoder` — graceful.

---

## Фаза 10b — P0: Очередь — DLX, ретраи, идемпотентность

**Приоритет: P0 Critical** · Оценка: 1-2 дня · Зависит от: Фаза 10 (можно параллельно с 9)

**Проблемы:**
- `services/transcoder/app/consumer.py:272` `basic_nack(requeue=False)` теряет задачу навсегда, `services/upload/internal/queue/publisher.go:94` публикует `Persistent` но без `DLX`, `consumer.py:264` бесконечный `while True` без backoff на `AMQPConnectionError`.
- Нет идемпотентности: повтор `video.uploaded` → дубли `video_renditions`.

**Задачи:**
1. Объявить `video.uploaded` с `x-dead-letter-exchange: dlx`, `video.uploaded.dlq` + `video.uploaded.retry` (TTL 30s). В `publisher.go:42` и `consumer.py:263` `queue_declare` с `arguments`.
2. Консюмер: `try/except` → `basic_nack(requeue=False)` только в DLQ, ретрай 3× с `x-retry-count` header, после — `status=failed` + логирование.
3. `metadata` `repository/video.go:123` `UpdateStatus` уже `ON CONFLICT DO UPDATE` — добавить идемпотентность по `video_id+status` (не перетирать `ready` на `processing`).
4. `upload` publisher: добавить `ensure()` ретрай уже есть `queue/publisher.go:78` — покрыть тестом `publisher_test.go`.

**Проверка:** убить transcoder mid-job → сообщение уходит в retry → через 30s повтор, после 3× — в DLQ + `status=failed`; `rabbitmqadmin list queues` показывает DLQ.

---

## Фаза 11 — P1: Масштабируемый upload (presigned multipart)

**Приоритет: P1 High** · Оценка: 4-5 дней · Зависит от: Фазы 9, 10b

**Проблема:** upload через Go прокси — узкое место для больших файлов и N параллельных загрузок (1 под = 1 файл в памяти). Нет resumable.

**Задачи:**
1. Новый флоу: `POST /api/v1/videos/presign` (gateway→upload→MinIO) возвращает `{video_id, upload_id, presigned_urls[]}` (S3 CreateMultipartUpload). FE (`frontend/src/lib/api.ts:123`) режет `File` на 10-50MB чанки, `PUT` напрямую в MinIO, `POST /complete` → upload публикует `video.uploaded`.
   - Альтернатива: `tus.io` (tus-gateway) — проще resumable, но S3 multipart нативнее для MinIO.
2. `services/upload` добавить `internal/storage/presign.go` (minio-go `PresignedPutObject` / `CreateMultipartUpload`), `handler/presign.go`.
3. Сохранить совместимость: оставить `POST /api/v1/videos/upload` для файлов <100MB, пометить deprecated.
4. Gateway: прокинуть presign без `MaxBytesReader`, добавить `CORS` для `PUT` на MinIO (или прокси через gateway с `X-Amz-*`).
5. FE: заменить `XMLHttpRequest` `lib/api.ts:130` на `fetch` + чанки + retry + `onProgress` по чанкам, `AbortController`, `resume from localStorage`.

**Затронет:** `services/upload/*`, `services/gateway/cmd/server/main.go`, `frontend/src/lib/api.ts`, `frontend/src/app/upload/page.tsx`, `deploy/docker-compose.yml` (MinIO `MINIO_API_CORS_ALLOW_ORIGIN`).

**Проверка:** загрузка 2GB через браузер → сеть показывает прямые `PUT` в `:9000`, обрыв сети → resume, `make e2e` с `SAMPLE` через presign зеленый.

---

## Фаза 12 — P1: Адаптивный ladder + HW accel

**Приоритет: P1 High** · Оценка: 3 дня · Зависит от: Фаза 10, 11

**Проблемы:**
- Фиксированный ladder `consumer.py:27` апскейлит 480p→1080p, тратит CPU на мыло.
- `libx264` CPU-only — дорого при N видео.

**Задачи:**
1. Использовать `probe_video` `consumer.py:58`: выбирать ladder динамически:
   - `<720p` → `[360p]` или `[360p,480p]`
   - `720p` → `[360p,720p]`
   - `≥1080p` → `[360p,720p,1080p]` (+ опц. `144p` для превью на мобильных)
2. Перейти на `CRF 23 + maxrate` вместо чистого CBR — экономия 20-30% битрейта. Оставить CBR опцией `env: ENCODE_MODE=crf|cbr`.
3. HW accel: `Dockerfile` `services/transcoder/Dockerfile:4` собрать `ffmpeg` с `--enable-nvenc` / `qsv` (или базовый образ `jrottenberg/ffmpeg:*-nvidia`), `transcode_one` выбирает `h264_nvenc` если доступен (`nvidia-smi`), иначе `libx264`.
4. Добавить `-vf scale=-2:{h}:flags=lanczos` уже есть `consumer.py:100` — оставить, добавить `format=yuv420p` для совместимости.
5. Генерировать thumbnail из 360p рипа, не из оригинала (быстрее).

**Проверка:** 480p исходник → только 360p рип (нет 1080p), 4K → 1080p макс, `ffprobe renditions` все `h264`, `encode time` на NVENC 3× быстрее, `scripts/e2e.sh` `EXT-X-STREAM-INF count` динамический (обновить `EXPECTED_RENDITIONS`).

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

## Фаза 15 — P2: Стоимость и хранение

**Приоритет: P2 Medium** · Оценка: 1-2 дня · Зависит от: Фазы 11, 12

**Задачи:**
1. MinIO lifecycle: удалять `raw/{id}/original.mp4` через 7d после `ready` (или сразу, оставлять только если `keep_raw=true`). Добавить `mc ilm add --expiry-days 7`.
2. Добавить `pgbouncer` перед postgres, `pool_pre_ping=True` уже есть `services/auth/src/core/db.py:26` — настроить `pool_size`.
3. `frontend` `next.config.mjs` — `output: standalone` для меньшего образа, `VideoPlayer.tsx:34` `maxBufferLength:10` уже экономит память клиента — оставить.
4. DASH опционально (nginx-vod уже умеет `vod_dash`), но после HLS харденинга.

**Проверка:** `raw/` удаляется, `videos` таблица имеет `updated_at`, `docker images` FE <200MB.

---

## Бэклог (не в фазах)

- `golang-migrate` / `alembic` миграции вместо `init.sql` на проде.
- `Content-Range` resumable fallback для presign (если MinIO multipart не поддержан).
- Rate-limit на `auth` login (brute-force) — `redis` + `slowapi`.
- E2E `scripts/e2e.sh:189` расширить на presign + private HLS.

## Чеклист приемки харденинга

- [ ] `make up && make e2e` зеленый (старый флоу + новый presign)
- [ ] `make lint-go lint-py lint-front && make test-go test-py` зеленые
- [ ] Нагрузочный: 5× параллельных 1GB загрузок + 10 видео в очереди — без OOM, DLQ работает
- [ ] Security: `internal` без токена 401, `raw` не публичен, `hls` private 403 без JWT
