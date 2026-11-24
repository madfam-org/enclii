-- One initial empty-only Alembic operation per retained dedicated DB operation.
-- No Job, secret, database, grant, active service or runtime is created by DDL.
CREATE TABLE janua_staging_migration_operations (
 id uuid PRIMARY KEY,
 database_operation_id uuid NOT NULL UNIQUE REFERENCES janua_staging_database_operations(id) ON DELETE RESTRICT,
 authority_id uuid NOT NULL REFERENCES janua_staging_inert_authorities(id) ON DELETE RESTRICT,
 actor_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 profile text NOT NULL CHECK(profile='janua-staging-alembic-v1'),
 plan_sha256 text NOT NULL CHECK(plan_sha256 ~ '^[a-f0-9]{64}$'),
 reason text NOT NULL CHECK(length(trim(reason)) BETWEEN 1 AND 512),
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK((payload->>'operation_id') IS NOT DISTINCT FROM id::text),
 CHECK((payload->>'plan_sha256') IS NOT DISTINCT FROM plan_sha256),
 CHECK((payload->'database'->>'operation_id') IS NOT DISTINCT FROM database_operation_id::text),
 CHECK((payload->'database'->>'actor_id') IS NOT DISTINCT FROM actor_id::text),
 CHECK((payload->'source'->>'authority_id') IS NOT DISTINCT FROM authority_id::text)
);
CREATE TABLE janua_staging_migration_objects (
 operation_id uuid NOT NULL REFERENCES janua_staging_migration_operations(id) ON DELETE RESTRICT,
 phase text NOT NULL CHECK(phase IN ('SUSPENDED_JOB_CREATED','OWNER_SECRET_CREATED','JOB_STARTED','OWNER_SECRET_REMOVED')),
 uid text NOT NULL CHECK(length(uid) BETWEEN 1 AND 128),
 object_sha256 text NOT NULL CHECK(object_sha256 ~ '^[a-f0-9]{64}$'),
 recorded_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(operation_id,phase)
);
CREATE TABLE janua_staging_migration_outcomes (
 operation_id uuid PRIMARY KEY REFERENCES janua_staging_migration_operations(id) ON DELETE RESTRICT,
 state text NOT NULL CHECK(state IN ('SUCCEEDED','FAILED','FAILED_NO_RUNTIME','ABORTED_BEFORE_START')),
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),
 recorded_at timestamptz NOT NULL DEFAULT now(),
 CHECK((payload->>'state') IS NOT DISTINCT FROM state)
);
CREATE FUNCTION validate_janua_staging_migration_operation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM janua_staging_database_operations d
  JOIN janua_staging_inert_authorities a ON a.id=d.authority_id
  JOIN users u ON u.id=NEW.actor_id AND u.active AND u.is_platform_admin
  JOIN janua_staging_database_objects s ON s.operation_id=d.id AND s.phase='RUNTIME_SECRET_CREATED'
  JOIN janua_staging_database_objects c ON c.operation_id=d.id AND c.phase='CLUSTER_CREATED'
  JOIN janua_staging_database_objects r ON r.operation_id=d.id AND r.phase='CLUSTER_SUBMITTED_RUNTIME_UNQUALIFIED'
  WHERE d.id=NEW.database_operation_id AND d.authority_id=NEW.authority_id AND d.actor_id=NEW.actor_id
   AND (NEW.payload->'database')=d.payload AND c.uid=r.uid
   AND (NEW.payload->>'cluster_uid') IS NOT DISTINCT FROM c.uid
   AND (NEW.payload->>'runtime_secret_uid') IS NOT DISTINCT FROM s.uid
   AND (NEW.payload->'source'->>'manifest_source_revision') IS NOT DISTINCT FROM (a.payload->>'source_revision')
   AND (NEW.payload->'source'->>'manifest_source_sha256') IS NOT DISTINCT FROM (a.payload->>'source_sha256')
 ) THEN RAISE EXCEPTION 'completed exact native Janua database identity required'; END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER janua_staging_migration_identity BEFORE INSERT ON janua_staging_migration_operations
 FOR EACH ROW EXECUTE FUNCTION validate_janua_staging_migration_operation();
