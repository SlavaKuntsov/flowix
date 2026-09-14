# Code Review — Flowix (2026-09-13)

> Полное ревью проекта как senior fullstack: Go-сервисы (gateway/metadata/upload), Python-сервисы (auth/transcoder), фронтенд, инфраструктура. В конце — приоритизированный план правок.
> Дополнение с фокусом на оптимизацию/архитектуру/визуал: `docs/REVIEW-fullstack-2026-09-13.md:1`.

## Общая оценка

Проект для пет-проекта/портфолио — крепкий: слои выделены, тесты на хендлерах есть, мониторинг (Prometheus/Grafana/Loki) подключён, миграции и topology очередей продуманы. Но в коде накопилось несколько реальных багов уровня продакшена (IDOR, зависшие видео, O(n²) загрузка), много копипасты между Go-сервисами, и фронтенд не использует ничего из того, что даёт Next.js App Router.

---

## 1. Критичное — чинить в первую очередь

### Безопасность

| # | Проблема | Где |
|---|----------|-----|
| 1 | **IDOR в upload**: `Complete`, `Status` и resumable `Upload` не проверяют владельца видео — любой залогиненный юзер, узнав UUID, может перезаписать чужой `raw/{id}/original.mp4` или запустить транскод чужого видео | `services/upload/internal/handler/presign.go:126`, `resumable.go:47,98` |
| 2 | **JWT secret с дефолтом** `change-me-super-secret...` захардкожен во всех сервисах; compose передаёт `${JWT_SECRET}` без проверки — забытая переменная = тихо небезопасный деплой. Нужен fail fast при старте | `gateway/cmd/server/main.go:24`, `metadata/cmd/server/main.go:57`, `upload/cmd/server/main.go:38`, `auth/src/core/config.py:7`, `deploy/docker-compose.yml:158` |
| 3 | **CORS `*` + `Allow-Credentials: true`** — фактически «любой origin с куками» для всех роутов, включая проксированный auth | `gateway/internal/middleware/cors.go:52-58` |
| 4 | **Rate-limit обходится**: in-memory по IP, доверяет клиентскому `X-Forwarded-For`; в auth slowapi ключует по IP гейтвея — один абузер блокирует всех. Redis задеплоен, но не используется | `gateway/internal/middleware/ratelimit.go:19-99`, `auth/src/core/limiter.py:15-33` |
| 5 | **Порты metadata/upload/auth торчат наружу** в compose — клиенты идут мимо гейтвея, минуя rate-limit и CORS | `deploy/docker-compose.yml` (auth 8001, metadata 8002, upload 8003) |
| 6 | **Пресigned PUT без лимита размера** — не подписан `Content-Length`, URL позволяет залить произвольный объём за 1 час | `upload/internal/storage/minio.go:65-105` |
| 7 | **Т thumbnails приватных видео публичны**: `/thumbnails/*` проксируется в MinIO без auth | `gateway/cmd/server/main.go:145-146` |
| 8 | **JWT без обязательного `exp`**: токен без expiration принимается навсегда (`jwt.WithExpirationRequired()` отсутствует) | все три auth-мидлвари Go |
| 9 | **InternalAuth** — не constant-time сравнение секрета | `metadata/internal/middleware/internal.go:15` |

### Корректность

