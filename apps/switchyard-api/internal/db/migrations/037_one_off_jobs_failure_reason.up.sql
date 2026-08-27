-- Persist why a one-off job never ran.
--
-- Admission denials (Kyverno webhook rejecting the Job create) previously left
-- the row 'pending' forever: the reconciler retried every 30s, logged, and the
-- operator saw only "job has not started yet: no pods scheduled". The
-- reconciler now marks such jobs 'failed' and records the denial here so the
-- one-off job logs endpoint can surface it when no pod ever existed.
ALTER TABLE one_off_jobs
    ADD COLUMN IF NOT EXISTS failure_reason TEXT NOT NULL DEFAULT '';

COMMENT ON COLUMN one_off_jobs.failure_reason IS
    'Why the job failed before producing a pod (e.g. a Kubernetes admission webhook denial). Empty when the job ran, or when it failed inside the container (see exit_code).';
