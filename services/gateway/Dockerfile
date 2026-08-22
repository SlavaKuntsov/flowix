FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download || true
COPY . .
RUN CGO_ENABLED=0 go build -o /server ./cmd/server

FROM alpine:3.19
RUN apk add --no-cache curl
COPY --from=builder /server /server
EXPOSE 8002
CMD ["/server"]
HEALTHCHECK --interval=10s --timeout=3s --retries=5 CMD curl -f http://localhost:8002/health || curl -f http://localhost:8080/health || curl -f http://localhost:8003/health || exit 1
