-- A retired binding (status 'deleted') is history, not an occupant. The full
-- (addon_id, service_id) constraint made a pair impossible to bind again once
-- any binding for it was retired -- including the automatic retire of a fresh
-- binding whose isolated network policy failed -- and every later attempt
-- surfaced as an opaque 500. Active and suspended bindings still occupy the
-- pair; retired rows stay as audit history.
ALTER TABLE database_addon_bindings DROP CONSTRAINT IF EXISTS unique_binding_per_service;
CREATE UNIQUE INDEX IF NOT EXISTS unique_live_binding_per_service
    ON database_addon_bindings (addon_id, service_id)
    WHERE status <> 'deleted';