| # | Проблема | Где |
|---|----------|-----|
| 10 | **Resumable upload O(n²)**: каждый чанк скачивает весь существующий объект в память, аппендит и перезаливает. На 5GB-файле — гигабайты перетрафика. Нужен S3 multipart upload | `upload/internal/handler/resumable.go:170-183` |
| 11 | **Видео навсегда «processing»**: если `update_status("ready")` упал, transcoder всё равно ack-ает сообщение — ретрая не будет никогда | `transcoder/app/consumer.py:832-926` |
| 12 | **Токены не обновляются никогда**: `refresh_token` кладётся в localStorage и ни разу не читается; при истечении access — молчаливый logout. Нет 401-обработки и refresh-ретея | `frontend/src/store/auth.ts:39`, `frontend/src/lib/api.ts:76-93` |
| 13 | **Двойной сабмит загрузки**: кнопка активна при `progress === 0` — второй клик запускает параллельную загрузку | `frontend/src/app/upload/page.tsx:82` |
| 14 | **Зомби-процессы ffmpeg**: в pipe-режиме при ошибке стрима или таймауте `communicate` ffmpeg не убивается и не реапится | `transcoder/app/consumer.py:518-559` |
| 15 | **Create-в-метадате валидирует после вставки**: видео вставляется, потом 400 на невалидной visibility — строка уже существует | `metadata/internal/handler/video.go:84-98` |
| 16 | **SigV4-баг в fallback**: host переписывается после подписи — возвращается нерабочая presigned URL | `upload/internal/storage/minio.go:87-96` |
| 17 | **Ошибки через сравнение строк**: `if err.Error() == "forbidden"`; любая ошибка БД маппится в 404. Нужны sentinel-ошибки + `errors.Is` | `metadata/internal/handler/video.go:220-228` |
| 18 | **Upload интерполирует `err.Error()` в рукописный JSON** — битый JSON при спецсимволах и утечка деталей инфры клиенту | `upload/internal/handler/upload.go:86,114`, `presign.go:87,101`, `resumable.go:120` |
| 19 | **Утечка поллинга на watch-странице**: при ошибке `setInterval` не останавливается никогда | `frontend/src/app/watch/[id]/page.tsx:48` |
| 20 | **Гонка при регистрации**: SELECT-then-INSERT без ловли `IntegrityError` — конкурентная регистрация одного email даёт 500 | `auth/src/routers/auth.py:46-51` |

---

## 2. Архитектура

### Go-сервисы (gateway / metadata / upload)

- **Тройная копипаста**: `AuthMiddleware`, `RequestLogger`+`respWriter`, `metrics.go`, `writeJSON`, bootstrap env/log скопированы в три сервиса почти байт-в-байт. Нужен shared-модуль через `go.work`.
- **Версии расходятся**: gateway/metadata `go 1.25.0`, upload `go 1.23.0`; jwt v5.3.1 vs v5.2.2; minio v7.3.0 vs v7.0.70. `go.work` в корне нет.
- **Нет graceful shutdown и таймаутов** `http.Server` ни в одном сервисе (slowloris + обрыв загрузок при деплое).
- **`http.Client` без таймаута** в upload→metadata — зависший запрос блокирует воркер навсегда (`upload/internal/client/metadata.go:16`); наоборот, в gateway на каждый HLS-сегмент создаётся новый `http.Client` (`hlsauth.go:107`) — самый горячий путь без коннект-пула.
- **`respWriter` не реализует `http.Flusher`** — стриминг прокси/HLS без инкрементального flush.
- **`QueueDelete(ifEmpty=false)`** при «лечении» topology — удаляется живая очередь с сообщениями (`upload/internal/queue/publisher.go:58`).
- **Мёртвый код**: `OptionalAuth`, `NewWithPrefix`, `RemoveObject`, дублированный `writeJSON` в metadata, метрики `ffmpeg_duration`/`rabbitmq_queue_depth`, которые никто не инкрементит, `var _ = regexp.MustCompile` в resumable.
- **Update в репозитории**: read-check-write (TOCTOU) + до 3 UPDATE без транзакции (`metadata/internal/repository/video.go:85-116`). `UpdateStatus` при этом написан правильно (tx + `FOR UPDATE`) — образец.
- **`Create` делает лишний запрос в `users` на каждую вставку**; ошибочность owner-email глотается (`video.go:29`).

### Python-сервисы (auth / transcoder)

