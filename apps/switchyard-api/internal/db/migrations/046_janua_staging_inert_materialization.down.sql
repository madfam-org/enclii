DO $$ BEGIN
 LOCK TABLE janua_staging_inert_operations, janua_staging_inert_objects, janua_staging_inert_authorities IN ACCESS EXCLUSIVE MODE;
 IF EXISTS (SELECT 1 FROM janua_staging_inert_operations) THEN
  RAISE EXCEPTION 'inert materialization history requires reviewed retirement; rollback refused';
 END IF;
DROP TABLE janua_staging_inert_authorities;
DROP TABLE janua_staging_inert_objects;
DROP TABLE janua_staging_inert_operations;
DROP FUNCTION validate_janua_inert_operation();
DROP FUNCTION validate_janua_inert_authority();
DROP FUNCTION refuse_janua_inert_rewrite();
END $$;
