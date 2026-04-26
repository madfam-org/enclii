-- Sentry observability integration (Parity Audit Gap #9):
--   Adds an optional per-service override that maps an Enclii service to a
--   Sentry project slug. The /v1/observability/sentry handler defaults the
--   slug to service.name when this column is NULL, so the column is only
--   needed for services whose Sentry project name diverges from the
--   Enclii service name (rare; almost all services share the name).
--
-- Lookup contract:
--   1. Handler resolves service by ID.
--   2. If services.sentry_project_slug IS NOT NULL → use it.
--   3. Otherwise fall back to services.name.
--   4. If Sentry returns 404 for that slug, the handler responds with
--      {configured: true, error_count: null, reason: "no_sentry_project"}
--      so the UI can render gracefully without alarming the operator.
--
-- The handler is a thin admin-only proxy; this column is the only schema
-- change needed for end-to-end Sentry integration.

ALTER TABLE services
    ADD COLUMN IF NOT EXISTS sentry_project_slug VARCHAR(128);

COMMENT ON COLUMN services.sentry_project_slug IS
    'Optional override for the Sentry project slug used by the '
    '/v1/observability/sentry endpoint. Defaults to services.name when NULL. '
    'Set this only when the Enclii service name does not match the Sentry '
    'project slug exactly (e.g. legacy renames).';
