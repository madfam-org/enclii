-- Reverse of 019_sentry_project_slug.up.sql.

ALTER TABLE services
    DROP COLUMN IF EXISTS sentry_project_slug;
