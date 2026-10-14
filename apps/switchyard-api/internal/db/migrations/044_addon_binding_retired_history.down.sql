-- Restoring full-table uniqueness would have to drop retired binding history
-- once a pair has been bound again; refuse instead of deleting audit rows.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM database_addon_bindings
        GROUP BY addon_id, service_id
        HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION 'cannot restore unique_binding_per_service: retired binding history repeats a pair';
    END IF;
END;
$$;
DROP INDEX IF EXISTS unique_live_binding_per_service;
ALTER TABLE database_addon_bindings ADD CONSTRAINT unique_binding_per_service UNIQUE (addon_id, service_id);
