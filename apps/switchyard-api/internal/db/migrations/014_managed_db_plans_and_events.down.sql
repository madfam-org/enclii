-- Reverse of 014_managed_db_plans_and_events.up.sql.
-- Drops the event ledger, unwinds the plan FK on database_addons, drops the
-- plan catalog. Safe to run even if some tables are empty.

DROP TABLE IF EXISTS public.managed_db_addon_events;

ALTER TABLE public.database_addons
    DROP CONSTRAINT IF EXISTS fk_database_addons_plan;

DROP INDEX IF EXISTS idx_database_addons_plan;

ALTER TABLE public.database_addons
    DROP COLUMN IF EXISTS plan;

DROP INDEX IF EXISTS idx_managed_db_plans_available;

DROP TABLE IF EXISTS public.managed_db_plans;
