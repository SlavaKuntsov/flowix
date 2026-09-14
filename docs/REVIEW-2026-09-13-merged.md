# Code Review — Flowix. Сводный отчёт трёх ревью (2026-09-13)

> Объединение и дедупликация трёх независимых ревью одного дня:
> 1. `docs/REVIEW.md:1` — построчный аудит безопасности/корректности + архитектура.
> 2. `docs/REVIEW-fullstack-2026-09-13.md:1` — фокус на оптимизацию, архитектурные варианты и визуал.
> 3. `docs/REVIEW-fresh-2026-09-13.md:1` — свежий проход: data-plane обход приватности, CVE, resumable DoS.
>
> Ссылки на строки актуальны на коммит `91efb5b` (main). Контракты — `docs/spec.md:1`, пайплайн — `docs/services-pipeline.md:1`.

## Вердикт

Ядро правильное: `upload → RabbitMQ → transcode → MinIO → HLS`, выровненный GOP, общий `audio.m4a`, DLX+retry с `x-retry-count`, идемпотентный `UpdateStatus` (tx + `FOR UPDATE`), golangci-lint чист. Но:

1. **Заявленная фича «private video» не работает** — приватность обходится целиком на data plane (CRITICAL, цепочка).
2. Слой upload-сервиса — главный очаг IDOR и DoS.
3. Транскодер: зависшие видео и зомби-ffmpeg на пути к отказоустойчивости.
4. Фронт — самый дешёвый рычаг UX: без UI-кита, react-query, silent refresh.

Счёт: **1 CRITICAL, 3 HIGH, 8 MEDIUM, ~15 LOW, 6 improvements.**

---

## 1. CRITICAL

### Приватность обходится на уровне data plane
**Где:** `deploy/docker-compose.yml:88-91,70,139`; `deploy/nginx/nginx.conf:56-63`; `services/metadata/internal/repository/video.go:64`; `services/gateway/cmd/server/main.go:129,145`

Вся авторизация видео живёт только в gateway (`HLSAuth`), но data plane торчит наружу мимо него:

1. **MinIO** (`:9000` опубликован): префиксы `renditions/` и `thumbnails/` объявлены анонимно скачиваемыми (`mc anonymous set download`). Зная UUID, любой качает `http://<host>:9000/videos/renditions/{id}/720p.mp4` — ни JWT, ни HLS-токен не нужны.
2. **nginx-vod** (`:8081` опубликован): в `location ^~ /hls/` нет проверки — `HLSAuth` реализован в gateway, которого здесь нет.
3. **Публичный `GET /api/v1/videos`** отдаёт ID приватных видео (List без фильтра visibility), замыкая цепочку.
4. **`/thumbnails/*`** проксируется в MinIO без auth — превью приватных видео публичны.

**Fix:** `mc anonymous set private` на весь bucket; убрать наружную публикацию `:9000`/`:8081`; visibility-фильтр в `List`/`Get` (`WHERE (visibility='public' OR owner_id=$me)`); thumbnails за тем же `HLSAuth` или signed URL с TTL.
**Refs:** OWASP A01:2021, CWE-732.

---

## 2. HIGH

### H1. IDOR в upload: complete/status/resumable не проверяют владельца
**Где:** `services/upload/internal/handler/presign.go:126-143`, `resumable.go:47,98`

`Complete`, `GET/PUT .../resumable` не сверяют владельца: `ownerID` из контекста доступен, но не используется. Любой залогиненный юзер, зная UUID, дозапишет/перезапишет чужой `raw/{id}/original.mp4` и запустит транскод чужого видео. **Fix:** сверять owner через internal-эндпоинт metadata, как в `HLSTokenHandler`; заодно сверять size/content-type (`complete` сейчас проверяет только существование объекта).

### H2. Resumable upload: неограниченное тело + файл целиком в RAM
**Где:** `services/upload/internal/handler/resumable.go:114,156`; `services/gateway/cmd/server/main.go:122`; `frontend/src/lib/api.ts:224`

На gateway-роуте `PUT .../resumable` нет `maxBytesMw` (он только на `/upload`), в хендлере `MaxBytesReader` тоже нет, при этом `io.ReadAll(r.Body)` читает всё. Фронт шлёт «resumable» файл одним чанком (`chunkSize = total - offset`) → 5GB целиком буферизуется в RAM воркера, один запрос кладёт контейнер. Побочно: объект раздувается сверх `UPLOAD_MAX_BYTES` (лимит per-request, не per-object). **Fix:** `MaxBytesReader` на роут + S3 multipart upload с чанками 5–10MB (он же убивает H3).

