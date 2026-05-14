-- Mark stale building releases as failed.
--
-- Build records should receive a success/failure callback from the build
-- subsystem. If no callback arrives within the production build timeout window,
-- keeping the release in "building" makes Enclii's release history untruthful
-- and can cause operators to wait on work that is no longer active.

UPDATE public.releases
SET status = 'failed',
    error_message = 'Build timed out (no callback received within 30 minutes)',
    updated_at = NOW()
WHERE status = 'building'
  AND created_at < NOW() - INTERVAL '30 minutes';

-- Also clear stale error text from ready releases. A ready release is terminal
-- success; retaining historical error text on it is contradictory.
UPDATE public.releases
SET error_message = NULL,
    updated_at = NOW()
WHERE status = 'ready'
  AND error_message IS NOT NULL;
