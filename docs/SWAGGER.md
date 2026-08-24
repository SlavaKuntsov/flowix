# Swagger — Go сервисы

Go (chi) не генерит OpenAPI как FastAPI, поэтому используем `swaggo/swag`.

## Где что

| Сервис | Swagger UI | OpenAPI JSON | Исходники |
|---|---|---|---|
| auth (FastAPI) | `http://:8001/docs` , `http://:8001/openapi.json` | авто | `services/auth/src/*` |
| metadata (Go) | `http://:8002/swagger/index.html` | `http://:8002/swagger/doc.json` + `http://:8002/openapi.json` (redirect) | `services/metadata/cmd/server/main.go:1` + `internal/handler/video.go:1` |
| upload (Go) | `http://:8003/swagger/index.html` | `http://:8003/swagger/doc.json` | `services/upload/cmd/server/main.go:1` + `internal/handler/upload.go:1` |

`metadata` — `Video CRUD` + `internal/status`, `upload` — `POST /api/v1/videos/upload` `multipart/form-data` (file+title). `gateway` — прокси, отдельный swagger не нужен (доку — через metadata/upload/auth).

## Установка (macOS, разово)

```bash
# swag CLI
make swagger-install  # go install github.com/swaggo/swag/cmd/swag@latest
# или
go install github.com/swaggo/swag/cmd/swag@latest
# или
brew install swag  # если есть tap

which swag        # /Users/kuntsov/go/bin/swag  или /opt/homebrew/bin/swag
swag --version    # v1.16.4
```

Требует `Go 1.27` (`go version`), `Docker` уже `golang:1.27-alpine` в `services/*/Dockerfile:1`.

## Регенерация (после изменения хэндлера)

Аннотации в коде:
```go
// @Summary Create video metadata
// @Tags videos
// @Accept json
// @Param body body model.CreateVideoRequest true "Create"
// @Security BearerAuth
// @Success 201 {object} model.Video
// @Router /api/v1/videos [post]
func (h *VideoHandler) Create(...) {}
```

Команды:
```bash
# Оба сервиса сразу
make swagger
# Только один
bash -c 'cd services/metadata && swag init -g cmd/server/main.go --parseDependency --parseInternal -o docs'
bash -c 'cd services/upload && swag init -g cmd/server/main.go --parseDependency --parseInternal -o docs'

# Пересобрать контейнеры
docker compose --env-file .env -f deploy/docker-compose.yml up -d --build metadata upload
# или локально
make dev-metadata  # :8002/swagger/index.html
```

Генерируются `services/metadata/docs/docs.go`, `swagger.json`, `swagger.yaml` — коммить их (они импортируются `_ "flowix/metadata/docs"` в `main.go:13`).

## Использование

1. Получи JWT: `POST http://:8001/api/v1/auth/login` `{"email":"a@a.com","password":"..."}`
2. Открой `http://:8002/swagger/index.html` → `Authorize` (иконка замка) → `Bearer <access_token>` → `Authorize`
3. `Try it out` на `POST /api/v1/videos` / `GET /api/v1/videos` — токен подставится в `Authorization: Bearer …` автоматом. `register/login` — публичные, `GET /api/v1/videos` — публичный, `POST/PATCH/DELETE` — требуют `BearerAuth`.

`upload` аналогично: `Authorize` → `POST /api/v1/videos/upload` `Try it out` → `file` (Choose File) + `title`.

## Zed IDE

`.zed/settings.json:1` уже `format_on_save` для Go (`gopls`). Swagger не требует LSP, но `swag init` запускай руками или `make swagger` после правки аннотаций. `Cmd+S` в Zed отформатирует `// @Summary` комменты как обычно.

## CI / lint

`make lint-go` не трогает `docs/` (сгенерированный `// Package docs Code generated`). Если менял аннотации — `make swagger && make build` и закоммить `docs/*`.
