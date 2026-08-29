"""init — baseline (deploy/migrations is source of truth)

Revision ID: 0001_init
Revises:
Create Date: 2026-08-30

This version is a no-op in prod: real schema lives in
deploy/migrations/000001_init.up.sql (golang-migrate).
Kept so `alembic upgrade head` succeeds in dev and `alembic revision --autogenerate` has a base.
If you add columns via SQLAlchemy, generate a new alembic version
and port its SQL to deploy/migrations/00000N_*.up.sql.
"""
from typing import Sequence, Union

from alembic import op
import sqlalchemy as sa

revision: str = "0001_init"
down_revision: Union[str, None] = None
branch_labels: Union[str, Sequence[str], None] = None
depends_on: Union[str, Sequence[str], None] = None


def upgrade() -> None:
    # No-op: schema managed by deploy/migrations (golang-migrate).
    # Create alembic_version table implicitly via `alembic upgrade`.
    pass


def downgrade() -> None:
    pass
