# Transcoder Worker (Python pika + FFmpeg + uv)

> `INTERNAL_TOKEN` для `PATCH /internal/.../status` (`consumer.py:43`), лимит 5GB в upload.
> Последовательный транскод, `-threads/-preset`, таймаут 900, диск-чек, лимиты контейнера.

## Запуск
```bash
# Локально (нужен RabbitMQ + MinIO + FFmpeg)
uv sync --project services/transcoder
python -m app.consumer  # pika BlockingConnection (issue #62: celery-стабы удалены)
# или
make dev-transcoder  # python -m app.consumer

# Через OrbStack
docker compose -f deploy/docker-compose.yml up -d transcoder rabbitmq minio
docker compose -f deploy/docker-compose.yml logs -f transcoder
```

## Env
- `INTERNAL_TOKEN` — для `PATCH /internal/videos/:id/status` (см. `deploy/docker-compose.yml:186`)
- `FFMPEG_THREADS=2` `FFMPEG_PRESET=veryfast` — лимиты CPU (`.env.example`)
- `UPLOAD_MAX_BYTES` — лимит upload (5GB) не влияет на транскодер

## Линт / формат
```bash
make lint-py  # проверяет и auth и transcoder
uv run --project services/transcoder black --check services/transcoder/app --target-version py314
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
# последовательный транскод, fps из probe, threads/preset
FFMPEG_THREADS=2 FFMPEG_PRESET=veryfast python -m app.consumer
```

## Лимиты
- Последовательный рендеринг (было 3× parallel → OOM), `timeout 900`, `fps` из probe (≤30 preserve, >30 cap 30), `-threads 2 -preset veryfast -maxrate 1.10×`
- `subprocess.run(timeout=900)` на таймауте сам убивает и дожидается ffmpeg — зомби не остаётся (issue #51; pipe-режим с ручным Popen удалён в #62)
- Heartbeat: `_HeartbeatKeeper` (daemon-поток, `process_data_events` каждые 30с) держит pika-соединение живым во время длинных транскодов (issue #51)
- `shutil.disk_usage` чек, `SIGTERM` graceful, `deploy/docker-compose.yml:179` + `prod.yml:44` limits `cpus 2 / mem 4G`
- `.env.example:30` `FFMPEG_THREADS`/`FFMPEG_PRESET`, `make dev-transcoder` = `python -m app.consumer`

## Zed IDE
Аналогично `services/auth/README.md` — `.zed/settings.json:1` → Python `format_on_save` + `ruff` импорты.