- **Мёртвый Celery**: `celery_app.py`/`tasks.py` — стабы («will be removed» по README), а пакеты `celery`, `redis`, `boto3`, `httpx`, `python-multipart` в зависимостях не используются.
- **Split-brain миграций**: alembic в auth — намеренный no-op, схема живёт в golang-migrate (`deploy/migrations`). Два инструмента — магнит для дрейфа. Выбрать один.
- **`transcode_one` vs `transcode_one_pipe`**: ~60% дублирования, уже разъехались (pipe игнорирует `ENCODE_MODE=crf` при nvenc). Нужен один билдер argv + параметризация режимом ввода.
- **Pipe-режим скачивает raw-файл 3 раза** (по разу на каждый рип), хотя локальная копия для probe/audio уже лежит в tmpdir — экономия стрима ничего не даёт.
- **Нет событий как контрактов**: `video.uploaded` парсится сырым `json.loads`; `video.transcoded` не существует — transcoder дёргает metadata синхронным HTTP PATCH. Нужны versioned Pydantic-модели.
- **Нет publisher confirms** (`confirm_delivery()`) — retry-publish может тихо теряться, at-least-once не гарантирован.
- **Опасное лечение topology в консьюмере**: `queue_delete` живой очереди при mismatch (`consumer.py:903`).
- **Retry/DLQ-логика (`on_message`) не покрыта тестами вообще** — а это самая критичная логика сервиса.
- **Auth**: нет ротации refresh-токенов (нет `jti`, старый refresh жив до 7 дней после выпуска нового), `/refresh` не проверяет существование юзера, лимиты только на login (register/refresh — нет), tokens в JSON body → localStorage (XSS). passlib и python-jose unmaintained → `argon2-cffi` + `pyjwt`. Нет политики паролей.
- **Прод-код с проверкой на MagicMock**: `if not str(type(sz)).endswith("MagicMock'>")` (`consumer.py:694`) — вынести в чистую функцию и тестировать напрямую.
- **`INTERNAL_TOKEN` по умолчанию пустой** — тихо отключает и auth-заголовок, и idempotency-проверку в transcoder.

---

## 3. Фронтенд и визуал

### Рекомендация по библиотеке компонентов

**Взять shadcn/ui** (Radix primitives + Tailwind, компоненты вендорятся в `src/components/ui`, без vendor lock-in). Стилистика проекта уже Tailwind — подходит идеально. Закрывает: доступные формы (сейчас label не связаны с input), диалоги/тосты вместо `window.confirm`/`alert`, DropdownMenu для качества/visibility, тёмную тему через CSS-переменные. Плеер оставить рукописный — текущий hls.js-сетап (nextLevel-логика, recovery, iOS fallback) лучше готовых обёрток, но убрать хак с приватным событием `BUFFER_FLUSHING` (`VideoPlayer.tsx:141-146`).

### State и данные

- **TanStack Query** вместо рукописных `useEffect`+`useState` во всех страницах: уберёт бойлерплейт, даст кэш, дедуп и заменит ручной `setInterval`-поллинг (`refetchInterval` + `enabled` по статусу).
- **Server Components для фида/watch** + `generateMetadata` (OG-теги) + `loading.tsx`/`error.tsx`/`not-found.tsx` — сейчас все страницы `"use client"`, SSR/SEO не используется, роут-левел стейтов нет.
- **Токены → httpOnly cookies** (BFF route handler в Next) вместо localStorage.
- **Zustand-селекторы** (`useAuth((s) => s.userId)`) вместо подписки на весь сторе — сейчас каждый `VideoCard` ререндерится при любом изменении auth.

### Перф

- **Dynamic import hls.js** (~130KB gz в основном бандле сейчас).
- **`next/image`** для превью вместо `<img>` с eslint-disable.
- Убрать dead code: `base()`/`hlsBase()` идентичны, `createVideoMeta` не используется, `renditions` типизируется, но не рендерится.

### UX

- Фикс двойного сабмита (`disabled={progress !== null && progress < 100}` или `uploading`-флаг).
- Отмена загрузки: `AbortSignal` уже протянут через API-слой, но кнопка не создаёт контроллер.
- Drag & drop + клиентская валидация размера (копия обещает «до 5GB», ничего не проверяет).
- Реальные чанки в resumable (сейчас один PUT всего файла, фолбэк тоже одним чанком — `api.ts:224`).
- Единый язык UI (смесь RU/EN строк) или next-intl; единый brand-цвет вместо хардкода `bg-red-500`/`bg-black`; logout с редиректом; `middleware.ts` для защиты `/upload`.

---

## 4. Инфраструктура и CI

