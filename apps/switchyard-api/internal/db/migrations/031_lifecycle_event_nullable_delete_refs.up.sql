-- Allow cleanup of services/releases while preserving lifecycle audit rows.
-- These references are optional in the application model, so deletion should
-- detach the event from the removed resource rather than block cleanup.

ALTER TABLE deployment_lifecycle_events
  DROP CONSTRAINT IF EXISTS deployment_lifecycle_events_deployment_id_fkey,
  ADD CONSTRAINT deployment_lifecycle_events_deployment_id_fkey
    FOREIGN KEY (deployment_id) REFERENCES deployments(id) ON DELETE SET NULL;

ALTER TABLE deployment_lifecycle_events
  DROP CONSTRAINT IF EXISTS deployment_lifecycle_events_release_id_fkey,
  ADD CONSTRAINT deployment_lifecycle_events_release_id_fkey
    FOREIGN KEY (release_id) REFERENCES releases(id) ON DELETE SET NULL;

ALTER TABLE deployment_lifecycle_events
  DROP CONSTRAINT IF EXISTS deployment_lifecycle_events_ci_run_id_fkey,
  ADD CONSTRAINT deployment_lifecycle_events_ci_run_id_fkey
    FOREIGN KEY (ci_run_id) REFERENCES ci_runs(id) ON DELETE SET NULL;

ALTER TABLE deployment_lifecycle_events
  DROP CONSTRAINT IF EXISTS deployment_lifecycle_events_project_id_fkey,
  ADD CONSTRAINT deployment_lifecycle_events_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE SET NULL;

ALTER TABLE deployment_lifecycle_events
  DROP CONSTRAINT IF EXISTS deployment_lifecycle_events_service_id_fkey,
  ADD CONSTRAINT deployment_lifecycle_events_service_id_fkey
    FOREIGN KEY (service_id) REFERENCES services(id) ON DELETE SET NULL;
