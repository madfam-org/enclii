-- 020_shared_discovered_plan.up.sql
--
-- Adds the `shared-discovered` plan code that the
-- /v1/admin/databases/discover endpoint inserts when backfilling
-- pre-existing logical postgres DBs and standalone Redis Deployments
-- as `database_addons` rows.
--
-- Without this row, every discovery candidate fails the
-- fk_database_addons_plan FK and the /databases page stays stuck on the
-- "No databases yet" empty state even though ~20+ real DBs exist.
--
-- See apps/switchyard-api/internal/api/databases_discover_handler.go
-- (Plan: "shared-discovered" set on every candidate).
--
-- The price is 0 — backfilled rows represent existing shared infra,
-- not net-new provisioned capacity, so they don't bill. The plan is
-- marked engine='postgres' to satisfy the engine CHECK constraint, but
-- redis addons reference it too — the FK only cares about the plan
-- code, not the engine match.

INSERT INTO public.managed_db_plans (
    code, engine, display_name, tier,
    storage_gb, cpu_request, memory_request, max_connections,
    ha_enabled, replica_count, available, price_cents_month
)
VALUES
    ('shared-discovered', 'postgres', 'Shared (discovered)', 'standard',
     1, 'shared', 'shared', 100,
     false, 1, false, 0)
ON CONFLICT (code) DO NOTHING;
