DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM janua_staging_network_operations) THEN
  RAISE EXCEPTION 'network preparation custody requires reviewed retirement';
 END IF;
END $$;
DROP TABLE janua_staging_network_completions,janua_staging_network_receipts,janua_staging_network_attempts,janua_staging_network_operations;
DROP FUNCTION validate_janua_network_completion(),validate_janua_network_attempt(),validate_janua_network_operation();
