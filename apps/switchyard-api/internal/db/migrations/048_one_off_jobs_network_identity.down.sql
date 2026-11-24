ALTER TABLE one_off_jobs DROP CONSTRAINT IF EXISTS one_off_jobs_network_identity_check;
ALTER TABLE one_off_jobs DROP COLUMN IF EXISTS network_identity;
