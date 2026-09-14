# Code Review — Flowix (третий проход, 2026-09-13)

> Свежий проход по всему проекту. Фокус — находки, которых НЕТ в `docs/REVIEW.md:1` и `docs/REVIEW-fullstack-2026-09-13.md:1`, плюс перепроверка ключевых старых.
>
> **Scope:** ~90 файлов исходников (gateway/metadata/upload — Go chi; auth — FastAPI; transcoder — pika; frontend — Next.js 14; deploy — compose/nginx/migrations).
> **Tools:** golangci-lint (0 issues по всем трём Go-сервисам). semgrep/gitleaks/bandit/trivy не установлены. CVE-скрипт упал на SSL; зависимости проверены вручную через OSV/NVD (web).
>
> **Итог: 1 CRITICAL (новый, цепочка), 3 HIGH (2 новых + подтверждён IDOR), 4 MEDIUM новых, 1 подтверждённый MEDIUM, пачка LOW.**

---

## Vulnerable dependencies

- **`python-jose[cryptography] >=3.3,<4`** (`services/auth/pyproject.toml`) — **CVE-2024-33663** (algorithm confusion, OpenSSH ECDSA keys) и **CVE-2024-33664** (JWT-бомба: DoS через JWE с высокой степенью сжатия в `decode()`). Затронуты все версии ≤ 3.3.0, проект не поддерживается. Эксплуатация: `/refresh` и `/me` принимают токен от любого клиента — DoS реален; algorithm-конфузия частично купирована пином `algorithms=["HS256"]`.
  Fix: миграция на PyJWT или joserfc; как минимум — лимит размера токена до `decode`.

---

## CRITICAL

### Приватность обходится на уровне data plane: renditions публично анонимны, nginx-vod наружу без auth
**File:** `deploy/docker-compose.yml:88-91`, `deploy/docker-compose.yml:139`, `deploy/nginx/nginx.conf:56-63`, `:70`
**Category:** Broken Access Control (OWASP A01:2021)

```yaml
/usr/bin/mc anonymous set private local/$${VIDEO_STORAGE_BUCKET:-videos} || true &&
/usr/bin/mc anonymous set download local/$${VIDEO_STORAGE_BUCKET:-videos}/renditions || true &&
/usr/bin/mc anonymous set download local/$${VIDEO_STORAGE_BUCKET:-videos}/thumbnails || true &&
...
  nginx-vod:
    ports:
      - "${NGINX_VOD_PORT:-8081}:80"
```

**Problem:** Вся авторизация видео живёт только в gateway (`HLSAuth`). Но data plane торчит наружу мимо gateway:
1. MinIO: префиксы `renditions/` и `thumbnails/` объявлены **анонимно скачиваемыми** (`mc anonymous set download`). MinIO-порт 9000 тоже опубликован (`docker-compose.yml:70`). Любой, зная UUID видео, качает `http://<host>:9000/videos/renditions/{id}/720p.mp4` напрямую — ни JWT, ни HLS-токен не нужны.
2. nginx-vod: `:8081` опубликован, а в `nginx.conf` на `location ^~ /hls/...` нет никакой проверки — HLSAuth реализован в gateway, которого здесь нет. `/hls/{id}/master.m3u8` на 8081 отдаёт приватное видео кому угодно.

Цепочка замыкается через уже известную дыру: публичный `GET /api/v1/videos` (`services/gateway/cmd/server/main.go:129` + `repository/video.go:64` без фильтра visibility) отдаёт **ID приватных видео**. Итог: режим private не защищает ничего — полный обход всей privacy-модели.

**Fix:**
- `mc anonymous set private` на весь bucket; раздачу renditions/thumbnails — только через gateway с авторизацией или presigned GET с TTL.
- Убрать наружную публикацию `:9000` и `:8081` из compose (только internal-сеть); если nginx-vod нужен напрямую — перенести HLSAuth внутрь nginx (auth_request в gateway).
- Добавить фильтр visibility в `List` (закрывает и источник UUID).

**Refs:** OWASP A01:2021; CWE-732 (incorrect permission assignment).

---

## HIGH

### Resumable upload: неограниченное тело запроса + чтение всего файла в память
**File:** `services/upload/internal/handler/resumable.go:114`, `:156`, `services/gateway/cmd/server/main.go:122`, `frontend/src/lib/api.ts:224`
**Category:** DoS / Memory

```go
// resumable.go:114 — PUT без Content-Range
data, err := io.ReadAll(r.Body)
...
// resumable.go:156 — PUT с Content-Range
chunk, err := io.ReadAll(io.LimitReader(r.Body, chunkSize+1))
```

