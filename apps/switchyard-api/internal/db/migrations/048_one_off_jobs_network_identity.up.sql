-- Record whether a one-off job asked to run with its service's network identity.
--
-- One-off Job pods carried only the reconciler's bookkeeping labels, so a
-- namespace whose NetworkPolicies deny egress by default and admit database
-- egress by pod label (for example `app: <service>`) blocked the job from its
-- own service's database. Operator scripts that need that database had to run
-- break-glass with `kubectl exec` instead of through the audited run-once path.
--
-- '' (the default, and every existing row) keeps today's behaviour: the pod
-- gets no service labels. 'service' asks the reconciler to copy onto the pod
-- only the labels the namespace's NetworkPolicies select on, and only when the
-- job runs the service's deployed image (see timetable_network_identity.go).
ALTER TABLE one_off_jobs
    ADD COLUMN IF NOT EXISTS network_identity TEXT NOT NULL DEFAULT '';

ALTER TABLE one_off_jobs
    DROP CONSTRAINT IF EXISTS one_off_jobs_network_identity_check;

ALTER TABLE one_off_jobs
    ADD CONSTRAINT one_off_jobs_network_identity_check
    CHECK (network_identity IN ('', 'service'));

COMMENT ON COLUMN one_off_jobs.network_identity IS
    'Network identity requested for the job pod: empty = none (default), service = the service''s NetworkPolicy-selected labels, only for the service''s deployed image.';
