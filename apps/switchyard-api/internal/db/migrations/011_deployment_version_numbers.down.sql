-- 010_deployment_version_numbers: revert P2.6 semantic version numbers.
-- Idempotent via IF EXISTS (same contract as the automatic dirty-recovery in migrations.go).

DROP INDEX IF EXISTS idx_deployments_service_id;
DROP INDEX IF EXISTS idx_deployments_service_version;

ALTER TABLE deployments DROP COLUMN IF EXISTS version_number;
ALTER TABLE deployments DROP COLUMN IF EXISTS service_id;
