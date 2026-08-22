# Auth Service (Python FastAPI + uv)

## Запуск
```bash
# Локально (hot reload)
uv sync --project services/auth
uv run --project services/auth uvicorn src.main:app --reload --port 8001
# или
make dev-auth

# Через OrbStack (Docker)
docker compose -f deploy/docker-compose.yml up -d auth postgres rabbitmq
docker compose -f deploy/docker-compose.yml logs -f auth
curl http://localhost:8001/health
curl http://localhost:8001/api/v1/auth/register -X POST -H "Content-Type: application/json" -d '{"email":"a@a.com","password":"12345678"}'
```

## Линт / формат / фикс
```bash
# Проверка (black + flake8 + mypy)
make lint-py
# Или детально:
uv run --project services/auth black --check services/auth/src --target-version py311
uv run --project services/auth flake8 services/auth/src
uv run --project services/auth mypy services/auth/src
uv run --project services/auth ruff check services/auth/src  # если установлен ruff

# Авто-фикс
make fix-py        # ruff --fix + black
make fmt-py        # только black
uv run --project services/auth ruff check --select I --fix services/auth/src  # сортировка импортов
uv run --project services/auth black services/auth/src --target-version py311
```

## Тесты
```bash
uv run --project services/auth pytest -q
uv run --project services/auth pytest --cov
```

## Zed IDE (при сохранении)
Настроено в `.zed/settings.json:1`:
- `Python` → `pyright` + `ruff`, `format_on_save: on`
- При `Cmd+S`: `black` форматирует, `ruff` сортирует импорты (`I`) и фиксит `F/E/W`
- Ручной рефактор: `F2` (rename), `Cmd+.` (quick fix), `Cmd+Shift+P` → `organize imports`

Если не срабатывает: `Cmd+Shift+P` → `editor: restart language server`, проверь `uv sync` выполнен.
