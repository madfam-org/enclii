DROP INDEX IF EXISTS idx_audit_logs_acting_team;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS acting_on_behalf_of_team_id;

DROP INDEX IF EXISTS idx_admin_acting_sessions_tenant;
DROP INDEX IF EXISTS idx_admin_acting_sessions_active;
DROP TABLE IF EXISTS admin_acting_sessions;