**Problem:** На gateway-роуте `PUT /api/v1/videos/{id}/resumable` (`main.go:122`) нет `maxBytesMw` (он применён только к `/upload`), в самом upload-сервисе `MaxBytesReader` для resumable тоже не ставится. При этом handler делает `io.ReadAll` тела целиком. Фронт шлёт «resumable» файл **одним чанком** (`api.ts:224` — `chunkSize = total - offset`), т.е. 5GB-файл целиком буферизуется в RAM воркера → OOM, один авторизованный запрос кладёт контейнер. Побочно: число чанков не ограничено — объект можно раздуть сверх `UPLOAD_MAX_BYTES` (лимит действует per-request, не per-object).

**Fix:** `r.Body = http.MaxBytesReader(w, r.Body, maxBytes)` в resumable-хендлере (или `maxBytesMw` на gateway-роуте); чанки 5–10MB на фронте; вместо ReadAll+append — S3 multipart upload (`CreateMultipartUpload`/`UploadPart`/`CompleteMultipartUpload`), он же убивает O(n²)-аппенд.

### IDOR в presign-флоу (подтверждение, не исправлено)
**File:** `services/upload/internal/handler/presign.go:126-143`, `services/upload/internal/handler/resumable.go:47`, `:98`
**Category:** Broken Access Control (OWASP A01:2021)

```go
// Complete — проверяется только существование объекта, не владелец
s3Key := fmt.Sprintf("raw/%s/original.mp4", videoID)
if err := h.storage.StatObject(r.Context(), s3Key); err != nil {
```

**Problem:** `Complete`, `GET/PUT .../resumable` по-прежнему не сверяют владельца видео. Любой залогиненный юзер, зная UUID, может: дозаписать в чужой `raw/{id}/original.mp4` (resumable PUT), перезаписать его целиком (PUT без Content-Range), вызвать `complete` и запустить транскод/удаление raw чужого видео. `ownerID` из контекста доступен, но не используется.

**Fix:** Перед `StatObject` звать metadata (internal-эндпоинт уже есть) и сверять `owner_id`; в resumable — то же самое. Ровно как это сделано в `HLSTokenHandler`.

### python-jose: CVE-2024-33663 + CVE-2024-33664
См. раздел «Vulnerable dependencies». HIGH, потому что `decode_token` вызывается на публичных эндпоинтах (`/refresh`, `/me`) и `jwt.decode` обрабатывает произвольные байты от клиента.

---

## MEDIUM

### Регистрация без политики пароля и с регистрозависимым email
**File:** `services/auth/src/schemas.py:5-11`, `services/auth/src/routers/auth.py:46`
**Category:** Auth / Input Validation

```python
class RegisterRequest(BaseModel):
    email: EmailStr
    password: str   # ни min_length, ни сложности
```

**Problem:** Пароль принимается любой длины, включая пустой/однобуквенный (argon2 его честно захеширует). Плюс `EmailStr` не нормализует регистр, а `users.email UNIQUE` в Postgres регистрозависим: `Ivan@x.com` и `ivan@x.com` — два аккаунта; при этом логин с другим регистром не найдёт пользователя (50/50 у всех почтовых провайдеров, где регистр игнорируется).

**Fix:** `password: str = Field(min_length=8)` (или curl-подобную политику), email хранить в lowercase (нормализация перед вставкой + `CITEXT`/функциональный уникальный индекс).

### JWT от HLS лежит в query string
**File:** `services/gateway/internal/middleware/hlsauth.go:161`, `:265`
**Category:** Token leakage

```go
tokenParam := r.URL.Query().Get("token")
...
"url": fmt.Sprintf("/hls/%s/master.m3u8?token=%s", videoID, token),
```

**Problem:** HLS-токен (1 час, доступ к приватному видео) передаётся в URL — попадает в access-логи nginx/gateway/promtail, историю браузера, Referer при переходе с сегментных запросов. Уже есть рабочая альтернатива: `X-HLS-Token`/`Authorization` заголовки (`hlsauth.go:163`), фронт умеет (`VideoPlayer.tsx` xhrSetup).

**Fix:** Отдавать токен только в теле ответа, а URL формировать на клиенте; манифест грузить с заголовком. Query-параметр оставить как fallback и замолчать его в логах.

### Redis наружу без пароля
**File:** `deploy/docker-compose.yml:100-108`
**Category:** Config

