-- Persist rollout-blocked root cause from K8s rollout evaluation (GA stability).
ALTER TABLE services
    ADD COLUMN IF NOT EXISTS rollout_blocked_reason TEXT NOT NULL DEFAULT '';

COMMENT ON COLUMN services.rollout_blocked_reason IS
    'When health is unhealthy due to a blocked rollout, the k8s.RolloutBlockedReason value (e.g. image_pull_back_off). Cleared when rollout is OK.';
