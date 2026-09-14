# Flowix — Senior Fullstack Review (2026-09-13)

> Автор: senior fullstack review. Исходники: `docs/spec.md:1`, `docs/services-pipeline.md:1`, `docs/PLAN.md:1`.
> Предыдущее ревью: `docs/REVIEW.md:1` (не перезаписано, это дополнение с фокусом на оптимизацию, архитектуру и визуал).
> Стек на момент ревью: Go 1.27 chi (gateway/metadata/upload), FastAPI+pika (auth/transcoder), nginx-vod JIT, Next.js 14 + hls.js + zustand + tailwind.

## 0. Вердикт

Ядро правильное: `upload → RabbitMQ → transcode → MinIO → HLS`, выровненный GOP, общий `audio.m4a`, `INTERNAL_TOKEN`, DLX+retry, presigned upload, HLS-auth.
Главные риски сейчас:

1. Приватность течёт мимо HLS (metadata List/Get + `/thumbnails/*` без auth).
2. HLS-auth делает HTTP в metadata на каждый `.ts`-сегмент — сам себе DDoS.
3. `transcoder/app/consumer.py:1` — god-file 998 строк, pipe-флаг не экономит диск, fan-out объявлен но не потребляется.
4. Фронт без UI-кита, без react-query, с ручным плеером — самый дешёвый прирост UX именно тут.

---

## 1. Логика — что чинить

### P0 — безопасность / корректность

| # | Проблема | Где | Фикс |
|---|----------|-----|------|
| 1 | `List` отдаёт приватные видео всем, `Get` без проверки visibility | `services/metadata/internal/repository/video.go:64`, `handler/video.go:110`, `gateway/cmd/server/main.go:129` | `WHERE (visibility='public' OR owner_id=$me)`, `Get` проверяет owner |
| 2 | Превью приватных видео публичны | `gateway/cmd/server/main.go:145` `/thumbnails/*` | Тот же `HLSAuth` / signed URL с TTL |
| 3 | HLS-auth без кеша: 1 просмотр = 100+ запросов в metadata | `gateway/internal/middleware/hlsauth.go:98` `fetchVideoMeta`, `http.Client{Timeout:2s}` без пула | LRU `video_id→meta` 10с, общий `http.Client` с пулом, negative cache |
| 4 | Gateway без таймаутов, JWT с дефолтом | `gateway/cmd/server/main.go:170` `ListenAndServe`, `:24` `change-me...` | `http.Server{Read:15s,Write:60s,Idle:120s}` + fail fast, graceful shutdown |
| 5 | Pipe-режим качает файл всё равно + ещё раз на каждый рип | `transcoder/app/consumer.py:719-727,770-783` | Либо честный `get_object stream → stdin` без `fget_object`, либо удалить флаг |
| 6 | `transcode_one` vs `transcode_one_pipe` дублируют сборку ffmpeg (~60%) | `consumer.py:294,435` | Один `build_ffmpeg_argv(input_mode, ...)` |
| 7 | `FANOUT_QUEUES` объявлены, но `basic_consume` только `QUEUE` | `consumer.py:65,973` | Либо допилить fan-out воркеры, либо удалить |
| 8 | BlockingConnection + ffmpeg 900с в одном потоке — heartbeat 600 всё равно отвалится на 5GB | `consumer.py:877-980` | Heartbeat в отдельном потоке / отдельный коннект, `ffmpeg -progress` |
| 9 | `complete` не сверяет size/owner/content-type | `upload/internal/handler/presign.go` | Сверить `StatObject.size` + owner из metadata |
| 10 | Presign resume только reuse URL, не offset; resumable шлёт одним чанком | `frontend/src/lib/api.ts:224,260` | S3 multipart (create/sign parts/complete), чанки 5-10MB |
| 11 | Нет silent refresh: access 15м → молчаливый logout | `frontend/src/store/auth.ts:39`, `lib/api.ts:76` | 401-interceptor + refresh retry + очередь запросов |
| 12 | Токены в `localStorage` | `auth.ts:29,48` | httpOnly cookies via Next Route Handler (BFF), позже |

### P1 — производительность бэка

- `metadata Update`: 3× `UPDATE` + `GetByID` = 4 запроса (`repository/video.go:85`). Один `UPDATE ...` в транзакции.
- `Create` делает лишний `SELECT email` synchronously (`video.go:29`). JOIN или фон.
- `List ORDER BY created_at DESC` без курсора/total/индекса. Добавить `keyset` + `pg_trgm` поиск.
- Gateway пересоздаёт `http.Client` на HLS-пути. Один переиспользуемый клиент.
- `upload Upload` + gateway оба ставят `MaxBytesReader` с разных env — выровнять, один источник.
- Thumbnails через gateway — лишний hop. Отдавать signed CDN URL напрямую из MinIO.

## 2. Архитектура — варианты

1. **nginx-vod — риск.** Модуль Kaltura фактически заброшен. Варианты:
   - A (рекомендую для MVP): прегенерить HLS `.ts` в транскодере, хранить в MinIO, раздавать статику + CDN. Ноль JIT-CPU, идеальный кеш.
   - B: оставить JIT, но запинить форк + fallback на static при 5xx.
2. **Пуш вместо поллинга.** Сейчас `watch/[id]` поллит каждые 3с (`watch/[id]/page.tsx:48`). `video.transcoded` как событие → `gateway SSE /api/v1/videos/{id}/events` → фронт.
3. **Единый контракт.** Go struct + Pydantic + TS `Video` руками дрейфуют. `openapi.json` как source of truth + `oapi-codegen` + `openapi-typescript`.
4. **Compose-профили.** 17 сервисов в одном `docker-compose.yml` тяжело для dev. `core / monitoring / gpu` профили, `standalone + distroless` для FE.
5. **Наблюдаемость.** Метрики есть, трейсов нет. OTel `gateway→services` + `trace_id` в логах, Sentry FE/BE.