**Problem:** `redis:7-alpine` без `--requirepass`, порт 6379 опубликован на 0.0.0.0. Без пароля Redis — классика массовых компрометаций (запись в RDB → RCE / майнеры). Для dev допустимо, но дефолт `make up` = незащищённый Redis на всех интерфейсах хоста.

**Fix:** `command: redis-server --requirepass ${REDIS_PASSWORD}` + `REDIS_URL=redis://:pass@redis:6379/0`.

### INTERNAL_TOKEN пустой → /internal/* полностью открыт
**File:** `services/metadata/internal/middleware/internal.go:11`, `deploy/docker-compose.yml:171`
**Category:** Auth

```go
if secret == "" {
    next.ServeHTTP(w, r)
    return
}
```

**Problem:** Compose передаёт `INTERNAL_TOKEN: ${INTERNAL_TOKEN:-}` — забытая переменная делает `PATCH /internal/videos/{id}/status` и `GET /internal/videos/{id}` публично доступными (порт metadata опубликован, `docker-compose.yml:180`). Там можно выставить любой статус и renditions любого видео. Побочно: transcoder при пустом токене молча теряет idempotency-проверку (`_get_status` возвращает None, `consumer.py`).

**Fix:** Fail fast при старте metadata, если `INTERNAL_TOKEN` пуст (как рекомендуется для JWT_SECRET); пустой токен — только при явном `ENV=dev`.

### JWT в Go-мидлварях: `exp` не обязателен (подтверждение, не исправлено)
**File:** `services/gateway/internal/middleware/auth.go:33-39` (и копии в metadata/upload)
**Category:** Auth

**Problem:** `jwt.Parse` проверяет `exp`, только если он есть; токен без `exp` валиден вечно. `type` тоже опционален. Не исправлено с прошлого ревью.

**Fix:** `jwt.WithExpirationRequired()` (jwt/v5) + обязательная проверка `type == "access"`.

---

## LOW

- `services/upload/internal/queue/publisher.go:58` — авто-хил удаляет очередь через `QueueDelete` без `ifEmpty` — вместе с сообщениями; то же в `consumer.py` main-loop. Нужен `ifEmpty=false→true` + ручная миграция topology.
- `services/gateway/internal/proxy/proxy.go:67` — `ModifyResponse` удаляет `Vary` у **всех** проксированных ответов, включая содержательный `Vary: Accept-Encoding` апстримов.
- `services/upload/internal/handler/presign.go:106-110` — если `CreateVideo` прошёл, а presign упал, metadata-запись остаётся сиротой (видео «uploaded» без объекта). Удалить запись при ошибке.
- `services/transcoder/app/consumer.py:694` — проверка `str(type(sz)).endswith("MagicMock'>")` в продовом коде — утечка тестовой логики в рантайм.
- `frontend/src/app/upload/page.tsx:82` — кнопка активна при `progress === 0` → двойной сабмит запускает две параллельные загрузки (старое, не исправлено).
- `frontend/src/app/watch/[id]/page.tsx:44-47` — при ошибке `getVideo` интервал продолжает поллить вечно (unmount-очистка есть, error-выхода нет).
- `frontend/src/lib/api.ts:274` — presigned URL (валидные креды на 1 час) сохраняется в `localStorage`.
- Все сервисы: `/metrics`, `/docs`, `/openapi.json` доступны без auth; `JWT_SECRET`, пароли Postgres/MinIO/RabbitMQ, Grafana `admin/admin` — дефолты из `.env.example`; postgres на md5 (сознательный tradeoff под pgbouncer, но стоит комментарий-план на scram).

### Подтверждённые (по-прежнему не исправлены) из прошлых ревью
IDOR (выше), `List`/`Get` без visibility, CORS `*` + credentials, обход rate-limit через `X-Forwarded-For`, O(n²) append, видео навсегда «processing» при упавшем `update_status` (`consumer.py:832` + ack `:926`), зомби-ffmpeg в pipe-режиме, отсутствие silent refresh, нет `http.Server` таймаутов/graceful shutdown (все три `main.go`).

---

## Improvements

### [HIGH IMPACT] Один shared Go-модуль вместо трёх копий middleware
**File:** `services/gateway/internal/middleware/auth.go`, `services/metadata/internal/middleware/auth.go`, `services/upload/internal/middleware/auth.go`
**Category:** Architecture / foot-gun

