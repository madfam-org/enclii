-- Closed native DB operation journal; requires046 inert native authority.
-- No service/addon registration, database, credential or grant is created here.
CREATE TABLE janua_staging_database_operations (
 id uuid PRIMARY KEY,
 authority_id uuid NOT NULL REFERENCES janua_staging_inert_authorities(id) ON DELETE RESTRICT,
 held_service_id uuid NOT NULL UNIQUE,
 actor_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 profile text NOT NULL CHECK(profile='janua-staging-database-v1'),
 plan_sha256 text NOT NULL CHECK(plan_sha256 ~ '^[a-f0-9]{64}$'),
 reason text NOT NULL CHECK(length(trim(reason)) BETWEEN 1 AND 512),
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(authority_id),
 CHECK((payload->>'operation_id') IS NOT DISTINCT FROM id::text),
 CHECK((payload->>'actor_id') IS NOT DISTINCT FROM actor_id::text),
 CHECK((payload->>'plan_sha256') IS NOT DISTINCT FROM plan_sha256),
 CHECK((payload->'authority'->>'id') IS NOT DISTINCT FROM authority_id::text),
 CHECK((payload->'authority'->>'held_service_id') IS NOT DISTINCT FROM held_service_id::text),
 CHECK((payload->'authority'->'activation_held') IS NOT DISTINCT FROM 'true'::jsonb),
 CHECK((payload->'authority'->'active_service_registered') IS NOT DISTINCT FROM 'false'::jsonb),
 CHECK((payload->'authority'->'native_043_binding_proven') IS NOT DISTINCT FROM 'false'::jsonb)
);
CREATE TABLE janua_staging_database_objects (
 operation_id uuid NOT NULL REFERENCES janua_staging_database_operations(id) ON DELETE RESTRICT,
 phase text NOT NULL CHECK(phase IN ('RUNTIME_SECRET_CREATED','CLUSTER_CREATED','CLUSTER_SUBMITTED_RUNTIME_UNQUALIFIED')),
 uid text NOT NULL CHECK(length(uid) BETWEEN 1 AND 128),
 recorded_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(operation_id,phase)
);
CREATE FUNCTION validate_janua_staging_database_operation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS (
  SELECT 1 FROM janua_staging_inert_authorities a
  JOIN users u ON u.id=NEW.actor_id AND u.active AND u.is_platform_admin
  JOIN projects p ON p.id=a.project_id AND p.slug='janua'
  JOIN environments e ON e.id=a.environment_id AND e.project_id=p.id
  WHERE a.id=NEW.authority_id AND e.name='staging' AND e.kube_namespace='janua-staging'
   AND (a.payload->>'held_service_id') IS NOT DISTINCT FROM NEW.held_service_id::text
   AND (a.payload->'activation_held') IS NOT DISTINCT FROM 'true'::jsonb
   AND (a.payload->'active_service_registered') IS NOT DISTINCT FROM 'false'::jsonb
   AND (a.payload->'native_043_binding_proven') IS NOT DISTINCT FROM 'false'::jsonb
   AND ((NEW.payload->'authority') - 'observed_at') = (a.payload - 'observed_at')
 ) THEN RAISE EXCEPTION 'closed inert Janua database authority required'; END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER janua_staging_database_identity BEFORE INSERT ON janua_staging_database_operations
 FOR EACH ROW EXECUTE FUNCTION validate_janua_staging_database_operation();
CREATE FUNCTION refuse_janua_staging_database_rewrite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'Janua staging database history is immutable'; END;
$$;
CREATE TRIGGER janua_staging_database_operation_immutable BEFORE UPDATE OR DELETE ON janua_staging_database_operations
 FOR EACH ROW EXECUTE FUNCTION refuse_janua_staging_database_rewrite();
CREATE TRIGGER janua_staging_database_operation_no_truncate BEFORE TRUNCATE ON janua_staging_database_operations
 FOR EACH STATEMENT EXECUTE FUNCTION refuse_janua_staging_database_rewrite();
CREATE TRIGGER janua_staging_database_object_immutable BEFORE UPDATE OR DELETE ON janua_staging_database_objects
 FOR EACH ROW EXECUTE FUNCTION refuse_janua_staging_database_rewrite();
CREATE TRIGGER janua_staging_database_object_no_truncate BEFORE TRUNCATE ON janua_staging_database_objects
 FOR EACH STATEMENT EXECUTE FUNCTION refuse_janua_staging_database_rewrite();

-- Serializing on the claimed operation makes reordered/concurrent event inserts
-- refuse; final custody can be bound only to the same retained Cluster UID.
CREATE FUNCTION validate_janua_staging_database_object() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE previous_uid text;
BEGIN
 PERFORM id FROM janua_staging_database_operations WHERE id=NEW.operation_id FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'claimed staging database operation required'; END IF;
 IF NEW.phase='RUNTIME_SECRET_CREATED' THEN
  IF EXISTS(SELECT 1 FROM janua_staging_database_objects WHERE operation_id=NEW.operation_id)
  THEN RAISE EXCEPTION 'runtime custody must be first'; END IF;
 ELSIF NEW.phase='CLUSTER_CREATED' THEN
  SELECT uid INTO previous_uid FROM janua_staging_database_objects
   WHERE operation_id=NEW.operation_id AND phase='RUNTIME_SECRET_CREATED';
  IF NOT FOUND OR previous_uid=NEW.uid THEN RAISE EXCEPTION 'distinct retained runtime custody required'; END IF;
 ELSIF NEW.phase='CLUSTER_SUBMITTED_RUNTIME_UNQUALIFIED' THEN
  SELECT uid INTO previous_uid FROM janua_staging_database_objects
   WHERE operation_id=NEW.operation_id AND phase='CLUSTER_CREATED';
  IF NOT FOUND OR previous_uid<>NEW.uid THEN RAISE EXCEPTION 'same retained Cluster UID required'; END IF;
 ELSE RAISE EXCEPTION 'closed staging database phase required';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER janua_staging_database_object_identity BEFORE INSERT ON janua_staging_database_objects
 FOR EACH ROW EXECUTE FUNCTION validate_janua_staging_database_object();