- **CI не покрывает фронтенд** — нет `lint-front`/`build` джобов в `.github/workflows/ci.yml`.
- **Нет `.dockerignore` во frontend** — `COPY . .` тащит локальный `node_modules`/`.next` в контекст (на arm64 Mac может сломать билд); `npm ci 2>/dev/null || npm install` глушит ошибки.
- **Next 14.2.5** — есть security-фиксы в 14.2.25+; поднять до последней 14.2.x или 15.
- Redis задеплоен, но не используется ни gateway (rate-limit), ни auth (ревокация токенов) — либо использовать, либо убрать.
- Тесты, которых не хватает: pgx-репозиторий (testcontainers), `HLSAuth`/`HLSTokenHandler`, `on_message` retry/DLQ, e2e на resumable-путь, интеграционный тест auth на реальном Postgres.

---

## 5. План правок

### Фаза 0 — багфиксы и безопасность (первая неделя, по одному PR на скоуп)

1. `upload`: ownership-проверки в Complete/Status/resumable + sentinel-ошибки вместо строк + safe JSON-ответы + лимит размера на resumable PUT (gateway и сервис). → verify: unit-тесты на IDOR-сценарии и битые JSON.
2. `upload`: заменить append-алгоритм на S3 multipart upload. → verify: e2e resumable на 100MB+ файле.
3. `transcoder`: ack только после успешного `update_status`, kill/reap ffmpeg в pipe-режиме, `confirm_delivery()`, валидация `video_id`/`s3_key` из очереди. → verify: тест на «metadata упал → ретрай, не ack».
4. Все сервисы: fail fast на пустой `JWT_SECRET`/`INTERNAL_TOKEN`, CORS — явный allowlist, rate-limit на Redis (или доверенный XFF), убрать наружу порты metadata/upload/auth, подписать Content-Length в presign, `jwt.WithExpirationRequired()`.
5. `frontend`: refresh по 401 с ретеем, фикс двойного сабмита, фикс утечки поллинга, poster для плеера. → verify: ручной сценарий истечения access-токена.

### Фаза 1 — архитектура Go и Python

6. Shared-модуль для Go (go.work): auth/logger/metrics/writeJSON, выравнивание версий; graceful shutdown + таймауты серверов и клиентов. → verify: `make lint-go && make test-go`, деплой не обрывает in-flight загрузки.
7. metadata: sentinel-ошибки, транзакция в Update, валидация до вставки; убрать мёртвый код.
8. transcoder: удалить Celery-стабы и мёртвые зависимости, объединить `transcode_one`/`transcode_one_pipe` в один билдер argv, rethink pipe-режима, отключить опасный `queue_delete`.
9. Контракты: Pydantic-модели `video.uploaded`, событие `video.transcoded`; выбрать одну систему миграций. → verify: обновлён `docs/spec.md:73` + README сервисов.
10. Auth: ротация refresh-токенов с `jti`/revocation (Redis-денилист), лимиты на register/refresh по XFF, IntegrityError на регистрации, min-length пароля, `pyjwt` + `argon2-cffi`.

### Фаза 2 — фронтенд/визуал

11. shadcn/ui: Button/Input/Dialog/Toast/DropdownMenu, тёмная тема, brand-цвет, один язык UI.
12. TanStack Query + RSC для фида/watch + `loading.tsx`/`error.tsx`/`not-found.tsx` + OG-теги.
13. Перф: dynamic import hls.js, `next/image`, zustand-селекторы, убрать приватный hls.js-хак.
14. Фичи: поиск, пагинация, «мои видео», отмена загрузки, drag&drop, реальные чанки в resumable.

### Фаза 3 — инфраструктура

15. CI: `lint-front`/`build-front`; `.dockerignore` для frontend; bump Next; чинить Dockerfile.
16. Тесты: pgx-репозиторий (testcontainers), HLSAuth, `on_message` retry/DLQ, e2e resumable.

---

## Источники находок

Ревью построено на полном чтении исходников всех сервисов; ссылки на строки актуальны на коммит `91efb5b` (main). При правках сверяться с `docs/spec.md:1` (контракты) и `docs/services-pipeline.md:1` (пайплайн).