**Why:** `AuthMiddleware`, logger, `writeJSON`, bootstrap скопированы трижды, и копии уже **разошлись**: jwt `v5.3.1` в gateway vs `v5.2.2` в metadata/upload, `go 1.25.0` vs `go 1.23.0`. Каждый фикс (например, `WithExpirationRequired`) придётся делать трижды и забыть один раз. `go.work` + `internal/pkg/` убирает весь класс «пофиксил в двух из трёх».

### [HIGH IMPACT] `Update` в metadata: 4 round-trip и TOCTOU → один UPDATE
**File:** `services/metadata/internal/repository/video.go:85-116`
**Category:** Speed

**Why:** Сейчас: `GetByID` (2 запроса) → до 3 отдельных `UPDATE` → `GetByID` снова (2 запроса). Между read и write другой воркер может изменить запись (TOCTOU), а 6 запросов на каждый PATCH — лишняя нагрузка. Один атомарный запрос делает то же:

```sql
UPDATE videos SET title=COALESCE($1,title), description=COALESCE($2,description),
  visibility=COALESCE($3,visibility)
WHERE id=$4 AND owner_id=$5
RETURNING id, owner_id, title, description, duration, status, visibility, thumbnail_s3_key, created_at, updated_at;
```

`owner_id` в WHERE убирает и отдельную проверку forbidden.

### [MEDIUM IMPACT] HLSAuth: кеш метаданных + общий http.Client
**File:** `services/gateway/internal/middleware/hlsauth.go:105`
**Category:** Speed

**Why:** Каждый `.ts`-сегмент → новый `http.Client{Timeout:2s}` и HTTP-запрос в metadata. Просмотр = 100–300 лишних запросов и TCP-хендшейков. Переиспользуемый клиент + LRU `video_id→meta` с TTL 10–15с (negative cache 5с) сокращают запросы к metadata на ~2 порядка при том же UX смены visibility.

### [MEDIUM IMPACT] Транскодер: один builder ffmpeg-аргументов
**File:** `services/transcoder/app/consumer.py:289-414` vs `:417-560`
**Category:** Readability

**Why:** `transcode_one` и `transcode_one_pipe` дублируют сборку команды на ~60%, уже с расхождениями (в pipe-ветке `-preset` ставится после `-c:v` в другом порядке). Один `build_ffmpeg_argv(input_mode, height, bitrate_k, fps, crf, enc)` — половина файла короче, и следующий параметр (например, `-tune`) добавляется в одном месте.

### [MEDIUM IMPACT] limiter.py: убрать socket-пробу Redis
**File:** `services/auth/src/core/limiter.py:15-33`
**Category:** Readability

**Why:** Ручной `socket.create_connection` для проверки Redis дублирует то, что slowapi сделает сам при первой операции, и ломается на таймаутах/IPv6. Достаточно создать Limiter с `storage_uri` и при `ConnectionError` в обработчике деградировать в память (или просто доверять `REDIS_URL` — сервис и так падает без Redis в compose-зависимостях).

### [LOW IMPACT] Мёртвые метрики
**File:** `services/auth/src/main.py:26-30`, `services/transcoder/app/consumer.py:24-27`
**Category:** Observability

**Why:** `upload_bytes`, `vod_cache_hit`, `rabbitmq_queue_depth`, дублированный `ffmpeg_duration` объявлены, но никогда не инкрементятся — Grafana-дашборды по ним пустые, а чтение кода вводит в заблуждение. Удалить или подключить.

---

## What's actually good

`UpdateStatus` в metadata (`repository/video.go:139-170`) — транзакция + `FOR UPDATE` + идемпотентность «не даунгрейд terminal-статусов»: это правильный образец, остальным репозиториям стоит так же. DLX + retry-очередь с `x-retry-count` в заголовках — корректная схема ретраев без бесконечных циклов. `ModifyResponse`, стирающий CORS-заголовки апстримов, — грамотное решение проблемы двойного `Access-Control-Allow-Origin`. Общий `audio.m4a` для всех рипов с внятным комментарием «почему» — редкая забота о щелчках при переключении качества. golangci-lint чист по всем трём сервисам.

---

## Verdict

Для приватного пет-проекта — рабочая система, но заявленная фича «private video» сейчас **не работает**: CRITICAL-цепочка (анонимные renditions + открытый nginx-vod + публичный List с чужими UUID) обходит её целиком, минуя gateway. Чинить в порядке: (1) MinIO-политики + порты 9000/8081 + visibility-фильтр в List, (2) IDOR в complete/resumable, (3) лимит тела на resumable. Python-jose заменить на PyJWT — час работы. Остальное — по плану прошлых ревью.
