-- 039_platform_admin_rank
--
-- ADR-003 (docs/architecture/ADR_003_TENANT_ADMIN_SCOPE.md, ruling R21):
-- `platform_admin` is strictly above `tenant_admin`, and a tenant admin can
-- never touch another tenant.
--
-- WHAT THIS MIGRATION DOES AND DELIBERATELY DOES NOT DO
-- =====================================================
-- It adds ONE column: users.is_platform_admin. That column — not a role
-- string — is the authority for the platform rank. Role strings reach the
-- request context from places a tenant can influence (an API token's
-- `scopes` list is copied verbatim into user_roles by
-- internal/middleware/auth.go, and the same list is honoured as a role by
-- internal/auth/jwt_middleware.go), so a rank that could be asserted by a
-- role string would be a rank a tenant admin could mint for themselves.
--
-- It does NOT rewrite users.role. Every existing 'admin' row STAYS 'admin'
-- and is read by the API as `tenant_admin` (auth.NormalizeRole). That is the
-- mapping ADR-003 requires — "Existing `admin` principals map to
-- `tenant_admin`, not to `platform_admin`" — and it is also the mapping with
-- no rollback cost: the down migration drops a column instead of trying to
-- reconstruct which of two ranks a rewritten string used to be.
--
-- It does NOT set is_platform_admin=true for anyone. The rank is granted from
-- an explicit operator allow-list at startup
-- (ENCLII_PLATFORM_ADMIN_EMAILS, falling back to ENCLII_ADMIN_EMAILS — see
-- internal/auth/platform_admin.go), never from an email domain, never from a
-- pattern, and never from this file. A public repository is the wrong place
-- to name the estate's operators.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS is_platform_admin boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN public.users.is_platform_admin IS
    'ADR-003 platform rank. TRUE is the only cross-tenant principal. Reconciled at API startup from the ENCLII_PLATFORM_ADMIN_EMAILS allow-list; never granted by a role string or a JWT claim.';

COMMENT ON COLUMN public.users.role IS
    'Tenant-scoped RBAC rank: admin (read as tenant_admin per ADR-003), developer, or viewer. Cross-tenant reach comes from users.is_platform_admin, not from this column.';

-- Partial index: the platform-admin set is a handful of rows out of the whole
-- users table, and the guard asks "is THIS user one of them?" on requests that
-- have already missed the cheaper membership checks.
CREATE INDEX IF NOT EXISTS idx_users_is_platform_admin
    ON public.users (id)
    WHERE is_platform_admin;
