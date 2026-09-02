DROP TRIGGER IF EXISTS update_videos_updated_at ON videos;
DROP FUNCTION IF EXISTS update_updated_at_column();
ALTER TABLE videos DROP COLUMN IF EXISTS updated_at;
