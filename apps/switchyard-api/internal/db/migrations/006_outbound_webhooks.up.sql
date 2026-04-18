-- 006_outbound_webhooks: Customer-configurable outbound lifecycle webhooks
-- Mirrors Stripe's signed-webhook pattern: HMAC-SHA256 with timestamp header.
-- Distinct from the Slack/Discord notification destinations in genesis —
-- these are for external systems (CI tools, dashboards, IDPs, pagers)
-- that want to subscribe to Enclii lifecycle events via their own HTTPS URL.

CREATE TABLE outbound_webhook_subscriptions (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id           UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name                 TEXT NOT NULL,
    url                  TEXT NOT NULL,
    -- First 8 chars of SHA-256(signing_secret). Display only; raw secret
    -- is stored encrypted in a linked K8s Secret (RFC 0005) — for now we
    -- keep an encrypted blob in secret_encrypted until the vault integration
    -- lands. The prefix lets operators identify which secret belongs to
    -- which subscription without exposing the plaintext.
    secret_sha256_prefix TEXT NOT NULL,
    secret_encrypted     BYTEA NOT NULL,
    -- Which event types the subscriber wants. Empty array = all events.
    event_types          TEXT[] NOT NULL DEFAULT '{}',
    active               BOOLEAN NOT NULL DEFAULT TRUE,
    -- Audit
    created_by           TEXT NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at           TIMESTAMPTZ,
    -- Delivery stats (denormalized for quick status reads)
    last_success_at      TIMESTAMPTZ,
    last_failure_at      TIMESTAMPTZ,
    consecutive_failures INT NOT NULL DEFAULT 0,
    auto_disabled_at     TIMESTAMPTZ,
    CONSTRAINT outbound_webhook_url_https CHECK (url LIKE 'https://%'),
    CONSTRAINT outbound_webhook_name_nonempty CHECK (length(name) > 0)
);

CREATE INDEX idx_outbound_webhook_subs_project
    ON outbound_webhook_subscriptions(project_id)
    WHERE deleted_at IS NULL;

CREATE INDEX idx_outbound_webhook_subs_active
    ON outbound_webhook_subscriptions(active, project_id)
    WHERE deleted_at IS NULL;

-- Append-only delivery log. One row per attempt (so retries create
-- additional rows linked by the same (subscription_id, event_id) tuple
-- via attempt_number).
CREATE TABLE outbound_webhook_deliveries (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id      UUID NOT NULL REFERENCES outbound_webhook_subscriptions(id) ON DELETE CASCADE,
    -- Link back to the originating lifecycle event when applicable
    lifecycle_event_id   UUID REFERENCES deployment_lifecycle_events(id),
    -- Envelope identity — this is the evt_* id in the payload JSON
    event_id             TEXT NOT NULL,
    event_type           TEXT NOT NULL,
    payload              JSONB NOT NULL,
    payload_sha256       TEXT NOT NULL,
    attempt_number       INT NOT NULL DEFAULT 1,
    -- pending / delivered / failed / dlq
    status               TEXT NOT NULL DEFAULT 'pending',
    http_status          INT,
    response_snippet     TEXT,  -- First 500 bytes of response body
    error_message        TEXT,
    attempted_at         TIMESTAMPTZ,
    delivered_at         TIMESTAMPTZ,
    duration_ms          INT,
    -- Retry scheduling
    next_retry_at        TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT outbound_webhook_deliveries_status_chk
        CHECK (status IN ('pending', 'delivering', 'delivered', 'failed', 'dlq'))
);

CREATE INDEX idx_outbound_webhook_deliveries_subscription
    ON outbound_webhook_deliveries(subscription_id, created_at DESC);

CREATE INDEX idx_outbound_webhook_deliveries_pending
    ON outbound_webhook_deliveries(next_retry_at)
    WHERE status IN ('pending', 'failed');

CREATE INDEX idx_outbound_webhook_deliveries_event
    ON outbound_webhook_deliveries(event_id);
