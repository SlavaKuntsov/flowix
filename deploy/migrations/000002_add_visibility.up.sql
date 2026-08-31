-- Phase 13: HLS CDN auth — add visibility to videos
DO $$ BEGIN
  CREATE TYPE video_visibility AS ENUM ('public','private','unlisted');
EXCEPTION WHEN duplicate_object THEN null;
END $$;

ALTER TABLE videos ADD COLUMN IF NOT EXISTS visibility video_visibility NOT NULL DEFAULT 'public';
CREATE INDEX IF NOT EXISTS idx_videos_visibility ON videos(visibility);
