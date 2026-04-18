-- 010_deployment_version_numbers: Heroku-style semantic version numbers for deployments (P2.6).
--
-- Adds a monotonic per-service counter (`v1`, `v2`, …) alongside the existing
-- UUID/digest identifiers. Humans remember numbers, not hex — this is a
-- legibility win for operators looking at deploy rows, rollback dialogs, and
-- audit events.
--
-- Contract:
--   * version_number is INT, 1-indexed, monotonic per service.
--   * Allocated at deploy-start inside the same transaction as the INSERT.
--   * Never reused — rollback creates a NEW deploy row with a NEW version
--     that notes its rolled_back_from / rolled_back_to targets.
--   * Deleted deploys leave gaps in history (immutable post-allocation).
--
-- Denormalization note:
--   The service_id lives on `releases`, not `deployments`. To enforce
--   (service_id, version_number) UNIQUE without a cross-table constraint
--   (which Postgres doesn't support), we denormalize service_id onto
--   `deployments`. It's set at INSERT time by looking up releases.service_id
--   and never mutated. This also makes allocation queries trivial.

-- Add the columns. Nullable to allow the backfill below; we don't NOT NULL
-- them in this migration because (a) rolling deploys may race with a freshly
-- shipped schema and (b) the backfill script handles historical rows. Once
-- the backfill has run in all envs, a follow-up migration can tighten
-- version_number/service_id to NOT NULL.
ALTER TABLE deployments ADD COLUMN IF NOT EXISTS version_number INT;
ALTER TABLE deployments ADD COLUMN IF NOT EXISTS service_id UUID;

-- Backfill service_id from releases.service_id for every existing deployment.
-- Idempotent: only updates rows where service_id is NULL.
UPDATE deployments d
   SET service_id = r.service_id
  FROM releases r
 WHERE d.release_id = r.id
   AND d.service_id IS NULL;

-- Backfill version_number for each service by ordering existing deployments
-- chronologically (created_at ASC) and assigning 1..N. Idempotent: only
-- assigns to rows where version_number is NULL and service_id is now set.
WITH ranked AS (
    SELECT d.id,
           ROW_NUMBER() OVER (PARTITION BY d.service_id ORDER BY d.created_at ASC, d.id ASC) AS rn
      FROM deployments d
     WHERE d.service_id IS NOT NULL
       AND d.version_number IS NULL
)
UPDATE deployments d
   SET version_number = ranked.rn
  FROM ranked
 WHERE d.id = ranked.id;

-- UNIQUE constraint on (service_id, version_number). Partial index excludes
-- the transitional window where either column may still be NULL, so the
-- backfill above is safe to re-run (idempotent) and allocation races surface
-- as a clean unique-violation at the app layer.
CREATE UNIQUE INDEX IF NOT EXISTS idx_deployments_service_version
    ON deployments(service_id, version_number)
 WHERE service_id IS NOT NULL
   AND version_number IS NOT NULL;

-- Supporting index for allocation `SELECT MAX(version_number) WHERE service_id = $1`.
CREATE INDEX IF NOT EXISTS idx_deployments_service_id
    ON deployments(service_id)
 WHERE service_id IS NOT NULL;

COMMENT ON COLUMN deployments.version_number IS 'Heroku-style monotonic counter per service (v1, v2, …). Allocated at deploy-start; never reused even across rollbacks. See P2.6.';
COMMENT ON COLUMN deployments.service_id IS 'Denormalized from releases.service_id to enforce (service_id, version_number) UNIQUE and speed allocation. Immutable post-insert.';
