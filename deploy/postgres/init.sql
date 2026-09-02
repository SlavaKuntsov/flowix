-- Flowix initial schema — Phase 1
-- LEGACY: kept for `docker volume rm` fresh init. Source of truth is deploy/migrations/000001_init.up.sql (golang-migrate).
-- On prod, schema is managed by `migrate` service in deploy/docker-compose.yml, not this file.
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

DO $$ BEGIN
  CREATE TYPE video_status AS ENUM ('uploaded','processing','ready','failed');
EXCEPTION WHEN duplicate_object THEN null;
END $$;

DO $$ BEGIN
  CREATE TYPE video_visibility AS ENUM ('public','private','unlisted');
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
  visibility video_visibility NOT NULL DEFAULT 'public',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- thumbnail for VideoCard preview (added Phase 7 fix)
ALTER TABLE videos ADD COLUMN IF NOT EXISTS thumbnail_s3_key TEXT;
ALTER TABLE videos ADD COLUMN IF NOT EXISTS visibility video_visibility NOT NULL DEFAULT 'public';
ALTER TABLE videos ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
CREATE OR REPLACE FUNCTION update_updated_at_column() RETURNS TRIGGER AS $$ BEGIN NEW.updated_at = now(); RETURN NEW; END; $$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS update_videos_updated_at ON videos;
CREATE TRIGGER update_videos_updated_at BEFORE UPDATE ON videos FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

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
CREATE INDEX IF NOT EXISTS idx_videos_visibility ON videos(visibility);
