-- 010_canary_rollouts: Canary release state machine for P2.7
--
-- Tracks the lifecycle of a canary rollout from start → validating → promoting
-- → terminal (succeeded | auto_rolled_back | manual_rolled_back). Each row is
-- an append-only record of a single rollout attempt. The running rollout for
-- a service is whichever has state NOT IN the terminal set.
--
-- The two-Deployment pattern is encoded in the stable/canary deployment refs:
--   - stable_deployment_id  — enclii deployment powering the CURRENT stable RS
--   - canary_deployment_id  — enclii deployment built from the candidate digest
--   - new_stable_deployment_id — populated when auto-promote builds the new stable
--
-- Replica math is stored for reproducibility of the flip (see docstring on
-- computeCanarySplit in reconciler/canary.go).

CREATE TABLE canary_rollouts (
    id                         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_id                 UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    environment_id             UUID NOT NULL REFERENCES environments(id),

    -- Source references
    stable_deployment_id       UUID NOT NULL REFERENCES deployments(id),
    canary_deployment_id       UUID NOT NULL REFERENCES deployments(id),
    new_stable_deployment_id   UUID REFERENCES deployments(id),  -- Set on promote

    -- Rollout spec (frozen at start time)
    canary_digest              TEXT NOT NULL,        -- image digest (informational — canary_deployment_id is the authority)
    canary_percentage          INT  NOT NULL CHECK (canary_percentage BETWEEN 5 AND 50),
    total_replicas             INT  NOT NULL CHECK (total_replicas >= 2),
    canary_replicas            INT  NOT NULL CHECK (canary_replicas >= 1),
    stable_replicas            INT  NOT NULL CHECK (stable_replicas >= 1),
    validation_window_seconds  INT  NOT NULL DEFAULT 600 CHECK (validation_window_seconds BETWEEN 60 AND 3600),
    smoke_endpoint             TEXT,                 -- optional user-supplied http URL on the canary
    error_rate_threshold       DOUBLE PRECISION NOT NULL DEFAULT 0.05,

    -- State machine
    state                      TEXT NOT NULL DEFAULT 'pending'
                                 CHECK (state IN (
                                   'pending', 'running', 'validating',
                                   'promoting', 'succeeded',
                                   'auto_rolled_back', 'manual_rolled_back', 'failed'
                                 )),

    -- Timestamps for each transition (null until entered)
    started_at                 TIMESTAMPTZ,
    validating_started_at      TIMESTAMPTZ,
    promoting_started_at       TIMESTAMPTZ,
    terminal_at                TIMESTAMPTZ,

    -- Audit + diagnostics
    initiated_by               UUID REFERENCES users(id),
    change_ticket_url          TEXT,
    last_error                 TEXT,
    rollback_reason            TEXT,

    created_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_canary_service_created ON canary_rollouts(service_id, created_at DESC);
CREATE INDEX idx_canary_env ON canary_rollouts(environment_id, created_at DESC);
CREATE INDEX idx_canary_state ON canary_rollouts(state)
    WHERE state IN ('pending', 'running', 'validating', 'promoting');

-- Only one non-terminal rollout per service at a time (enforced at app layer +
-- partial unique index). Prevents two overlapping canaries from fighting over
-- the same Deployment/Service pair.
CREATE UNIQUE INDEX idx_canary_one_active_per_service
    ON canary_rollouts(service_id)
    WHERE state IN ('pending', 'running', 'validating', 'promoting');

CREATE TRIGGER set_canary_rollouts_updated_at
    BEFORE UPDATE ON canary_rollouts
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_updated_at();
