# Transcoder Worker (Python Celery + FFmpeg + uv)

## Запуск
```bash
# Локально (нужен Redis/RabbitMQ + MinIO + FFmpeg)
uv sync --project services/transcoder
uv run --project services/transcoder celery -A app.celery_app worker --loglevel=info
# или
make dev-transcoder

# Через OrbStack
docker compose -f deploy/docker-compose.yml up -d transcoder rabbitmq minio
docker compose -f deploy/docker-compose.yml logs -f transcoder
```

## Линт / формат
```bash
make lint-py  # проверяет и auth и transcoder
uv run --project services/transcoder black --check services/transcoder/app --target-version py311
uv run --project services/transcoder flake8 services/transcoder/app
uv run --project services/transcoder mypy services/transcoder/app
make fmt-py
make fix-py
```

## Тесты
```bash
uv run --project services/transcoder pytest -q
```

## FFmpeg проверка (aligned segments)
```bash
ffprobe -v error -select_streams v:0 -show_entries stream=avg_frame_rate,codec_name -of default=nw=1 renditions/720p.mp4
ffprobe -v error -skip_frame nokey -select_streams v:0 -show_entries frame=pkt_pts_time -of csv 720p.mp4 | head
```

## Zed IDE
Аналогично `services/auth/README.md` — `.zed/settings.json:1` → Python `format_on_save` + `ruff` импорты.
