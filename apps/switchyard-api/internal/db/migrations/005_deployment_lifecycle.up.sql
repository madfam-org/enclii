-- 005_deployment_lifecycle: Deployment lifecycle event timeline + onboarding registry
-- Provides a queryable event log linking push → build → deploy → health
-- across all repos (enclii, janua, dhanam, and future ecosystem repos).

CREATE TABLE deployment_lifecycle_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Link to existing records (all optional — populated as pipeline progresses)
    deployment_id   UUID REFERENCES deployments(id),
    release_id      UUID REFERENCES releases(id),
    ci_run_id       UUID REFERENCES ci_runs(id),
    -- Identity
    project_id      UUID REFERENCES projects(id),
    service_id      UUID REFERENCES services(id),
    repo_full_name  TEXT NOT NULL,
    commit_sha      TEXT NOT NULL,
    branch          TEXT NOT NULL,
    ref             TEXT NOT NULL,
    -- Target environment (derived from branch or explicit)
    target_env      TEXT,
    -- Event
    event_type      TEXT NOT NULL,
    source          TEXT NOT NULL,
    message         TEXT,
    metadata        JSONB DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_lifecycle_repo_time ON deployment_lifecycle_events(repo_full_name, created_at DESC);
CREATE INDEX idx_lifecycle_commit ON deployment_lifecycle_events(commit_sha);
CREATE INDEX idx_lifecycle_branch ON deployment_lifecycle_events(repo_full_name, branch, created_at DESC);
CREATE INDEX idx_lifecycle_project ON deployment_lifecycle_events(project_id, created_at DESC);
CREATE INDEX idx_lifecycle_deployment ON deployment_lifecycle_events(deployment_id) WHERE deployment_id IS NOT NULL;
CREATE INDEX idx_lifecycle_env ON deployment_lifecycle_events(target_env, created_at DESC) WHERE target_env IS NOT NULL;

-- Onboarding registry for self-service repo registration
CREATE TABLE onboarding_registrations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID NOT NULL REFERENCES projects(id),
    repo_full_name  TEXT NOT NULL UNIQUE,
    webhook_id      BIGINT,
    webhook_secret  TEXT,
    argocd_app_name TEXT,
    onboard_status  TEXT NOT NULL DEFAULT 'pending',
    config_snapshot JSONB,
    error_message   TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
