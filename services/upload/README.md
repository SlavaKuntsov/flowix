# Upload Service (Go chi)

## Запуск
```bash
docker compose --env-file .env -f deploy/docker-compose.yml up -d upload minio rabbitmq postgres metadata
curl http://localhost:8003/health
curl http://localhost:8003/swagger/index.html  # Swagger
# Локально:
go run ./services/upload/cmd/server  # или make dev-upload
```

## Swagger
```bash
make swagger  # генерит services/upload/docs
docker compose --env-file .env -f deploy/docker-compose.yml up -d --build upload
open http://localhost:8003/swagger/index.html  # multipart: file + title
open http://localhost:8003/swagger/doc.json
```
Аннотации в `internal/handler/upload.go:30` (`// @Param file formData file true`). См. `docs/SWAGGER.md:1`.

Команды аналогичны `services/metadata/README.md` — замени `metadata` на `upload`.

## Лимиты
`UPLOAD_MAX_BYTES` (по умолчанию 5GB) ограничивает тело multipart-загрузки и resumable-путей (`PUT /api/v1/videos/{id}/resumable` — и полный PUT, и суммарный размер объекта по чанкам). Превышение → `413 Request Entity Too Large`. На gateway тот же лимит через `maxBytesMw`.

## Ownership (issue #46)
`complete`, `GET/PUT resumable` и presign-пути перед записью в `raw/{id}/*` проверяют владельца через metadata internal-API (`GET /internal/videos/{id}`, заголовок `X-Internal-Token` из `INTERNAL_TOKEN`): чужой id → `403`, несуществующий → `404`, metadata недоступна → `502` (fail-closed). `complete` дополнительно сверяет объект в MinIO: пустой или не-`video/*` content-type → `400`.

## S3 multipart upload (issue #56)
Основной флоу загрузки вместо Content-Range append (тот был O(n²): каждый чанк перечитывал и перезаливал весь объект):

- `POST /api/v1/videos/multipart` — создаёт metadata-запись и multipart-сессию для `raw/{id}/original.mp4` → `{video_id, upload_id, chunk_size}` (8MB).
- `GET /api/v1/videos/{id}/multipart` — список загруженных частей; `upload_id` резолвится на сервере (`ListMultipartUploads`), клиент его не хранит — resume работает после потери состояния.
- `POST /api/v1/videos/{id}/multipart/presign-part` `{part_number}` — presigned PUT URL одной части, чанк идёт напрямую в MinIO.
- `POST /api/v1/videos/{id}/multipart/complete` — сервер сам листит части (`ListParts`) и закрывает сессию (`CompleteMultipartUpload`), публикует `video.uploaded`. Нет ни одной части → `400`.

Все маршруты проверяют ownership как resumable. E2E: секция `8b` в `scripts/e2e.sh` (upload с обрывом посередине → resume → complete).

## Линт / формат
```bash
make fmt-go
make lint-go
gofmt -w services/upload
go vet ./services/upload/...
```

## Zed IDE
Тот же `.zed/settings.json:1` → Go `format_on_save` + `organizeImports`.

## Общий Go-модуль pkg (issue #63)

AuthMiddleware/OptionalAuth, RequestLogger, метрики Prometheus, `httputil.WriteJSON` и запуск сервера (таймауты Read/Write/Idle + graceful shutdown по SIGTERM/SIGINT) живут в общем модуле `pkg/` (`flowix/pkg`, подключён через `go.work` и `replace` в `go.mod`). Приватные копии в сервисе удалены — правки общих хелперов делай в `pkg/`, а не в сервисе. Таймауты и грейс shutdown: `pkg/httpserver` (env `SHUTDOWN_GRACE` переопределяет грейс, см. `stop_grace_period` в `deploy/docker-compose.yml`).
