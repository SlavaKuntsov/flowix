# Upload Service (Go chi)

Команды аналогичны `services/metadata/README.md` — замени `metadata` на `upload`.

## Запуск
```bash
docker compose -f deploy/docker-compose.yml up -d upload minio rabbitmq postgres metadata
curl http://localhost:8003/health
# Локально:
go run ./services/upload/cmd/server
```

## Линт / формат
```bash
make fmt-go
make lint-go
gofmt -w services/upload
go vet ./services/upload/...
```

## Zed IDE
Тот же `.zed/settings.json:1` → Go `format_on_save` + `organizeImports`.