## 3. Фронтенд и визуал — главный рычаг

Сейчас: голый Tailwind, `confirm()/alert()`, смесь RU/EN, `<img>` вместо `next/image`, ручные `useEffect`-фетчи, кастомный плеер 194 строки (`VideoPlayer.tsx:110` с `BUFFER_FLUSHING`-хаком и `as any`).

### Рекомендуемый стек (обоснование)

| Задача | Взять | Почему, что заменяет |
|--------|-------|----------------------|
| Дизайн-система | **shadcn/ui + Radix** | Вендорится в репо, Tailwind уже есть, a11y из коробки. `Button/Card/Badge/Dialog/Progress/Skeleton/DropdownMenu/Sonner` вместо `confirm/alert`, хардкода `bg-red-500/bg-black` |
| Плеер | **Vidstack** (hls.js внутри) | Готовые качество/скорость/PiP/субтитры/мобильный fullscreen. Убирает ~120 строк ручного `nextLevel`-кода |
| Данные | **TanStack Query** | Кеш ленты, `refetchInterval` вместо ручного `setInterval`, инфинит-скролл |
| Формы | **react-hook-form + zod** | login/register/upload валидация |
| Загрузка | **Uppy / react-dropzone + tus** | Drag&drop, чанки, resume, отмена (AbortController уже протянут в `api.ts`, но кнопки нет) |
| Тосты/диалоги | **Sonner + Radix Dialog** | Вместо `alert/confirm` в `VideoCard.tsx:29`, `watch/[id]/page.tsx:97` |
| Картинки | **next/image** | Вместо `<img>` с eslint-disable в `VideoCard.tsx:44` |

Визуально: тёмная YouTube-like тема, `Header` с поиском, `VideoCard` с длительностью + hover-preview, `watch` в 2 колонки (player + next up), `upload` — степпер `файл → мета → заливка → статус транскода`, `loading.tsx/error.tsx/not-found.tsx`, `generateMetadata` OG-теги, `middleware.ts` защита `/upload`, `next-intl` (сейчас смесь языков).

Перф: `dynamic import hls.js` (~130KB gz из бандла), zustand-селекторы (`useAuth(s=>s.userId)` — сейчас каждый `VideoCard` ререндерится), убрать дубли `base()/hlsBase()`, неиспользуемый `createVideoMeta`.

---

## 4. Предлагаемый план

### Milestone 0 — Quick wins (1-2 дня)

1. Metadata `List/Get` — фильтр visibility + owner. Thumbnails за auth.
2. `hlsauth` LRU-кеш 10с + общий `http.Client`.
3. Gateway `http.Server` таймауты + fail fast на пустом `JWT_SECRET` + graceful shutdown.
4. Фронт: Sonner+Dialog вместо confirm/alert, фикс двойного сабмита (`upload/page.tsx:82`), фикс утечки поллинга, 401-refresh интерцептор.
- Verify: приватное без токена 403 везде; `ab` на `/hls/` без деградации metadata; истечение access → silent refresh.

### Milestone 1 — Пайплайн (1 спринт)

1. Разбить `consumer.py` → `queue.py/ffmpeg.py/pipeline.py`, один билдер argv.
2. Честный pipe без `fget_object` + `ffmpeg -progress` → `%` в metadata.
3. Heartbeat-поток, `confirm_delivery()`, валидация `video_id/s3_key`.
4. S3 multipart presign (чанки 5-10MB, resume по offset, сверка size+owner в `complete`).
5. SSE `video.transcoded` вместо поллинга.
- Verify: e2e resumable 100MB+ с обрывом; «metadata упал → retry, не ack»; прогресс % в UI.

### Milestone 2 — Визуал (1 спринт)

1. `shadcn init` (Button/Input/Card/Badge/Dialog/Progress/Skeleton/DropdownMenu/Sonner), тёмная тема, brand-цвет, один язык.
2. Vidstack вместо ручных контролов; Uppy на upload; TanStack Query + RSC (`page.tsx`, `watch`), `loading/error/not-found`, OG.
3. `next/image`, dynamic hls.js, zustand-селекторы.
4. Поиск + пагинация + «мои видео» + отмена загрузки + drag&drop.
- Verify: Lighthouse perf/a11y, ручные сценарии mobile Safari.

### Milestone 3 — Архитектура (бэклог)

1. Static HLS опция (`VOD_MODE=static`), CDN Cache-Control, план ухода от nginx-vod.
2. OpenAPI codegen + контракт-тесты, `docs/spec.md:73` как зеркало.
3. OTel + Sentry + pprof; compose profiles; FE `standalone` <200MB; bump Next 14.2.5 → 14.2.x+/15.
4. Тесты: pgx testcontainers, HLSAuth, `on_message` retry/DLQ, e2e resumable, auth на реальном Postgres.

---

## 5. Связь с существующими фазами

- Дополняет `docs/PLAN.md:183` Фаза 12 (ladder/HW/fan-out), Фаза 13 (HLS auth+CDN), Фаза 14 (observability), Фаза 15 (cost) — не заменяет.
- Не противоречит `docs/REVIEW.md:1` — там построчный аудит багов, тут приоритизация под оптимизацию/архитектуру/визуал.
