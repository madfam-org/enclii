ALTER TABLE deployment_lifecycle_events
  DROP CONSTRAINT IF EXISTS deployment_lifecycle_events_service_id_fkey,
  ADD CONSTRAINT deployment_lifecycle_events_service_id_fkey
    FOREIGN KEY (service_id) REFERENCES services(id);

ALTER TABLE deployment_lifecycle_events
  DROP CONSTRAINT IF EXISTS deployment_lifecycle_events_project_id_fkey,
  ADD CONSTRAINT deployment_lifecycle_events_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(id);

ALTER TABLE deployment_lifecycle_events
  DROP CONSTRAINT IF EXISTS deployment_lifecycle_events_ci_run_id_fkey,
  ADD CONSTRAINT deployment_lifecycle_events_ci_run_id_fkey
    FOREIGN KEY (ci_run_id) REFERENCES ci_runs(id);

ALTER TABLE deployment_lifecycle_events
  DROP CONSTRAINT IF EXISTS deployment_lifecycle_events_release_id_fkey,
  ADD CONSTRAINT deployment_lifecycle_events_release_id_fkey
    FOREIGN KEY (release_id) REFERENCES releases(id);

ALTER TABLE deployment_lifecycle_events
  DROP CONSTRAINT IF EXISTS deployment_lifecycle_events_deployment_id_fkey,
  ADD CONSTRAINT deployment_lifecycle_events_deployment_id_fkey
    FOREIGN KEY (deployment_id) REFERENCES deployments(id);
