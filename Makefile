# OrbStack: docker CLI уже указывает на orbstack socket (context orbstack).
# Если контекст сбился: docker context use orbstack
# .env — единственный в корне, compose явно указывает на него (--env-file), deploy/.env не нужен
COMPOSE=docker compose --env-file .env -f deploy/docker-compose.yml

.PHONY: up down logs ps build lint fmt test e2e

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

# Go
lint-go:
	golangci-lint run ./services/...

fmt-go:
	gofmt -w services/

test-go:
	go test ./services/...

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

# local dev via uv
dev-auth:
	uv run --project services/auth uvicorn src.main:app --reload --port 8001

dev-transcoder:
	uv run --project services/transcoder celery -A app.celery_app worker --loglevel=info

# Frontend
lint-front:
	cd frontend && npm run lint

fmt-front:
	cd frontend && npm run format || npx prettier --write .

e2e:
	bash scripts/e2e.sh
