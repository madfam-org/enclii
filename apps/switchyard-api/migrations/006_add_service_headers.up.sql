-- 006_add_service_headers: Per-service custom HTTP response headers
-- Enables services to declare custom response headers (e.g., COOP/COEP for SharedArrayBuffer)
-- that are injected via nginx ingress annotations during reconciliation.

ALTER TABLE services ADD COLUMN IF NOT EXISTS headers JSONB DEFAULT '{}';