### H3. Resumable O(n²): каждый чанк перечитывает и перезаливает весь объект
**Где:** `services/upload/internal/handler/resumable.go:170-183`

Каждый чанк скачивает весь существующий объект в память, аппендит и перезаливает. На 5GB-файле — гигабайты перетрафика. **Fix:** S3 multipart (совместно с H2).

### H4. python-jose ≤ 3.3.0: CVE-2024-33663 + CVE-2024-33664
**Где:** `services/auth/pyproject.toml`, `services/auth/src/core/security.py`

CVE-2024-33663 (algorithm confusion) и CVE-2024-33664 (JWT-бомба, DoS в `decode()`); все версии ≤ 3.3.0 затронуты, проект не поддерживается. DoS реален: `/refresh` и `/me` декодируют произвольные байты клиента. **Fix:** PyJWT или joserfc; минимум — лимит размера токена до decode. passlib тоже unmaintained → `argon2-cffi`.

---

## 3. MEDIUM

### M1. List/Get отдают приватные видео всем (источник UUID для CRITICAL)
**Где:** `services/metadata/internal/repository/video.go:64`, `handler/video.go:110`, `gateway/cmd/server/main.go:129`
**Fix:** `WHERE (visibility='public' OR owner_id=$me)`; `Get` — проверка visibility/owner. Учесть `unlisted` (недоступен в List, доступен по ссылке).

### M2. Токены не обновляются никогда
**Где:** `frontend/src/store/auth.ts:39`, `frontend/src/lib/api.ts:76-93`
`refresh_token` кладётся в localStorage и ни разу не читается; при истечении access — молчаливый logout. Нет 401-обработки и refresh-ретея. **Fix:** 401-интерцептор + refresh retry + очередь запросов.

### M3. Токены в localStorage; JWT в query string
**Где:** `frontend/src/auth.ts:29,48`; `gateway/internal/middleware/hlsauth.go:161,265`
XSS-кража токенов; HLS-токен (1 час) в URL попадает в access-логи/promtail/Referer. **Fix:** на фронте — httpOnly cookies через BFF; в gateway — токен только в теле ответа, заголовочные варианты уже работают.

### M4. CORS `*` + `Allow-Credentials: true`
**Где:** `gateway/internal/middleware/cors.go:52-58`
Фактически «любой origin с куками». **Fix:** явный allowlist origin из env.

### M5. Rate-limit обходится
**Где:** `gateway/internal/middleware/ratelimit.go:19-99`, `auth/src/core/limiter.py:15-33`
In-memory по IP, доверяет клиентскому `X-Forwarded-For` (первый IP подделывается); auth slowapi ключует по IP гейтвея — один абузер блокирует всех. Redis задеплоен, но не используется. **Fix:** rate-limit на Redis, доверенный XFF только от известного прокси (или считать по RemoteAddr, раз gateway — точка входа).

### M6. JWT: секрет с дефолтом, `exp` не обязателен, INTERNAL_TOKEN пустой = open
**Где:** `gateway/cmd/server/main.go:24`, `metadata/cmd/server/main.go:57`, `upload/cmd/server/main.go:38`, `auth/src/core/config.py:7`; все три Go auth-мидлвари; `metadata/internal/middleware/internal.go:11`
Забытая переменная = тихо небезопасный деплой; токен без `exp` валиден навсегда; пустой `INTERNAL_TOKEN` делает `/internal/*` публично доступными (порт metadata опубликован) и молча отключает идемпотентность transcoder. **Fix:** fail fast при старте; `jwt.WithExpirationRequired()`; InternalAuth — constant-time compare.

### M7. Видео навсегда «processing»
**Где:** `transcoder/app/consumer.py:832-926`
Если `update_status("ready")` упал, transcoder всё равно ack-ает — ретрая не будет никогда. **Fix:** ack только после успешного update_status; иначе basic_nack → retry-очередь.

