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

## Линт / формат
```bash
make fmt-go
make lint-go
gofmt -w services/upload
go vet ./services/upload/...
```

## Zed IDE
Тот же `.zed/settings.json:1` → Go `format_on_save` + `organizeImports`.
