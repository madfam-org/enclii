-- Reverse of 036_managed_db_data_apis.up.sql.
-- Drops the per-addon data-API table and restores the event-ledger CHECK to its
-- pre-035 (migration 014) type set. Safe to run even if the table is empty.

DROP INDEX IF EXISTS idx_managed_db_data_apis_status;
DROP INDEX IF EXISTS idx_managed_db_data_apis_project;
DROP TABLE IF EXISTS public.managed_db_data_apis;

-- Restore the original event-type CHECK (without the data_api.* types).
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
        'addon.plan.changed'
    ));
