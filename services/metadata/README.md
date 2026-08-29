# Metadata Service (Go chi)

## Запуск
```bash
# Через OrbStack (рекомендуется — Go 1.27 в образе)
docker compose --env-file .env -f deploy/docker-compose.yml up -d metadata postgres
docker compose --env-file .env -f deploy/docker-compose.yml logs -f metadata
curl http://localhost:8002/health
curl http://localhost:8002/swagger/index.html  # Swagger

# Локально (если Go установлен 1.27)
go run ./services/metadata/cmd/server  # или make dev-metadata
# или с hot reload (air):
go install github.com/cosmtrek/air@latest
air -c services/metadata/.air.toml
```

## Swagger
```bash
# Установка swag (разово, macOS)
make swagger-install  # go install github.com/swaggo/swag/cmd/swag@latest
# Генерация после правки аннотаций // @Summary в internal/handler/video.go
make swagger  # cd services/metadata && swag init -g cmd/server/main.go -o docs
docker compose --env-file .env -f deploy/docker-compose.yml up -d --build metadata
open http://localhost:8002/swagger/index.html  # Authorize -> Bearer <token>
open http://localhost:8002/swagger/doc.json
```
См. `docs/SWAGGER.md:1` и `services/metadata/cmd/server/main.go:1` (`_ "flowix/metadata/docs"` + `r.Get("/swagger/*", httpSwagger.WrapHandler)`).

## Линт / формат / фикс
```bash
make fmt-go   # gofmt -w services/
make lint-go  # golangci-lint run ./services/...

# Детально (локально):
gofmt -w services/metadata
go vet ./services/metadata/...
golangci-lint run ./services/metadata/...  # требует golangci-lint

# В Docker (без локального Go):
docker run --rm -v $PWD:/app -w /app golang:1.27-alpine gofmt -w ./services/metadata
docker run --rm -v $PWD:/app -w /app -v $PWD/.golangci.yml:/etc/golangci.yml golangci/golangci-lint:latest golangci-lint run ./services/metadata/...
```

## Тесты
```bash
go test ./services/metadata/... -v
# или
make test-go
docker run --rm -v $PWD:/app -w /app golang:1.27-alpine go test ./services/metadata/... -v
```

## Zed IDE (при сохранении)
`.zed/settings.json:1` → `Go` → `gopls` + `gofumpt`, `format_on_save: on`, `source.organizeImports`
- При `Cmd+S`: `gofmt` + `goimports` (импорты сортируются), `staticcheck` подсвечивает
- Рефактор: `F2` (rename), `Cmd+Click` (go to def), `Cmd+.` (fix)
- Если не форматирует: `Cmd+Shift+P` → `editor: restart language server`, проверь `gopls` установлен (`go install golang.org/x/tools/gopls@latest`)
