-- Flowix initial schema — golang-migrate version
-- Source: deploy/postgres/init.sql (Phase 1), converted to versioned migration.
-- This file is the single source of truth. init.sql kept as legacy fallback for fresh `docker volume rm`.
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

DO $$ BEGIN
  CREATE TYPE video_status AS ENUM ('uploaded','processing','ready','failed');
EXCEPTION WHEN duplicate_object THEN null;
END $$;

CREATE TABLE IF NOT EXISTS users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email TEXT UNIQUE NOT NULL,
  password_hash TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS videos (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  description TEXT DEFAULT '',
  duration INT,
  status video_status NOT NULL DEFAULT 'uploaded',
  thumbnail_s3_key TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS video_renditions (
  video_id UUID NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
  quality TEXT NOT NULL,
  bitrate INT NOT NULL,
  width INT NOT NULL,
  height INT NOT NULL,
  s3_key TEXT NOT NULL,
  PRIMARY KEY (video_id, quality)
);

CREATE INDEX IF NOT EXISTS idx_videos_owner ON videos(owner_id);
CREATE INDEX IF NOT EXISTS idx_videos_status ON videos(status);
CREATE INDEX IF NOT EXISTS idx_videos_created ON videos(created_at DESC);
