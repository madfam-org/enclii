-- P3.5: Add framework_slug to releases.
-- Populated by roundhouse build callbacks; maps to the canonical catalog
-- in packages/sdk-go/pkg/frameworks. Nullable: legacy rows are not
-- backfilled (UI falls back to client-side inference).
ALTER TABLE releases ADD COLUMN IF NOT EXISTS framework_slug VARCHAR(64);

CREATE INDEX IF NOT EXISTS idx_releases_framework_slug
  ON releases (framework_slug)
  WHERE framework_slug IS NOT NULL;
