-- Operator holds are namespace-specific and survive workload recreation.
-- No automatic expiry or cascading deletion: release is a separate reviewed operation.
CREATE TABLE IF NOT EXISTS service_job_holds (
    namespace text NOT NULL,
    cronjob_name text NOT NULL,
    project_id uuid NOT NULL REFERENCES projects(id),
    service_id uuid NOT NULL REFERENCES services(id),
    environment_id uuid NOT NULL REFERENCES environments(id),
    service_name text NOT NULL,
    reason text NOT NULL CHECK (length(trim(reason)) > 0),
    actor_id text NOT NULL CHECK (length(trim(actor_id)) > 0),
    reviewed_uid text NOT NULL,
    reviewed_resource_version text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (namespace, cronjob_name)
);
