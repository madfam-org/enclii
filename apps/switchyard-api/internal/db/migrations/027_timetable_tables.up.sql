-- 027_timetable_tables
--
-- Repairs the Timetable persistence layer. Migration 020 added the legacy
-- services.jobs JSONB column, but the shipped repository/API code uses the
-- normalized cron_jobs, cron_job_runs, and one_off_jobs tables. Production
-- `enclii jobs list` and `enclii jobs run-once` return 500 when these tables
-- are missing. Keep this migration idempotent so dirty-recovery and partially
-- provisioned databases can safely re-run it.

CREATE TABLE IF NOT EXISTS public.cron_jobs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL REFERENCES public.projects(id) ON DELETE CASCADE,
    service_id uuid NOT NULL REFERENCES public.services(id) ON DELETE CASCADE,
    name character varying(255) NOT NULL,
    schedule character varying(100) NOT NULL,
    command text NOT NULL,
    image character varying(255),
    timeout integer DEFAULT 300 NOT NULL,
    retries integer DEFAULT 0 NOT NULL,
    suspended boolean DEFAULT false NOT NULL,
    concurrency character varying(20) DEFAULT 'forbid'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    last_run_at timestamp with time zone,
    next_run_at timestamp with time zone,
    CONSTRAINT cron_jobs_pkey PRIMARY KEY (id),
    CONSTRAINT cron_jobs_timeout_positive CHECK (timeout > 0),
    CONSTRAINT cron_jobs_retries_nonnegative CHECK (retries >= 0),
    CONSTRAINT cron_jobs_concurrency_check CHECK (
        (concurrency)::text = ANY (
            (ARRAY[
                'allow'::character varying,
                'forbid'::character varying,
                'replace'::character varying
            ])::text[]
        )
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_cron_jobs_project_name
    ON public.cron_jobs USING btree (project_id, name);

CREATE INDEX IF NOT EXISTS idx_cron_jobs_project_id
    ON public.cron_jobs USING btree (project_id);

CREATE INDEX IF NOT EXISTS idx_cron_jobs_service_id
    ON public.cron_jobs USING btree (service_id);

CREATE INDEX IF NOT EXISTS idx_cron_jobs_active_next_run
    ON public.cron_jobs USING btree (suspended, next_run_at);

CREATE TABLE IF NOT EXISTS public.cron_job_runs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    cron_job_id uuid NOT NULL REFERENCES public.cron_jobs(id) ON DELETE CASCADE,
    status character varying(50) DEFAULT 'running'::character varying NOT NULL,
    exit_code integer,
    started_at timestamp with time zone DEFAULT now() NOT NULL,
    ended_at timestamp with time zone,
    log_output text,
    CONSTRAINT cron_job_runs_pkey PRIMARY KEY (id),
    CONSTRAINT cron_job_runs_status_check CHECK (
        (status)::text = ANY (
            (ARRAY[
                'running'::character varying,
                'completed'::character varying,
                'failed'::character varying
            ])::text[]
        )
    )
);

CREATE INDEX IF NOT EXISTS idx_cron_job_runs_cron_job_id
    ON public.cron_job_runs USING btree (cron_job_id);

CREATE INDEX IF NOT EXISTS idx_cron_job_runs_started_at
    ON public.cron_job_runs USING btree (started_at DESC);

CREATE TABLE IF NOT EXISTS public.one_off_jobs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL REFERENCES public.projects(id) ON DELETE CASCADE,
    service_id uuid NOT NULL REFERENCES public.services(id) ON DELETE CASCADE,
    name character varying(255) NOT NULL,
    command text NOT NULL,
    image character varying(255),
    timeout integer DEFAULT 300 NOT NULL,
    run_at timestamp with time zone,
    status character varying(50) DEFAULT 'pending'::character varying NOT NULL,
    exit_code integer,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    started_at timestamp with time zone,
    ended_at timestamp with time zone,
    CONSTRAINT one_off_jobs_pkey PRIMARY KEY (id),
    CONSTRAINT one_off_jobs_timeout_positive CHECK (timeout > 0),
    CONSTRAINT one_off_jobs_status_check CHECK (
        (status)::text = ANY (
            (ARRAY[
                'pending'::character varying,
                'running'::character varying,
                'completed'::character varying,
                'failed'::character varying
            ])::text[]
        )
    )
);

CREATE INDEX IF NOT EXISTS idx_one_off_jobs_project_id
    ON public.one_off_jobs USING btree (project_id);

CREATE INDEX IF NOT EXISTS idx_one_off_jobs_service_id
    ON public.one_off_jobs USING btree (service_id);

CREATE INDEX IF NOT EXISTS idx_one_off_jobs_pending_run_at
    ON public.one_off_jobs USING btree (status, run_at);
