# Issue #63: build context = repo root (см. deploy/docker-compose.yml) —
# рядом копируем общий модуль pkg/. go.work в образ не попадает: сборка
# идёт per-module через replace flowix/pkg => ../../pkg.
FROM golang:1.27-alpine AS builder
WORKDIR /app
COPY pkg/go.mod pkg/go.sum pkg/
COPY services/metadata/go.mod services/metadata/go.sum services/metadata/
RUN cd /app/pkg && go mod download && cd /app/services/metadata && go mod download
COPY pkg/ pkg/
COPY services/metadata/ services/metadata/
WORKDIR /app/services/metadata
RUN CGO_ENABLED=0 go build -o /server ./cmd/server

FROM alpine:3.19
RUN apk add --no-cache curl
COPY --from=builder /server /server
EXPOSE 8002
CMD ["/server"]
HEALTHCHECK --interval=10s --timeout=3s --retries=5 CMD curl -f http://localhost:8002/health || exit 1
