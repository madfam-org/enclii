-- 039_platform_admin_rank DOWN
--
-- Drops the platform rank column. Safe to run: no other column was rewritten
-- by the up migration, so nothing has to be reconstructed. After this runs,
-- the API's tenant-scope guard has no platform-admin principals at all — set
-- ENCLII_TENANT_SCOPE_ENFORCE=false as well if you are rolling the enforcement
-- back rather than just the schema (see
-- docs/runbooks/TENANT_SCOPE_ENFORCEMENT_ROLLOUT.md).

DROP INDEX IF EXISTS idx_users_is_platform_admin;

ALTER TABLE users DROP COLUMN IF EXISTS is_platform_admin;

COMMENT ON COLUMN public.users.role IS 'User role for RBAC: admin, developer, or viewer';
