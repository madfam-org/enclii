-- P3.6 Tenant data export API
--
-- Customer-initiated per-project data export producing a tarball of:
--   K8s manifests + pg_dump of linked DBs + R2 blob inventory +
--   secret references (no values) + audit timeline.
--
-- SOC 2 portability evidence; Taleb-style antifragility sales asset.
-- See docs/architecture/tenant-export.md.

CREATE TABLE IF NOT EXISTS public.tenant_exports (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id          uuid NOT NULL REFERENCES public.projects(id) ON DELETE CASCADE,

    -- Lifecycle
    status              varchar(16) NOT NULL DEFAULT 'pending'
                          CHECK (status IN ('pending','running','ready','failed','expired','deleted')),

    -- Audit / attribution
    requested_by        varchar(255) NOT NULL,   -- Janua sub / user id / email
    requested_at        timestamptz NOT NULL DEFAULT now(),
    approved_by         varchar(255),            -- nullable; required for prod
    approved_at         timestamptz,

    -- Output
    tarball_r2_key      text,                    -- prefix under tenant-exports/ when multi-part
    tarball_size_bytes  bigint,
    sha256              varchar(80),             -- "sha256:" + 64 hex
    part_count          integer NOT NULL DEFAULT 1 CHECK (part_count > 0),

    -- Failure/forensics
    error_message       text,
    started_at          timestamptz,
    completed_at        timestamptz,

    -- Retention (14 days after ready)
    expires_at          timestamptz,

    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_tenant_exports_project
    ON public.tenant_exports(project_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_tenant_exports_status
    ON public.tenant_exports(status) WHERE status IN ('pending','running');

CREATE INDEX IF NOT EXISTS idx_tenant_exports_expiry
    ON public.tenant_exports(expires_at)
    WHERE status = 'ready' AND expires_at IS NOT NULL;

-- Approved-by must differ from requested-by in production (enforced in app
-- code because the migration doesn't know environment). Documented here.
COMMENT ON TABLE public.tenant_exports IS
  'Customer-initiated tenant export requests (P3.6). Ships tarball of manifests + pg_dump + blob manifest + audit timeline. 14-day R2 retention. HITL approval in prod.';

COMMENT ON COLUMN public.tenant_exports.status IS
  'pending: awaiting HITL approval (prod only). running: pipeline active. ready: tarball in R2 with valid presign. failed: partial tarball purged. expired: past 14d. deleted: soft-deleted by admin.';

COMMENT ON COLUMN public.tenant_exports.requested_by IS
  'Janua sub or user email — whoever hit POST. For prod approvals, approved_by must differ from requested_by.';

COMMENT ON COLUMN public.tenant_exports.tarball_r2_key IS
  'Object key prefix when part_count=1 the whole part001.tar.gz; when >1, the index.json alongside the parts.';
