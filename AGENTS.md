# AGENTS.md — Flowix guidelines

> Источник принципов: [multica-ai/andrej-karpathy-skills](https://github.com/multica-ai/andrej-karpathy-skills) (Andrej Karpathy observations). Логика проекта — `docs/spec.md:1` и `docs/services-pipeline.md:1`.

Behavioral guidelines to reduce common LLM coding mistakes. Merge with project-specific instructions as needed.

**Tradeoff:** These guidelines bias toward caution over speed. For trivial tasks, use judgment.

## 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

Before implementing:
- State your assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them - don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

## 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

## 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:
- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd do it differently.
- If you notice unrelated dead code, mention it - don't delete it.

When your changes create orphans:
- Remove imports/variables/functions that YOUR changes made unused.
- Don't remove pre-existing dead code unless asked.

The test: Every changed line should trace directly to the user's request.

## 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:
- "Add validation" → "Write tests for invalid inputs, then make them pass"
- "Fix the bug" → "Write a test that reproduces it, then make it pass"
- "Refactor X" → "Ensure tests pass before and after"

For multi-step tasks, state a brief plan:
```
1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
```

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.

---

**These guidelines are working if:** fewer unnecessary changes in diffs, fewer rewrites due to overcomplication, and clarifying questions come before implementation rather than after mistakes.

---

## Project-Specific Guidelines — Flowix

### Где логика проекта (ходи сюда для уточнения)

- **ТЗ и архитектура (бывший AGENTS.md):** `docs/spec.md:1` — стек, структура монорепо, описание каждого микросервиса, FFmpeg-параметры, кодстайл, коммит-конвенции.
- **Сервисы и пайплайн (на русском):** `docs/services-pipeline.md:1` — зачем каждый сервис (gateway/auth/metadata/upload/transcoder/nginx-vod/frontend) и полный пайплайн `upload → RabbitMQ → transcode → MinIO → HLS` с диаграммой и сценариями отладки.
- **План по фазам:** `docs/PLAN.md:1` — 8 фаз, граф зависимостей, DDL `deploy/postgres/init.sql:1`, контракты событий `video.uploaded`/`video.transcoded`.
- **Swagger / Zed:** `docs/SWAGGER.md:1`, `docs/ZED.md:1` — генерация OpenAPI для Go, настройки IDE.

### Стек и структура (кратко)

- **Go (chi `go-chi/chi/v5`):** `services/gateway`, `services/metadata`, `services/upload` — `golang:1.27-alpine`, `pgx`, `zerolog`, `amqp091-go`. Версионирование `/api/v1`.
- **Python (FastAPI + Celery + uv):** `services/auth` (`src/main.py:1`), `services/transcoder` (`app/celery_app.py:1`, `app/tasks.py:1`) — `python:3.11-slim`, `uv`, `argon2`, `aiobotocore`, `ffmpeg-python`.
- **Infra:** `deploy/docker-compose.yml:1` — `postgres:16-alpine :5432`, `minio/minio :9000/:9001`, `rabbitmq:3-management :5672/:15672`, `nginx-vod :8081`, `gateway :8080`, `frontend :3000`.
- **Frontend:** `frontend/` — Next.js 14 App Router + `hls.js` + `zustand` + `tailwind`.
- **Transcoding JIT:** храним 3 MP4/рип (360/720/1080) с `-force_key_frames "expr:gte(t,n_forced*2)"` и `-sc_threshold 0` (`docs/spec.md:82`), nginx-vod режет на сегменты.

### Команды (via Makefile)

```bash
cp .env.example .env
make up        # docker compose --env-file .env -f deploy/docker-compose.yml up --build -d
make logs && make ps
make lint-py && make lint-go && make lint-front
make fmt-py && make fmt-go
make test-go && make test-py
make swagger   # swag init для metadata+upload
make e2e       # scripts/e2e.sh — upload→transcode→hls
```

Локально без Docker: `make dev-auth` (`:8001`), `make dev-transcoder` (Celery), `make dev-metadata` (`:8002`), `make dev-upload` (`:8003`), `make dev-gateway` (`:8080`), `make dev-frontend` (`:3000`).

### Кодстайл

- Go: `gofmt`, `golangci-lint`, DI, `context.Context`, `zerolog`, вешай `// @Summary` для Swagger.
- Python: `black` (100), `flake8`, `mypy`, type hints обязательны, Pydantic для событий, `ruff --fix` в Zed.
- Frontend: `eslint` + `prettier`, функц. компоненты, `zustand` (не Redux).
- Тесты критичной логики обязательны; `docker-compose` для интеграционных.

### Микросервис-гайд

- Новый сервис → `services/<name>/` + `Dockerfile` + `go.mod`/`pyproject.toml` → допиши `deploy/docker-compose.yml:1` и `.env.example:1`.
- Тяжёлую работу — в очередь (RabbitMQ/Celery), не в хэндлерах.
- Меняешь контракт — обнови `.proto`/Pydantic/Go struct + `docs/spec.md:73` + README сервиса.

### Коммиты — Conventional Commits

`type(scope): short lower-case imperative` — см. `docs/spec.md:256`. Header ≤72, атомарно по scope (`auth`, `transcoder`, `gateway`, `metadata`, `upload`, `frontend`, `infra`, `docs`...), не мешай скоупы. Never commit `.env`, `__pycache__/`, `node_modules/`, `data/`, `tmp/`.

### Для ИИ-агентов в этом репо

- Перед правкой сервиса — читай его `services/*/README.md:1` + `docs/services-pipeline.md:1`.
- Обновляй тесты и доку вместе с кодом; новые env — в `.env.example:1`.
- Следуй 4 принципам выше: думай → упрощай → хирургически → верифицируй (тест до/после).