### M8. Redis наружу без пароля
**Где:** `deploy/docker-compose.yml:100-108`
`redis:7-alpine` без `--requirepass`, порт на 0.0.0.0 — классический путь компрометации. **Fix:** requirepass + пароль в REDIS_URL. Рядом: дефолтные креды (MinIO minioadmin, Postgres flowix, Grafana admin/admin, md5-аутентификация Postgres — сознательный tradeoff под pgbouncer, нужен план на scram).

### M9. Регистрация: нет политики пароля, регистрозависимый email, гонка
**Где:** `services/auth/src/schemas.py:5-11`, `routers/auth.py:46-51`
Пароль любой длины (включая пустой); `EmailStr` не нормализует регистр, а `users.email UNIQUE` регистрозависим — `Ivan@x.com` и `ivan@x.com` — два аккаунта; SELECT-then-INSERT без ловли `IntegrityError` — конкурентная регистрация даёт 500. **Fix:** `min_length=8`, email в lowercase (CITEXT), `IntegrityError` → 409.

### M10. Порты сервисов торчат наружу
**Где:** `deploy/docker-compose.yml` (auth 8001, metadata 8002, upload 8003)
Клиенты идут мимо гейтвея, минуя rate-limit и CORS. **Fix:** публиковать только gateway (+frontend), остальные — internal.

### M11. Пресigned PUT без лимита размера
**Где:** `upload/internal/storage/minio.go:65-105`
`Content-Length` не подписан — URL позволяет залить произвольный объём за 1 час. **Fix:** presign с условием размера (POST policy) или сверка в `complete` (см. H1).

---

## 4. LOW (сгруппировано)

**Transcoder:** зомби-ffmpeg в pipe-режиме при ошибке стрима/таймауте (`consumer.py:518-559`); heartbeat 600с против ffmpeg 900с×3 — соединение умрёт на 5GB (`consumer.py:877-980`); проверка MagicMock в продовом коде (`:694`); pipe-режим качает файл всё равно + ещё раз на каждый рип (`:719-783`); `FANOUT_QUEUES` объявлены, но не потребляются (`:65,973`).

**Go-сервисы:** ошибки через сравнение строк `err.Error() == "forbidden"`, любая ошибка БД → 404 (`metadata/internal/handler/video.go:220-228`); интерполяция `err.Error()` в рукописный JSON — битый JSON + утечка инфры (`upload.go:86,114`, `presign.go:87,101`, `resumable.go:120`); create-в-метадате валидирует после вставки (`video.go:84-98`); SigV4-баг в fallback-presign (host после подписи — `minio.go:87-96`, частично починен в `PresignedPutObjectExternal`); `QueueDelete(ifEmpty=false)` при лечении topology удаляет живую очередь с сообщениями (`publisher.go:58`, `consumer.py` main-loop); `respWriter` без `http.Flusher`; `http.Client` без таймаута (`upload/internal/client/metadata.go:16`); proxy стирает `Vary` у всех ответов (`proxy.go:67`); presign-сирота при упавшем presign (`presign.go:106-110`).

**Frontend:** двойной сабмит при `progress === 0` (`upload/page.tsx:82`); утечка поллинга при ошибке (`watch/[id]/page.tsx:44-47`); presigned URL в localStorage (`api.ts:274`); `as any` и приватный `BUFFER_FLUSHING`-хак (`VideoPlayer.tsx:110,141-146`).

**Инфра/наблюдаемость:** `/metrics`, `/docs`, `/openapi.json` без auth; мёртвые метрики (`upload_bytes`, `vod_cache_hit`, `rabbitmq_queue_depth`, `ffmpeg_duration` в auth) — объявлены, никогда не инкрементятся; e2e-логин с паролем `string`.

---

## 5. Архитектура

### Go-сервисы
- **Тройная копипаста** `AuthMiddleware`/logger/metrics/writeJSON/bootstrap, версии уже разошлись (jwt v5.3.1 vs v5.2.2, go 1.25.0 vs 1.23.0, minio v7.3.0 vs v7.0.70). → shared-модуль через `go.work`.
- **Нет graceful shutdown и таймаутов** `http.Server` ни в одном сервисе (slowloris + обрыв загрузок при деплое).
- **HLS-auth сам себе DDoS:** HTTP в metadata на каждый `.ts`-сегмент, новый `http.Client` без пула (`hlsauth.go:98-107`). → LRU `video_id→meta` 10–15с + negative cache + переиспользуемый клиент (~2 порядка меньше запросов).
- **Update в репозитории:** read-check-write (TOCTOU) + до 3 UPDATE без транзакции (`repository/video.go:85-116`) → один `UPDATE ... WHERE id AND owner_id RETURNING`. `UpdateStatus` — образец, так и надо.
- `Create` делает лишний синхронный `SELECT email` на каждую вставку (`video.go:29`).
- Мёртвый код: `OptionalAuth`, `NewWithPrefix`, `RemoveObject`, дублированный `writeJSON`.

