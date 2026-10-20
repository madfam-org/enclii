-- One statement/transaction: exclude concurrent records before observing history,
-- and retain that exclusive lock through the conditional drop.
DO $$ BEGIN
 LOCK TABLE janua_staging_preregistration_intents IN ACCESS EXCLUSIVE MODE;
 IF EXISTS (SELECT 1 FROM janua_staging_preregistration_intents) THEN
  RAISE EXCEPTION 'cannot remove held preregistration history';
 END IF;
 DROP TABLE janua_staging_preregistration_intents;
 DROP FUNCTION validate_janua_staging_preregistration();
 DROP FUNCTION refuse_janua_staging_preregistration_rewrite();
END; $$;
