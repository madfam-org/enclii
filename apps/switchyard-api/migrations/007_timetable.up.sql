-- Timetable: Cron jobs and one-off scheduled jobs for user services.

CREATE TABLE cron_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    service_id UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    schedule VARCHAR(255) NOT NULL,
    command TEXT NOT NULL,
    image VARCHAR(512),
    timeout INT NOT NULL DEFAULT 300,
    retries INT NOT NULL DEFAULT 0,
    suspended BOOLEAN NOT NULL DEFAULT FALSE,
    concurrency VARCHAR(20) NOT NULL DEFAULT 'forbid',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_run_at TIMESTAMPTZ,
    next_run_at TIMESTAMPTZ,
    UNIQUE(project_id, name)
);

CREATE TABLE cron_job_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    cron_job_id UUID NOT NULL REFERENCES cron_jobs(id) ON DELETE CASCADE,
    status VARCHAR(20) NOT NULL DEFAULT 'running',
    exit_code INT,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at TIMESTAMPTZ,
    log_output TEXT
);

CREATE TABLE one_off_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    service_id UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    command TEXT NOT NULL,
    image VARCHAR(512),
    timeout INT NOT NULL DEFAULT 300,
    run_at TIMESTAMPTZ,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    exit_code INT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    ended_at TIMESTAMPTZ
);

CREATE INDEX idx_cron_jobs_project ON cron_jobs(project_id);
CREATE INDEX idx_cron_jobs_service ON cron_jobs(service_id);
CREATE INDEX idx_cron_job_runs_job ON cron_job_runs(cron_job_id);
CREATE INDEX idx_one_off_jobs_project ON one_off_jobs(project_id);
CREATE INDEX idx_one_off_jobs_status ON one_off_jobs(status);
