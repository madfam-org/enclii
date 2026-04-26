-- Reverse of 018_namespace_discoverer.up.sql.

DROP INDEX IF EXISTS idx_services_zombie_since;

ALTER TABLE services
    DROP COLUMN IF EXISTS zombie_since,
    DROP COLUMN IF EXISTS last_reconciled_at;

DROP INDEX IF EXISTS idx_discovered_orphans_last_seen;
DROP TABLE IF EXISTS discovered_orphans;
