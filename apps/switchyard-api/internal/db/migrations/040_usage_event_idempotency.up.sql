-- 040_usage_event_idempotency
--
-- Make waybill's usage-event ingest idempotent.
--
-- WHY THIS IS A SCHEMA CHANGE AND NOT EMITTER-SIDE CARE
-- =====================================================
-- Every emitter of `POST /internal/events` is a best-effort HTTP call made
-- beside a database write it must not be able to fail. Retries are therefore
-- not exceptional, they are the design: a request that times out after the
-- server committed, a controller that restarts and re-observes the same
-- completed backup, a pod rescheduled mid-flight. Before this column,
-- `Collector.Record` minted a fresh uuid per call and inserted
-- unconditionally, so each of those retries became a SECOND usage event for
-- the same real-world transition. Nothing downstream could tell the copies
-- apart, and the aggregator sums what it is given.
--
-- Idempotency an emitter enforces by "being careful" is not idempotency. The
-- only place a duplicate can be refused reliably is the table itself.
--
-- SHAPE
-- =====
-- NULLABLE, with a UNIQUE index rather than a UNIQUE constraint on the
-- column: in Postgres NULLs do not collide in a unique index, so every
-- existing emitter that sends no key keeps its current behaviour exactly —
-- unconditional insert, no dedup, no migration of historical rows. Only a
-- caller that supplies a key opts into dedup. That keeps this change additive
-- and reversible.
--
-- The key is chosen by the emitter and must name the TRANSITION, not the
-- attempt: the addon emitter uses the id of the managed_db_addon_events ledger
-- row the event was derived from, which is minted once per transition and
-- survives any number of delivery retries.
ALTER TABLE usage_events
    ADD COLUMN IF NOT EXISTS idempotency_key TEXT;

COMMENT ON COLUMN usage_events.idempotency_key IS
    'Emitter-chosen key naming the real-world transition this event records. NULL means the emitter opted out of dedup (NULLs do not collide in the unique index below). Must be stable across delivery retries and unique per transition.';

CREATE UNIQUE INDEX IF NOT EXISTS idx_usage_events_idempotency_key
    ON usage_events (idempotency_key)
    WHERE idempotency_key IS NOT NULL;
