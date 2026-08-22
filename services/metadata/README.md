# Metadata Service (Go chi)

## Запуск
```bash
# Через OrbStack (рекомендуется — Go 1.22 в образе)
docker compose -f deploy/docker-compose.yml up -d metadata postgres
docker compose -f deploy/docker-compose.yml logs -f metadata
curl http://localhost:8002/health

# Локально (если Go установлен)
go run ./services/metadata/cmd/server
# или с hot reload (air):
go install github.com/cosmtrek/air@latest
air -c services/metadata/.air.toml
```

## Линт / формат / фикс
```bash
make fmt-go   # gofmt -w services/
make lint-go  # golangci-lint run ./services/...

# Детально (локально):
gofmt -w services/metadata
go vet ./services/metadata/...
golangci-lint run ./services/metadata/...  # требует golangci-lint

# В Docker (без локального Go):
docker run --rm -v $PWD:/app -w /app golang:1.22 gofmt -w ./services/metadata
docker run --rm -v $PWD:/app -w /app -v $PWD/.golangci.yml:/etc/golangci.yml golangci/golangci-lint:latest golangci-lint run ./services/metadata/...
```

## Тесты
```bash
go test ./services/metadata/... -v
# или
make test-go
docker run --rm -v $PWD:/app -w /app golang:1.22 go test ./services/metadata/... -v
```

## Zed IDE (при сохранении)
`.zed/settings.json:1` → `Go` → `gopls` + `gofumpt`, `format_on_save: on`, `source.organizeImports`
- При `Cmd+S`: `gofmt` + `goimports` (импорты сортируются), `staticcheck` подсвечивает
- Рефактор: `F2` (rename), `Cmd+Click` (go to def), `Cmd+.` (fix)
- Если не форматирует: `Cmd+Shift+P` → `editor: restart language server`, проверь `gopls` установлен (`go install golang.org/x/tools/gopls@latest`)
