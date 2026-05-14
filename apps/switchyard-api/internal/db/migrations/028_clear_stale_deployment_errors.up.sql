-- Clear stale deployment error text from records that are currently healthy.
-- A previous UpdateStatus path could move a deployment back to running/healthy
-- without clearing an earlier timeout message, producing contradictory
-- production state such as status=running with error_message='Deployment timed out...'.

UPDATE public.deployments
SET error_message = NULL,
    updated_at = NOW()
WHERE status = 'running'
  AND health = 'healthy'
  AND error_message IS NOT NULL;
