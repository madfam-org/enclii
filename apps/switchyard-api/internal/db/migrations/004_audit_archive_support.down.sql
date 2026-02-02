DROP TABLE IF EXISTS audit_archive_batches;
DROP INDEX IF EXISTS idx_audit_logs_created_at;
DROP INDEX IF EXISTS idx_audit_logs_archived_at;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS archived_at;
