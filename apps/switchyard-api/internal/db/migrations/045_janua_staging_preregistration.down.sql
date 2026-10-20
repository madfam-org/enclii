DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM janua_staging_preregistration_intents) THEN
  RAISE EXCEPTION 'cannot remove held preregistration history';
 END IF;
END; $$;
DROP TABLE janua_staging_preregistration_intents;
DROP FUNCTION validate_janua_staging_preregistration();
DROP FUNCTION refuse_janua_staging_preregistration_rewrite();
