-- Add archived_at column and index for archive tracking
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_audit_logs_archived_at ON audit_logs (archived_at) WHERE archived_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs (created_at);

-- Create archive tracking table
CREATE TABLE IF NOT EXISTS audit_archive_batches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    archived_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    record_count INTEGER NOT NULL,
    oldest_record TIMESTAMPTZ NOT NULL,
    newest_record TIMESTAMPTZ NOT NULL,
    r2_key TEXT NOT NULL,
    r2_bucket TEXT NOT NULL DEFAULT 'enclii-audit-archive',
    sha256_hash TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    status TEXT NOT NULL DEFAULT 'completed'
);
