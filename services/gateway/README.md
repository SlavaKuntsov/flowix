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

## Zed IDE
См. `services/metadata/README.md` — Go `gopls` автоформат при сохранении.
