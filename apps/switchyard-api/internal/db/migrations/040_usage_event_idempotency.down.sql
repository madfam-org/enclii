DROP INDEX IF EXISTS idx_usage_events_idempotency_key;
ALTER TABLE usage_events DROP COLUMN IF EXISTS idempotency_key;
