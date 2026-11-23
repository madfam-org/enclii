-- Refuse rollback once ANY operation history exists. The lock, emptiness
-- test and drops share one atomic statement, including a rejected rollback.
DO $$ BEGIN
 LOCK TABLE janua_staging_database_operations,janua_staging_database_objects IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM janua_staging_database_operations) OR EXISTS(SELECT 1 FROM janua_staging_database_objects)
 THEN RAISE EXCEPTION 'retained staging database operations forbid rollback'; END IF;
 DROP TABLE janua_staging_database_objects;
 DROP TABLE janua_staging_database_operations;
 DROP FUNCTION validate_janua_staging_database_operation();
 DROP FUNCTION validate_janua_staging_database_object();
 DROP FUNCTION refuse_janua_staging_database_rewrite();
END; $$;
