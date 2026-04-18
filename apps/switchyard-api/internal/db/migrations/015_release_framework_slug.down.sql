DROP INDEX IF EXISTS idx_releases_framework_slug;
ALTER TABLE releases DROP COLUMN IF EXISTS framework_slug;
