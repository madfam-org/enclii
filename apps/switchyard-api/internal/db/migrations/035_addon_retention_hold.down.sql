-- Reverse the retention-hold storage (2026-08-17 audit #10).

-- Restore the genesis status set (without pending_deletion). Any rows still in
-- the pending_deletion state must be moved to a value the old constraint
-- accepts before it is re-applied, or the ADD CONSTRAINT fails.
UPDATE database_addons
    SET status = 'deleting'
    WHERE status = 'pending_deletion';

ALTER TABLE database_addons
    DROP CONSTRAINT IF EXISTS valid_addon_status;

ALTER TABLE database_addons
    ADD CONSTRAINT valid_addon_status CHECK (
        (status)::text = ANY ((ARRAY[
            'pending'::character varying,
            'provisioning'::character varying,
            'ready'::character varying,
            'failed'::character varying,
            'deleting'::character varying,
            'deleted'::character varying
        ])::text[])
    );

DROP INDEX IF EXISTS idx_database_addons_deletion_scheduled_at;

ALTER TABLE database_addons
    DROP COLUMN IF EXISTS deletion_scheduled_at;
