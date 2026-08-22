# Zed IDE — настройка для Flowix

Настройки уже в `.zed/settings.json:1`. Zed подхватит их автоматически при открытии проекта.

## Что делает автосохранение

| Язык | LSP | Формат | Импорты | Линт-фикс |
|---|---|---|---|---|
| Python | `pyright` (типы) + `ruff` | `black` (100 строк) | `ruff --select I` (isort) | `ruff --fix` |
| Go | `gopls` | `gofumpt`/`gofmt` | `goimports` (`source.organizeImports`) | `staticcheck` |
| TS/JS | `typescript-language-server` + `eslint` | `prettier` через `eslint` | `source.organizeImports` | `eslint --fix` |

- `format_on_save: on` + `remove_trailing_whitespace_on_save` + `ensure_final_newline_on_save`
- `code_actions_on_format` — при каждом `Cmd+S` импорты сортируются, ошибки линта фиксятся.

## Требования (установи один раз)

```bash
# Zed сам скачает LSP, но для локального запуска нужны:
uv --version                # 0.7+ (уже есть)
go version || echo "Go не нужен локально — собирается в Docker (golang:1.22-alpine)"

# Для Zed Gopls (если не установлен):
go install golang.org/x/tools/gopls@latest

# Для Python линта вне Zed (CLI):
uv sync --project services/auth
uv sync --project services/transcoder
uv run --project services/auth ruff check services/auth/src  # если ставишь ruff

# Frontend
cd frontend && npm install  # после Phase 7 (eslint + prettier)
```

## Как проверить что работает в Zed

1. Открой `services/auth/src/core/security.py:1` — добавь неиспользуемый импорт `import os` — при сохранении он должен исчезнуть (ruff `F401`).
2. Открой `services/metadata/cmd/server/main.go:1` — добавь лишний пробел — при сохранении `gofmt` отформатирует.
3. `Cmd+Shift+P` → `editor: show language server info` — должны быть `pyright`, `gopls`, `eslint` в статусе `running`.

## Ручной рефакторинг в Zed

- **Rename symbol:** `F2` или `Cmd+Click` → `Rename` (через `gopls`/`pyright`).
- **Organize Imports:** `Cmd+Shift+P` → `editor: organize imports` (или просто `Cmd+S`).
- **Fix all:** `Cmd+.` (quick fix) → `Fix all auto-fixable`.
- **Format document:** `Cmd+Shift+I` или `editor: format`.

## Если формат не срабатывает

```bash
# Проверь лог LSP в Zed: Cmd+Shift+P → "zed: open log"
# Перезапусти LSP: Cmd+Shift+P → "editor: restart language server"
# Проверь что .zed/settings.json валиден:
cat .zed/settings.json | python3 -m json.tool
```

## CLI — те же команды что в Zed (для CI)

```bash
make fmt-py    # uv run black services/auth/src services/transcoder/app
make lint-py   # black --check + ruff/flake8 + mypy
make fmt-go    # gofmt -w (в Docker: docker run --rm -v $PWD:/app -w /app golang:1.22 gofmt -w .)
make lint-go   # golangci-lint (в Docker)
make fmt-front lint-front  # prettier + eslint (после Phase 7)
```
