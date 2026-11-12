DO $$ BEGIN
 LOCK TABLE janua_staging_migration_operations,janua_staging_migration_objects,janua_staging_migration_outcomes IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM janua_staging_migration_operations) OR EXISTS(SELECT 1 FROM janua_staging_migration_objects) OR EXISTS(SELECT 1 FROM janua_staging_migration_outcomes)
 THEN RAISE EXCEPTION 'retained initial migration history forbids rollback'; END IF;
 DROP TABLE janua_staging_migration_outcomes;
 DROP TABLE janua_staging_migration_objects;
 DROP TABLE janua_staging_migration_operations;
 DROP FUNCTION validate_janua_staging_migration_operation();
 DROP FUNCTION validate_janua_staging_migration_object();
 DROP FUNCTION validate_janua_staging_migration_outcome();
 DROP FUNCTION refuse_janua_staging_migration_rewrite();
END; $$;
