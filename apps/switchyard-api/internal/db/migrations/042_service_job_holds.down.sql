-- Refuse rollback while containment is active. Removing holds could restart jobs.
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM service_job_holds) THEN
    RAISE EXCEPTION 'cannot remove service_job_holds while operational holds exist';
  END IF;
END $$;
DROP TABLE service_job_holds;
