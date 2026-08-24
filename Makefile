# OrbStack: docker CLI уже указывает на orbstack socket (context orbstack).
# Если контекст сбился: docker context use orbstack
# .env — единственный в корне, compose явно указывает на него (--env-file), deploy/.env не нужен
COMPOSE=docker compose --env-file .env -f deploy/docker-compose.yml

.PHONY: up down logs ps build lint fmt test e2e swagger swagger-install

up:
	$(COMPOSE) up --build -d
	@echo "infra: postgres :5432, minio :9000/:9001, rabbit :5672/:15672, nginx-vod :8081, gateway :8080"

down:
	$(COMPOSE) down -v

logs:
	$(COMPOSE) logs -f

ps:
	$(COMPOSE) ps

build:
	$(COMPOSE) build

# Go — локально если установлено, иначе через Docker (golang:1.27, golangci-lint)
# Каждый сервис — отдельный go.mod, поэтому линтим per-service
lint-go:
	@for svc in metadata gateway upload; do \
	  echo "==> lint $$svc"; \
	  if which golangci-lint >/dev/null 2>&1; then \
	    (cd services/$$svc && golangci-lint run ./...) || exit 1; \
	  else \
	    docker run --rm -v $(PWD):/app -w /app/services/$$svc golangci/golangci-lint:latest golangci-lint run ./... || exit 1; \
	  fi; \
	done

fmt-go:
	@which gofmt >/dev/null 2>&1 && gofmt -w services/ || docker run --rm -v $(PWD):/app -w /app golang:1.27-alpine gofmt -w ./services

test-go:
	@for svc in metadata gateway upload; do \
	  echo "==> test $$svc"; \
	  if which go >/dev/null 2>&1; then \
	    (cd services/$$svc && go test ./...) || exit 1; \
	  else \
	    docker run --rm -v $(PWD):/app -w /app/services/$$svc golang:1.27-alpine go test ./... || exit 1; \
	  fi; \
	done

# Python (uv) — пути абсолютные из корня, т.к. uv --project не меняет cwd
lint-py:
	uv run --project services/auth black --check services/auth/src --target-version py311
	uv run --project services/transcoder black --check services/transcoder/app --target-version py311
	uv run --project services/auth flake8 services/auth/src
	uv run --project services/transcoder flake8 services/transcoder/app
	uv run --project services/auth mypy services/auth/src
	uv run --project services/transcoder mypy services/transcoder/app

fmt-py:
	uv run --project services/auth black services/auth/src --target-version py311
	uv run --project services/transcoder black services/transcoder/app --target-version py311
	uv run --project services/auth ruff check --select I --fix services/auth/src 2>/dev/null || uv run --project services/auth isort services/auth/src 2>/dev/null || true

fix-py:
	uv run --project services/auth ruff check --fix services/auth/src 2>/dev/null || true
	uv run --project services/auth black services/auth/src --target-version py311
	uv run --project services/transcoder black services/transcoder/app --target-version py311

test-py:
	uv run --project services/auth pytest -q
	uv run --project services/transcoder pytest -q

# local dev via uv / go (требует Go 1.27 локально, иначе используй docker compose up)
dev-auth:
	uv run --project services/auth uvicorn src.main:app --reload --port 8001

dev-transcoder:
	uv run --project services/transcoder celery -A app.celery_app worker --loglevel=info

dev-metadata:
	set -a; . ./.env 2>/dev/null || true; cd services/metadata && go run ./cmd/server

dev-upload:
	set -a; . ./.env 2>/dev/null || true; cd services/upload && go run ./cmd/server

dev-gateway:
	set -a; . ./.env 2>/dev/null || true; cd services/gateway && go run ./cmd/server

dev-frontend:
	cd frontend && npm run dev

# Frontend
lint-front:
	cd frontend && npm run lint

fmt-front:
	cd frontend && npm run format || npx prettier --write .

e2e:
	bash scripts/e2e.sh

# Swagger (Go chi) — генерация docs для metadata и upload
swagger-install:
	go install github.com/swaggo/swag/cmd/swag@latest

swagger:
	@bash -c 'cd services/metadata && (which swag >/dev/null 2>&1 && swag init -g cmd/server/main.go --parseDependency --parseInternal -o docs || ~/go/bin/swag init -g cmd/server/main.go --parseDependency --parseInternal -o docs)'
	@bash -c 'cd services/upload && (which swag >/dev/null 2>&1 && swag init -g cmd/server/main.go --parseDependency --parseInternal -o docs || ~/go/bin/swag init -g cmd/server/main.go --parseDependency --parseInternal -o docs)'
	@echo "swagger generated: services/metadata/docs, services/upload/docs — пересобери: docker compose --env-file .env -f deploy/docker-compose.yml up -d --build metadata upload"
