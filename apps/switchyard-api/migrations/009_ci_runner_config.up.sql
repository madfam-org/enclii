-- Add CI runner mode to projects
-- Controls whether GitHub Actions workflows use GitHub-hosted or self-hosted (ARC) runners
ALTER TABLE projects
  ADD COLUMN ci_runner_mode VARCHAR(20) NOT NULL DEFAULT 'github'
    CHECK (ci_runner_mode IN ('github', 'self-hosted'));
