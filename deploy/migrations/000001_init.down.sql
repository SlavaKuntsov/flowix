DROP INDEX IF EXISTS idx_videos_created;
DROP INDEX IF EXISTS idx_videos_status;
DROP INDEX IF EXISTS idx_videos_owner;
DROP TABLE IF EXISTS video_renditions;
DROP TABLE IF EXISTS videos;
DROP TABLE IF EXISTS users;
DO $$ BEGIN
  DROP TYPE IF EXISTS video_status;
EXCEPTION WHEN undefined_object THEN null;
END $$;
-- keep pgcrypto extension (shared)
