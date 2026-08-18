-- C1: auto-generated REST API over managed Postgres (the Supabase PostgREST
-- equivalent). This migration adds the persistence for the per-addon data-API
-- feature and widens the addon event ledger to record enable/disable.
--
-- See docs/architecture/data-api-postgrest.md.
--
-- Topology (locked): one PostgREST Deployment per addon, in the addon's
-- namespace (project-<uuid8>), fronting the CloudNativePG cluster. RLS in the
-- tenant DB is the authorization boundary; enclii creates deny-by-default roles
-- and wires the JWT secret, exactly like Supabase.

-- -----------------------------------------------------------------------------
-- Per-addon data-API state
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS public.managed_db_data_apis (
    addon_id          uuid PRIMARY KEY
                        REFERENCES public.database_addons(id) ON DELETE CASCADE,
    project_id        uuid NOT NULL
                        REFERENCES public.projects(id) ON DELETE CASCADE,
    status            varchar(32) NOT NULL DEFAULT 'pending'
                        CHECK (status IN (
                            'pending',
                            'provisioning',
                            'ready',
                            'disabling',
                            'disabled',
                            'failed'
                        )),
    status_message    text NOT NULL DEFAULT '',
    -- Comma-separated list of exposed schemas (PostgREST db-schemas). Default
    -- 'public' matches Supabase. The tenant is responsible for RLS on anything
    -- exposed here.
    schemas           varchar(512) NOT NULL DEFAULT 'public',
    -- Role PostgREST uses for unauthenticated requests. Default 'anon'; created
    -- NOLOGIN with only USAGE on the schema (no table grants → closed by default).
    anon_role         varchar(63) NOT NULL DEFAULT 'anon',
    -- PostgREST connection pool size. Bounded by the plan's max_connections.
    db_pool           integer NOT NULL DEFAULT 10 CHECK (db_pool > 0),
    -- Name of the K8s Secret holding the JWT signing secret. Never the value.
    jwt_secret_name   varchar(253) NOT NULL DEFAULT '',
    -- Public host, e.g. <addon>.<project>.data.enclii.dev.
    host              varchar(253) NOT NULL DEFAULT '',
    -- The Deployment/Service/ConfigMap/Ingress name, e.g. data-<addon>.
    k8s_resource_name varchar(253) NOT NULL DEFAULT '',
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    enabled_at        timestamptz,
    disabled_at       timestamptz
);

COMMENT ON TABLE public.managed_db_data_apis IS
  'One row per managed-Postgres addon with the auto-generated REST API (PostgREST) enabled. See docs/architecture/data-api-postgrest.md.';
COMMENT ON COLUMN public.managed_db_data_apis.schemas IS
  'Comma-separated PostgREST db-schemas. Tenant owns RLS on anything exposed here.';
COMMENT ON COLUMN public.managed_db_data_apis.jwt_secret_name IS
  'Name of the K8s Secret holding the HS256 signing secret. The value never lands in this table or in API responses.';

CREATE INDEX IF NOT EXISTS idx_managed_db_data_apis_project
    ON public.managed_db_data_apis(project_id);

CREATE INDEX IF NOT EXISTS idx_managed_db_data_apis_status
    ON public.managed_db_data_apis(status);

-- -----------------------------------------------------------------------------
-- Widen the addon event ledger CHECK to record data-API lifecycle.
-- The constraint was defined inline in migration 014, so Postgres named it
-- managed_db_addon_events_event_type_check. Drop-and-recreate with the two new
-- types. Idempotent via IF EXISTS.
-- -----------------------------------------------------------------------------
ALTER TABLE public.managed_db_addon_events
    DROP CONSTRAINT IF EXISTS managed_db_addon_events_event_type_check;

ALTER TABLE public.managed_db_addon_events
    ADD CONSTRAINT managed_db_addon_events_event_type_check
    CHECK (event_type IN (
        'addon.create.requested',
        'addon.provisioning.started',
        'addon.ready',
        'addon.failed',
        'addon.destroy.requested',
        'addon.destroyed',
        'addon.binding.created',
        'addon.binding.deleted',
        'addon.credentials.rotated',
        'addon.plan.changed',
        'addon.data_api.enabled',
        'addon.data_api.disabled'
    ));
