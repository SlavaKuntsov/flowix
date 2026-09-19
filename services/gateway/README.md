# Gateway (Go chi — reverse proxy)

## Запуск
```bash
docker compose -f deploy/docker-compose.yml up -d gateway
# Gateway :8080 проксирует /api/v1/auth → auth:8001, /api/v1/videos → metadata|upload, /hls → nginx-vod
curl http://localhost:8080/health
curl http://localhost:8080/api/v1/auth/me -H "Authorization: Bearer $JWT"

# Локально:
go run ./services/gateway/cmd/server
```

## Линт / формат
```bash
make fmt-go
make lint-go
```

## CORS и rate-limit (issue #52)

- **CORS** — только явный allowlist: `CORS_ALLOWED_ORIGINS` (comma-separated,
  dev-дефолт `http://localhost:3000`). Origin из списка → отражаем его в
  `Access-Control-Allow-Origin` (+ `Vary: Origin`, credentials включены).
  Чужой origin или пустой список → ACAO-заголовков нет вообще; wildcard `*`
  не поддерживается.
- **Rate-limit** — fixed-window 20rps/burst 40 на Redis (`REDIS_URL`,
  атомарный Lua-скрипт): состояние общее для всех инстансов gateway.
  Redis недоступен → fail-open (availability over strictness), warn в лог
  не чаще раза в минуту.
- **XFF-доверие** — `X-Forwarded-For`/`X-Real-IP` учитываются только если
  непосредственный пир в `TRUSTED_PROXY_CIDRS` (дефолт `172.16.0.0/12` —
  docker-сеть); подделка XFF клиентом не влияет на ключ лимитера.
  Проверенный клиентский IP выставляется в `X-Real-IP` для downstream —
  auth ключует свой slowapi-лимитер по нему.

## HLS-auth: metadata-кэш и общий http client (issue #54)

`HLSAuth` больше не ходит в metadata на каждый `.ts`-сегмент:

- **Один `http.Client`** (`hlsMetaClient`, timeout 3s, keep-alive пул) переиспользуется
  и `HLSAuth`, и `HLSTokenHandler` — без нового клиента на каждый запрос.
- **LRU-кэш** `video_id → metadata` (4096 записей): позитивные ответы TTL 10s
  (смена visibility видна в пределах 15s по ТЗ), негативные (metadata 404/403)
  TTL 5s. Сеть/5xx от metadata не кэшируются. Кэш in-process, на инстанс middleware.
- Поведение authz не изменилось: private без валидного токена владельца → 403,
  не найден в metadata → passthrough к vod (404), metadata недоступна → 502.

## Zed IDE
См. `services/metadata/README.md` — Go `gopls` автоформат при сохранении.

## Общий Go-модуль pkg (issue #63)

AuthMiddleware/OptionalAuth, RequestLogger, метрики Prometheus, `httputil.WriteJSON` и запуск сервера (таймауты Read/Write/Idle + graceful shutdown по SIGTERM/SIGINT) живут в общем модуле `pkg/` (`flowix/pkg`, подключён через `go.work` и `replace` в `go.mod`). Приватные копии в сервисе удалены — правки общих хелперов делай в `pkg/`, а не в сервисе. Таймауты и грейс shutdown: `pkg/httpserver` (env `SHUTDOWN_GRACE` переопределяет грейс, см. `stop_grace_period` в `deploy/docker-compose.yml`).
