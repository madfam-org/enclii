DROP TRIGGER IF EXISTS set_canary_rollouts_updated_at ON canary_rollouts;
DROP INDEX IF EXISTS idx_canary_one_active_per_service;
DROP INDEX IF EXISTS idx_canary_state;
DROP INDEX IF EXISTS idx_canary_env;
DROP INDEX IF EXISTS idx_canary_service_created;
DROP TABLE IF EXISTS canary_rollouts;
