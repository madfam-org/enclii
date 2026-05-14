-- Roll back normalized Timetable tables.
-- Drop dependents before parents to avoid FK failures.

DROP TABLE IF EXISTS public.cron_job_runs;
DROP TABLE IF EXISTS public.one_off_jobs;
DROP TABLE IF EXISTS public.cron_jobs;
