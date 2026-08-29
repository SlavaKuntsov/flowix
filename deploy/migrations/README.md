# Migrations

Single source of truth — `deploy/migrations/*.sql` (golang-migrate).

- `000001_init.up.sql` — baseline from `deploy/postgres/init.sql` (now legacy fallback).
- New change: `make migrate-create name=add_video_visibility` → creates `000002_...up/down.sql`.
- Apply: `make migrate-up` (local) or `migrate` service in `deploy/docker-compose.yml` (auto on `make up`).
- Auth's `services/auth/migrations` (alembic) is kept for Python autogen, but **not used in prod** — prod uses this directory. If you add a column via SQLAlchemy, generate alembic version then port SQL to a new `deploy/migrations/00000N_*.up.sql`.

See `Makefile: migrate-*` and `docs/PLAN.md: Фаза 9`.
