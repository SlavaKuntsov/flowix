
# AGENTS.md

## Project Overview

This is a pet project for building a video streaming platform MVP with modern features:
- Adaptive bitrate streaming (HLS/DASH)
- Seamless quality switching (aligned segments, same codec and framerate)
- Playback speed control
- Scalable microservices backend
- Frontend for video playback (Next + hls.js)

The backend is split into microservices written in **Go** and **Python** to leverage the strengths of each language.

---

## Tech Stack

- **Backend (Go)**: API Gateway, Metadata Service, Upload Service, Analytics (future)
- **Backend (Python)**: Auth Service, Transcoding Worker, Thumbnail Generation, Admin Panel (optional)
- **Storage**: MinIO (S3-compatible)
- **Database**: PostgreSQL
- **Message Queue**: RabbitMQ or Redis Streams
- **Streaming Server**: nginx with `nginx-vod-module` (or custom Go service if preferred)
- **Transcoding**: FFmpeg
- **Frontend**: Next + Vite + hls.js (or video.js)
- **Containerization**: Docker, docker-compose

---

## Repository Structure (Monorepo)

```
.
├── services/
│   ├── gateway/               # Go (API Gateway)
│   ├── metadata/              # Go (Metadata Service)
│   ├── upload/                # Go (Upload Service)
│   ├── auth/                  # Python (FastAPI or Django)
│   ├── transcoder/            # Python (Celery worker)
│   └── ... (others as needed)
├── frontend/                  # Next app
├── deploy/                    # Dockerfiles, docker-compose.yml, nginx configs
├── docs/                      # Additional documentation
├── scripts/                   # Utility scripts (dev setup, etc.)
└── README.md
```

Each service has its own `Dockerfile` and can be developed independently.

---

## Microservices

### 1. API Gateway (Go)
- **Purpose**: Single entry point for client requests; routes to appropriate services; handles CORS, rate limiting, request aggregation.
- **Framework**: Gin, Echo, or chi.
- **Communication**: REST or gRPC to internal services.

### 2. Metadata Service (Go)
- **Purpose**: Manage video metadata (title, description, duration, available qualities, status, URLs).
- **Database**: PostgreSQL.
- **API**: CRUD for videos; internal endpoints for other services.

### 3. Upload Service (Go)
- **Purpose**: Accept video uploads from users, store raw files in MinIO, publish `video.uploaded` event to queue.
- **Framework**: Go standard library or Gin.
- **Key features**: Multipart upload, resumable uploads, progress tracking.

### 4. Auth Service (Python)
- **Purpose**: User authentication, JWT issuance, OAuth2 integration.
- **Framework**: FastAPI (or Django + DRF).
- **Database**: PostgreSQL (users, tokens).
- **Communication**: Exposes `/auth/*` endpoints; other services validate JWT via middleware.

### 5. Transcoding Worker (Python)
- **Purpose**: Consume `video.uploaded` events, download original from MinIO, run FFmpeg to produce multiple renditions, upload results, publish `video.transcoded` event.
- **Framework**: Celery with Redis/RabbitMQ broker.
- **Key FFmpeg requirements**:
  - Output formats: H.264/AAC in MP4 container.
  - Multiple renditions (e.g., 360p, 720p, 1080p) with appropriate bitrates.
  - **Aligned segments**: use `-force_key_frames "expr:gte(t,n_forced*2)"` (for 2s segments) and `-sc_threshold 0`.
  - Ensure same framerate and codec across all renditions.
  - For HLS packaging, either pre-segment or use nginx-vod's just-in-time mode (preferred: store single MP4 per rendition, let nginx-vod handle segmenting).
- **Important**: Use `ffmpeg-python` or subprocess calls.

### 6. Streaming Server (nginx-vod-module)
- **Purpose**: Serve HLS/DASH manifests and segments directly from MinIO, with on-the-fly packaging.
- **Configuration**: See `deploy/nginx/nginx.conf`.
- **Alternative**: Custom Go service using `m3u8` library if nginx-vod is not desired.

### 7. (Optional) Thumbnail Service (Python)
- **Purpose**: Generate video thumbnails/posters via FFmpeg and Pillow.
- **Can be merged into transcoding worker** or separate.

---

## Development Setup