### Python-сервисы
- **`consumer.py` — god-file 998 строк**: разбить на `queue.py/ffmpeg.py/pipeline.py`, один `build_ffmpeg_argv` (сейчас `transcode_one`/`transcode_one_pipe` дублируют ~60% и уже разъехались).
- **Мёртвый Celery**: `celery_app.py`/`tasks.py` — стабы, пакеты `celery/redis/boto3/httpx/python-multipart` не используются. Удалить.
- **Нет событий как контрактов**: `video.uploaded` парсится сырым `json.loads`; `video.transcoded` не существует. → versioned Pydantic-модели; позже SSE-пуш вместо поллинга на watch.
- **Нет publisher confirms** (`confirm_delivery()`) — retry-publish может тихо теряться.
- **Split-brain миграций**: alembic в auth — no-op, схема живёт в golang-migrate. Выбрать один.
- Auth: нет ротации refresh-токенов (нет `jti`, старый жив до 7 дней), `/refresh` не проверяет юзера, лимиты только на login.

### Стратегические варианты (из ревью 2)
1. **nginx-vod — риск** (модуль Kaltura заброшен). Вариант A (рекомендация для MVP): прегенерить HLS в транскодере, MinIO + CDN. Вариант B: пиннуть форк + fallback на static.
2. **Пуш вместо поллинга**: `video.transcoded` → gateway SSE `/api/v1/videos/{id}/events` → фронт.
3. **Единый контракт**: `openapi.json` как source of truth + `oapi-codegen` + `openapi-typescript` (Go struct / Pydantic / TS `Video` дрейфуют).
4. **Compose-профили** (`core / monitoring / gpu`), FE standalone + distroless.
5. **Наблюдаемость**: OTel gateway→services + `trace_id`, Sentry FE/BE, pprof.

---

## 6. Фронтенд и визуал (главный рычаг UX)

Сейчас: голый Tailwind, `confirm()/alert()`, смесь RU/EN, `<img>` с eslint-disable, ручные `useEffect`-фетчи, кастомный плеер 194 строки.

| Задача | Взять | Почему |
|--------|-------|--------|
| Дизайн-система | **shadcn/ui + Radix** | Вендорится в репо, Tailwind уже есть, a11y. Заменяет `confirm/alert`, хардкод `bg-red-500/bg-black` |
| Плеер | **Vidstack** (hls.js внутри) | Готовые качество/скорость/PiP; убирает ~120 строк ручного `nextLevel`-кода. Минимум — убрать `BUFFER_FLUSHING`-хак |
| Данные | **TanStack Query** | Кеш ленты, `refetchInterval` вместо ручного `setInterval`, инфинит-скролл |
| Формы | **react-hook-form + zod** | login/register/upload валидация |
| Загрузка | **Uppy / react-dropzone + tus** | Drag&drop, чанки, resume, отмена |
| Картинки | **next/image** | Вместо `<img>` в `VideoCard.tsx:44` |

Также: Server Components для фида/watch + `generateMetadata` (OG), `loading/error/not-found.tsx`, `middleware.ts` защита `/upload`, тёмная YouTube-like тема, единый язык UI (next-intl), dynamic import hls.js (~130KB gz из бандла), zustand-селекторы (каждый `VideoCard` сейчас ререндерится на изменение auth), фичи: поиск, пагинация, «мои видео», отмена загрузки.

---

## 7. Инфраструктура и CI

