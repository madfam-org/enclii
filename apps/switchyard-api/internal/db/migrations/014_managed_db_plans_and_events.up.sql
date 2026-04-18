-- P3.1 Sprint 1: managed-DB plan catalog + lifecycle event ledger.
--
-- Adds:
--   managed_db_plans            plan catalog (standard-0/1/2) — resource presets
--                               Sprint 3 billing will attach prices here.
--   database_addons.plan        column pointing at a plan code (defaults to standard-0
--                               for existing rows; pre-migration addons were all
--                               free-form config with shared-tier defaults).
--   managed_db_addon_events     append-only lifecycle audit trail.
--                               Billing (Sprint 3) reads 'created' / 'destroyed'
--                               events as billable signals.
--
-- Isolation model (see docs/architecture/managed-db-addon.md, D1): cluster-per-addon
-- via CloudNativePG. This migration does NOT touch the provisioning path — it adds
-- data plane concepts only.

-- -----------------------------------------------------------------------------
-- Plan catalog
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS public.managed_db_plans (
    code              varchar(64) PRIMARY KEY,
    engine            varchar(32) NOT NULL DEFAULT 'postgres'
                        CHECK (engine IN ('postgres','redis','mysql')),
    display_name      varchar(255) NOT NULL,
    tier              varchar(32) NOT NULL DEFAULT 'standard'
                        CHECK (tier IN ('standard','ha','dedicated')),
    storage_gb        integer NOT NULL CHECK (storage_gb > 0),
    cpu_request       varchar(32) NOT NULL,      -- e.g. '100m'
    memory_request    varchar(32) NOT NULL,      -- e.g. '256Mi'
    max_connections   integer NOT NULL CHECK (max_connections > 0),
    ha_enabled        boolean NOT NULL DEFAULT false,
    replica_count     integer NOT NULL DEFAULT 1 CHECK (replica_count >= 1),
    available         boolean NOT NULL DEFAULT true,
    price_cents_month bigint NOT NULL DEFAULT 0,  -- filled in Sprint 3
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE public.managed_db_plans IS
  'Catalog of managed-DB addon plans. Sprint 1 seeds standard-0/1/2; Sprint 3 adds prices and HA variants.';
COMMENT ON COLUMN public.managed_db_plans.code IS
  'Customer-facing plan id, e.g. standard-0. Stable contract.';
COMMENT ON COLUMN public.managed_db_plans.price_cents_month IS
  'Monthly price in cents. Zero until Sprint 3 billing cutover.';

CREATE INDEX IF NOT EXISTS idx_managed_db_plans_available
    ON public.managed_db_plans(available)
    WHERE available = true;

-- Seed Sprint 1 plans.
-- Prices intentionally zero; Sprint 3 sets them once Waybill emitters are wired.
INSERT INTO public.managed_db_plans (code, engine, display_name, tier, storage_gb, cpu_request, memory_request, max_connections, ha_enabled, replica_count)
VALUES
  ('standard-0', 'postgres', 'Standard 0 — 1 GB',   'standard',  1, '100m',  '256Mi',  10, false, 1),
  ('standard-1', 'postgres', 'Standard 1 — 10 GB',  'standard', 10, '500m',  '1Gi',    40, false, 1),
  ('standard-2', 'postgres', 'Standard 2 — 50 GB',  'standard', 50, '1000m', '2Gi',   100, false, 1)
ON CONFLICT (code) DO NOTHING;

-- -----------------------------------------------------------------------------
-- Plan pointer on database_addons
-- -----------------------------------------------------------------------------
ALTER TABLE public.database_addons
    ADD COLUMN IF NOT EXISTS plan varchar(64);

-- Backfill existing rows to standard-0 (pre-Sprint-1 addons were ad-hoc configs;
-- standard-0 is the closest conservative match).
UPDATE public.database_addons
   SET plan = 'standard-0'
 WHERE plan IS NULL;

-- Lock the column NOT NULL after backfill, and attach the FK.
ALTER TABLE public.database_addons
    ALTER COLUMN plan SET NOT NULL;

ALTER TABLE public.database_addons
    ADD CONSTRAINT fk_database_addons_plan
    FOREIGN KEY (plan)
    REFERENCES public.managed_db_plans(code)
    ON DELETE RESTRICT;

CREATE INDEX IF NOT EXISTS idx_database_addons_plan
    ON public.database_addons(plan);

-- -----------------------------------------------------------------------------
-- Lifecycle event ledger
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS public.managed_db_addon_events (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    addon_id         uuid NOT NULL REFERENCES public.database_addons(id) ON DELETE CASCADE,
    project_id       uuid NOT NULL REFERENCES public.projects(id) ON DELETE CASCADE,
    event_type       varchar(64) NOT NULL
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
                            'addon.plan.changed'
                        )),
    actor_user_sub   varchar(255),             -- auth sub; NULL for system events
    actor_user_email varchar(255),             -- denormalized for forensics
    details          jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at       timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE public.managed_db_addon_events IS
  'Append-only lifecycle ledger for managed-DB addons (P3.1). Sprint 3 billing reads created/destroyed as billable signals.';
COMMENT ON COLUMN public.managed_db_addon_events.details IS
  'Event-type-specific payload: plan, error_message, binding_service_id, etc.';

CREATE INDEX IF NOT EXISTS idx_managed_db_addon_events_addon
    ON public.managed_db_addon_events(addon_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_managed_db_addon_events_project
    ON public.managed_db_addon_events(project_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_managed_db_addon_events_type
    ON public.managed_db_addon_events(event_type, created_at DESC);
