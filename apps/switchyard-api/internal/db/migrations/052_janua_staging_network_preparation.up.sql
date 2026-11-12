-- Fixed network preparation only. No active Service/addon/custody registration.
CREATE TABLE janua_staging_network_operations (
 id uuid PRIMARY KEY,
 authority_id uuid NOT NULL UNIQUE REFERENCES janua_staging_inert_authorities(id) ON DELETE RESTRICT,
 actor_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 profile text NOT NULL CHECK(profile='janua-staging-network-preparation-v1'),
 plan_sha256 text NOT NULL CHECK(plan_sha256 ~ '^[a-f0-9]{64}$'),
 reason text NOT NULL CHECK(length(trim(reason)) BETWEEN 1 AND 512),
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK((payload->>'operation_id') IS NOT DISTINCT FROM id::text),
 CHECK((payload->>'actor_id') IS NOT DISTINCT FROM actor_id::text),
 CHECK((payload->'authority'->>'id') IS NOT DISTINCT FROM authority_id::text),
 CHECK((payload->>'plan_sha256') IS NOT DISTINCT FROM plan_sha256),
 CHECK(COALESCE(jsonb_typeof(payload->'object_sha256')='array' AND jsonb_array_length(payload->'object_sha256')=7,false))
);
CREATE TABLE janua_staging_network_attempts (
 operation_id uuid NOT NULL REFERENCES janua_staging_network_operations(id) ON DELETE RESTRICT,
 ordinal integer NOT NULL CHECK(ordinal BETWEEN 0 AND 6),
 object_key text NOT NULL,
 desired_sha256 text NOT NULL CHECK(desired_sha256 ~ '^[a-f0-9]{64}$'),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(operation_id,ordinal), UNIQUE(operation_id,object_key),
 UNIQUE(operation_id,ordinal,object_key)
);
CREATE TABLE janua_staging_network_receipts (
 operation_id uuid NOT NULL,
 ordinal integer NOT NULL,
 object_key text NOT NULL,
 uid text NOT NULL UNIQUE CHECK(length(uid)>0),
 resource_version text NOT NULL CHECK(length(resource_version)>0),
 observed_sha256 text NOT NULL CHECK(observed_sha256 ~ '^[a-f0-9]{64}$'),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(operation_id,ordinal),
 FOREIGN KEY(operation_id,ordinal,object_key) REFERENCES janua_staging_network_attempts(operation_id,ordinal,object_key) ON DELETE RESTRICT
);
CREATE TABLE janua_staging_network_completions (
 operation_id uuid PRIMARY KEY REFERENCES janua_staging_network_operations(id) ON DELETE RESTRICT,
 plan_sha256 text NOT NULL CHECK(plan_sha256 ~ '^[a-f0-9]{64}$'),
 created_at timestamptz NOT NULL DEFAULT now()
);

CREATE FUNCTION validate_janua_network_operation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM janua_staging_inert_authorities a
  JOIN projects p ON p.id=a.project_id AND p.slug='janua'
  JOIN users u ON u.id=NEW.actor_id AND u.active AND u.is_platform_admin
  JOIN environments e ON e.id=a.environment_id AND e.project_id=p.id
  JOIN onboarding_registrations r ON r.id=a.repository_registration_id AND r.project_id=p.id
  WHERE a.id=NEW.authority_id AND e.name='staging' AND e.kube_namespace='janua-staging'
  AND r.repo_full_name='madfam-org/janua' AND r.onboard_status='completed'
  AND (a.payload-'observed_at')=((NEW.payload->'authority')-'observed_at')
  AND NOT EXISTS(SELECT 1 FROM services s WHERE s.id=a.held_service_id)
  AND NOT EXISTS(SELECT 1 FROM database_addons d WHERE d.deleted_at IS NULL
   AND (d.environment_id=e.id OR d.k8s_namespace='project-'||left(p.id::text,8))))
 THEN RAISE EXCEPTION 'current held Janua network authority required'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER janua_network_operation_identity BEFORE INSERT ON janua_staging_network_operations
 FOR EACH ROW EXECUTE FUNCTION validate_janua_network_operation();

CREATE FUNCTION validate_janua_network_attempt() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE o janua_staging_network_operations; n text; expected text;
BEGIN
 SELECT * INTO o FROM janua_staging_network_operations WHERE id=NEW.operation_id FOR UPDATE;
 n:='project-'||left(o.payload->'authority'->>'project_id',8);
 expected:=(ARRAY['Namespace:/'||n,'NetworkPolicy:'||n||'/default-deny',
  'NetworkPolicy:'||n||'/janua-staging-db-isolation','NetworkPolicy:janua-staging/janua-staging-api-database',
  'NetworkPolicy:'||n||'/janua-staging-migrator-database','NetworkPolicy:janua-staging/janua-staging-redis-isolation',
  'NetworkPolicy:janua-staging/janua-staging-api-redis'])[NEW.ordinal+1];
 IF expected IS DISTINCT FROM NEW.object_key OR NEW.desired_sha256 IS DISTINCT FROM (o.payload->'object_sha256'->>NEW.ordinal)
  OR (SELECT count(*) FROM janua_staging_network_attempts WHERE operation_id=o.id)<>NEW.ordinal
  OR (SELECT count(*) FROM janua_staging_network_receipts r JOIN janua_staging_network_attempts a
   USING(operation_id,ordinal,object_key) WHERE r.operation_id=o.id AND r.observed_sha256=a.desired_sha256)<>NEW.ordinal
  OR EXISTS(SELECT 1 FROM janua_staging_network_completions WHERE operation_id=o.id)
 THEN RAISE EXCEPTION 'ordered fixed native create intent required'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER janua_network_attempt_identity BEFORE INSERT ON janua_staging_network_attempts
 FOR EACH ROW EXECUTE FUNCTION validate_janua_network_attempt();

CREATE FUNCTION validate_janua_network_completion() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM 1 FROM janua_staging_network_operations WHERE id=NEW.operation_id FOR UPDATE;
 IF NOT EXISTS(SELECT 1 FROM janua_staging_network_operations WHERE id=NEW.operation_id AND plan_sha256=NEW.plan_sha256)
  OR (SELECT count(*) FROM janua_staging_network_receipts r JOIN janua_staging_network_attempts a
   USING(operation_id,ordinal,object_key) WHERE r.operation_id=NEW.operation_id AND r.observed_sha256=a.desired_sha256)<>7
 THEN RAISE EXCEPTION 'all seven exact native configuration receipts required'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER janua_network_completion_identity BEFORE INSERT ON janua_staging_network_completions
 FOR EACH ROW EXECUTE FUNCTION validate_janua_network_completion();

DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['janua_staging_network_operations','janua_staging_network_attempts',
  'janua_staging_network_receipts','janua_staging_network_completions'] LOOP
  EXECUTE format('CREATE TRIGGER immutable_history BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION refuse_janua_inert_rewrite()',t);
  EXECUTE format('CREATE TRIGGER immutable_no_truncate BEFORE TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION refuse_janua_inert_rewrite()',t);
 END LOOP;
END $$;