### Prerequisites
- Docker and Docker Compose
- Go 1.21+
- Python 3.11+
- Node.js 20+ (for frontend)
- FFmpeg (for local testing)

### Starting the Stack

```bash
# Clone repo
git clone <repo-url>
cd <project>

# Copy environment template
cp .env.example .env

# Build and run all services
docker-compose up --build
```

This will start:
- MinIO on `:9000` (API) and `:9001` (console)
- PostgreSQL on `:5432`
- RabbitMQ on `:5672` (and management UI on `:15672`)
- All microservices
- nginx-vod on `:8080`
- Frontend dev server on `:3000`

### Environment Variables

Refer to `.env.example` for all variables. Key ones:
- `MINIO_ENDPOINT`, `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY`
- `DATABASE_URL`
- `RABBITMQ_URL`
- `JWT_SECRET`
- `VIDEO_STORAGE_BUCKET`

---

## Coding Conventions

### General
- Use clear, descriptive names.
- Add comments for complex logic.
- Follow standard linting/formatting for each language:
  - Go: `gofmt`, `golangci-lint`
  - Python: `black`, `flake8`, `mypy`
  - JavaScript: `eslint`, `prettier`
- Write unit tests for critical business logic.

### Go Services
- Use `net/http` or a lightweight framework (Gin/Echo).
- Prefer dependency injection.
- Use `context.Context` properly for cancellation.
- Structured logging (e.g., `zerolog` or `logrus`).
- API routes should be versioned (`/api/v1/...`).

### Python Services
- Use FastAPI for new services; type hints are mandatory.
- For Celery workers, define tasks in `tasks.py` and use Pydantic models for payloads.
- Use `aiobotocore` for async MinIO operations if needed.
- Logging: standard `logging` with JSON formatter in production.

### Frontend
- Use Next functional components and hooks.
- State management: Next Context or Zustand (no Redux unless necessary).
- hls.js integration as per official docs.
- Keep components small and reusable.

---

## Testing

### Go
```bash
cd services/metadata
go test ./...
```

### Python
```bash
cd services/transcoder
pytest
```

### Integration Tests
- Use docker-compose to spin up dependent services.
- Write tests that simulate full video upload → transcode → stream pipeline.

---

## Deployment Notes

- Use Docker images for each service.
- For production, consider using Kubernetes or a managed container platform.
- Set up a CDN (CloudFront, Cloudflare) in front of the streaming server for global distribution.
- Use horizontal scaling for stateless services; run multiple transcoding workers.

---

## Common Commands

### Build all Docker images
```bash
docker-compose build
```

### Run a specific service locally
```bash
cd services/metadata
go run ./cmd/server
```

### Run transcoding worker locally (with hot reload)
```bash
cd services/transcoder
celery -A app.celery worker --loglevel=info
```

### Run frontend dev server
```bash
cd frontend
npm install
npm run dev
```

### Lint and format
```bash
# Go
gofmt -w .

# Python
black .
flake8 .

# Frontend
npm run lint
```

---

## Guidelines for AI Agents

- When modifying a service, check its `README.md` inside the service directory for specific instructions.
- Always update tests when changing business logic.
- Do not commit `.env` files or secrets.
- Use `docker-compose` for integration testing; avoid relying on local installations of dependencies like PostgreSQL or MinIO.
- If adding a new microservice, create a new directory under `services/`, include a `Dockerfile`, and update `docker-compose.yml`.
- For video transcoding, adhere to the FFmpeg parameters mentioned to ensure seamless quality switching.
- Use the message queue for asynchronous tasks; do not perform heavy processing inside API handlers.
- Keep API contracts consistent; if using gRPC, update `.proto` files and generate code.
- Document any new environment variables in `.env.example`.

---

## Resources

- [nginx-vod-module documentation](https://github.com/kaltura/nginx-vod-module)
- [hls.js API](https://github.com/video-dev/hls.js/blob/master/docs/API.md)
- [FFmpeg HLS encoding guide](https://trac.ffmpeg.org/wiki/EncodingForStreamingSites)
- [MinIO Python SDK](https://docs.min.io/docs/python-client-quickstart-guide.html)
- [FastAPI documentation](https://fastapi.tiangolo.com/)
```

This AGENTS.md provides a clear overview of the project, architecture, and best practices for development and AI-assisted contributions.
