DROP INDEX IF EXISTS idx_videos_visibility;
ALTER TABLE videos DROP COLUMN IF EXISTS visibility;
DO $$ BEGIN
  DROP TYPE IF EXISTS video_visibility;
EXCEPTION WHEN undefined_object THEN null;
END $$;