CREATE FUNCTION validate_janua_staging_migration_object() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE prior text; plan_hash text;
BEGIN
 SELECT plan_sha256 INTO plan_hash FROM janua_staging_migration_operations WHERE id=NEW.operation_id FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'retained migration operation required'; END IF;
 IF NEW.phase IN ('SUSPENDED_JOB_CREATED','JOB_STARTED') AND NEW.object_sha256<>plan_hash THEN RAISE EXCEPTION 'exact retained migration plan hash required'; END IF;
 IF NEW.phase='SUSPENDED_JOB_CREATED' THEN
  IF EXISTS(SELECT 1 FROM janua_staging_migration_objects WHERE operation_id=NEW.operation_id)
  THEN RAISE EXCEPTION 'suspended Job must be first'; END IF;
 ELSIF NEW.phase='OWNER_SECRET_CREATED' THEN
  SELECT uid INTO prior FROM janua_staging_migration_objects WHERE operation_id=NEW.operation_id AND phase='SUSPENDED_JOB_CREATED';
  IF NOT FOUND OR prior=NEW.uid THEN RAISE EXCEPTION 'distinct exact suspended Job custody required'; END IF;
 ELSIF NEW.phase='JOB_STARTED' THEN
  SELECT uid INTO prior FROM janua_staging_migration_objects WHERE operation_id=NEW.operation_id AND phase='SUSPENDED_JOB_CREATED';
  IF NOT FOUND OR prior<>NEW.uid OR EXISTS(SELECT 1 FROM janua_staging_migration_outcomes WHERE operation_id=NEW.operation_id) OR NOT EXISTS(SELECT 1 FROM janua_staging_migration_objects WHERE operation_id=NEW.operation_id AND phase='OWNER_SECRET_CREATED')
  THEN RAISE EXCEPTION 'same retained Job and owner custody required'; END IF;
 ELSIF NEW.phase='OWNER_SECRET_REMOVED' THEN
  SELECT uid INTO prior FROM janua_staging_migration_objects WHERE operation_id=NEW.operation_id AND phase='OWNER_SECRET_CREATED' AND object_sha256=NEW.object_sha256;
  IF NOT FOUND OR prior<>NEW.uid OR NOT EXISTS(SELECT 1 FROM janua_staging_migration_outcomes WHERE operation_id=NEW.operation_id)
  THEN RAISE EXCEPTION 'exact terminal owner custody required'; END IF;
 ELSE RAISE EXCEPTION 'closed migration phase required'; END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER janua_staging_migration_object_identity BEFORE INSERT ON janua_staging_migration_objects
 FOR EACH ROW EXECUTE FUNCTION validate_janua_staging_migration_object();
CREATE FUNCTION validate_janua_staging_migration_outcome() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM id FROM janua_staging_migration_operations WHERE id=NEW.operation_id FOR UPDATE;
 IF NEW.state='ABORTED_BEFORE_START' AND EXISTS(SELECT 1 FROM janua_staging_migration_objects WHERE operation_id=NEW.operation_id AND phase='JOB_STARTED') THEN RAISE EXCEPTION 'started migration cannot abort as no-runtime'; END IF;
 IF NOT EXISTS(SELECT 1 FROM janua_staging_migration_objects j JOIN janua_staging_migration_objects s ON s.operation_id=j.operation_id
  WHERE j.operation_id=NEW.operation_id AND j.phase='SUSPENDED_JOB_CREATED' AND s.phase='OWNER_SECRET_CREATED'
   AND (NEW.payload->>'job_uid') IS NOT DISTINCT FROM j.uid AND (NEW.payload->>'owner_secret_uid') IS NOT DISTINCT FROM s.uid)
 THEN RAISE EXCEPTION 'exact retained migration native outcome required'; END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER janua_staging_migration_outcome_identity BEFORE INSERT ON janua_staging_migration_outcomes
 FOR EACH ROW EXECUTE FUNCTION validate_janua_staging_migration_outcome();
CREATE FUNCTION refuse_janua_staging_migration_rewrite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'initial Janua migration history is immutable'; END; $$;
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['janua_staging_migration_operations','janua_staging_migration_objects','janua_staging_migration_outcomes'] LOOP
  EXECUTE format('CREATE TRIGGER immutable_history BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION refuse_janua_staging_migration_rewrite()',t);
  EXECUTE format('CREATE TRIGGER immutable_no_truncate BEFORE TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION refuse_janua_staging_migration_rewrite()',t);
 END LOOP;
END; $$;