- **CI не покрывает фронтенд** — нет `lint-front`/`build` джобов.
- **Нет `.dockerignore` во frontend** — `COPY . .` тащит `node_modules`/`.next`; `npm ci 2>/dev/null || npm install` глушит ошибки.
- **Next 14.2.5** — security-фиксы в 14.2.25+ (middleware bypass); поднять до последней 14.2.x/15.
- Redis задеплоен, но не используется ни gateway (rate-limit), ни auth (ревокация) — либо использовать, либо убрать.
- Тесты, которых не хватает: pgx-репозиторий (testcontainers), `HLSAuth`/`HLSTokenHandler`, `on_message` retry/DLQ (самая критичная логика трансформатера), e2e на resumable-путь, auth на реальном Postgres.

---

## 8. Improvements (upside-пасс)

1. **[HIGH] Shared Go-модуль** — копии уже разошлись; убирает класс «пофиксил в двух из трёх».
2. **[HIGH] `Update` в metadata → один атомарный UPDATE** — 6 запросов → 1, TOCTOU исчезает.
3. **[MED] HLSAuth: LRU-кеш + общий http.Client** — ~2 порядка меньше запросов к metadata.
4. **[MED] Один builder ffmpeg-аргументов** — половина `consumer.py` короче.
5. **[MED] Убрать socket-пробу Redis из `limiter.py`** — slowapi сам деградирует при ConnectionError.
6. **[LOW] Удалить мёртвые метрики** — дашборды пустые, код вводит в заблуждение.

---

## 9. План правок (объединённый)

> Порядок выполнения задан фазами **16 → 17 → 18** (лейблы `phase:16/17/18` на issues #43–64, продолжение нумерации после фазы 15 из `docs/PLAN.md`). Внутри фазы — по приоритету P0→P2. Фаза 17 начинается со shared Go-модуля (#63): формально он P2, но CORS/rate-limit/HLSAuth-правки (P1) лучше делать уже в общем модуле, чтобы не чинить три копии дважды. Долгие фазы C/D (UI-кит, стратегия) — вне этого бэклога, по мере готовности 16–18.

### Phase 16 — security hotfix (первая неделя)

`phase:16`: **#43** MinIO-политики + порты 9000/8081 → **#44** visibility-фильтр List/Get → **#45** thumbnails за auth → **#46** IDOR в complete/resumable → **#47** лимит тела resumable → **#48** python-jose → pyjwt → **#53** fail fast на секретах + exp обязателен.

Verify: приватное видео недоступно ни одним путём (9000/8081/List/Get/thumbnails/HLS); токен без exp → 401; `make test-go && make test-py` зелёные.

### Phase 17 — hardening и refactors

`phase:17`: **#63** shared Go-модуль + graceful shutdown ← первым → **#52** CORS allowlist + rate-limit на Redis → **#54** HLSAuth: кеш + общий http.Client → **#55** Redis requirepass + закрыть порты сервисов → **#50** ack после успешного update_status → **#51** kill/reap ffmpeg → **#57** один атомарный UPDATE → **#58** валидация до вставки + sentinel-ошибки → **#59** структурированные JSON-ошибки → **#62** один argv-билдер + удалить Celery-стабы.

Verify: `make lint-go && make test-go && make test-py`, e2e зелёный; подделка XFF не обходит лимит; симуляция ошибки стрима не оставляет зомби-ffmpeg.

### Phase 18 — UX и auth lifecycle

`phase:18`: **#60** silent refresh по 401 (фронт) + **#64** ротация refresh с `jti`/revocation (бэк) — одно флоу, делать парой → **#61** двойной сабмит + утечка поллинга → **#56** S3 multipart с чанками 5–10MB.

Verify: истечение access — прозрачный refresh; e2e resumable на 100MB+ с обрывом посередине.

### Бэклог за рамками фаз 16–18 (не срочное)

- shadcn/ui, TanStack Query + RSC, Vidstack/Uppy, next/image, единый язык UI.
- CI: lint-front/build-front; `.dockerignore`; bump Next 14.2.5.
- Стратегия: static HLS vs nginx-vod, SSE вместо поллинга, OpenAPI codegen, OTel/Sentry, compose-профили.
- Тесты: pgx testcontainers, HLSAuth, on_message retry/DLQ, e2e resumable, auth на реальном Postgres.

---

## 10. Трассировка в таски GitHub Projects

Каждая находка выше замаплена на issue в `SlavaKuntsov/flowix` (лейблы `phase:backlog` + `area:*`, поля Priority/Phase/Area). Маппинг — в теле каждого issue. Быстрые LOW-фиксы объединены в задачи по скоупу, чтобы не плодить 30 issue.
