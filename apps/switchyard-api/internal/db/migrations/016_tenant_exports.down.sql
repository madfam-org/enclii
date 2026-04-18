-- Revert P3.6 tenant export schema.

DROP INDEX IF EXISTS public.idx_tenant_exports_expiry;
DROP INDEX IF EXISTS public.idx_tenant_exports_status;
DROP INDEX IF EXISTS public.idx_tenant_exports_project;
DROP TABLE IF EXISTS public.tenant_exports;
