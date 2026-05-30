-- Persist per-service Kubernetes probe configuration (Commercial GA Bet C).
ALTER TABLE services
    ADD COLUMN IF NOT EXISTS health_check JSONB;

COMMENT ON COLUMN services.health_check IS
    'Optional HTTP probe overrides (path, port, http_headers) for reconciler-generated Deployments.';
