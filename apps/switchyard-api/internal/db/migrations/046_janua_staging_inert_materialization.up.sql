-- Inert staging ownership is outside services, deployment targets and all
-- background activation inventories. No production registration is rewritten.
CREATE TABLE janua_staging_inert_operations (
 id uuid PRIMARY KEY,
 held_intent_id uuid NOT NULL REFERENCES janua_staging_preregistration_intents(id) ON DELETE RESTRICT,
 project_id uuid NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 environment_id uuid NOT NULL REFERENCES environments(id) ON DELETE RESTRICT,
 repository_registration_id uuid NOT NULL REFERENCES onboarding_registrations(id) ON DELETE RESTRICT,
 actor_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 held_service_id uuid NOT NULL UNIQUE,
 profile text NOT NULL CHECK (profile='janua-staging-inert-v1'),
 plan_sha256 text NOT NULL CHECK (plan_sha256 ~ '^[a-f0-9]{64}$'),
 request_sha256 text NOT NULL CHECK (request_sha256 ~ '^[a-f0-9]{64}$'),
 reason text NOT NULL CHECK (length(trim(reason)) BETWEEN 1 AND 512),
 payload jsonb NOT NULL CHECK (jsonb_typeof(payload)='object'),
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(project_id,environment_id),
 CHECK ((payload->>'id') IS NOT DISTINCT FROM id::text),
 CHECK ((payload->>'project_id') IS NOT DISTINCT FROM project_id::text),
 CHECK ((payload->>'environment_id') IS NOT DISTINCT FROM environment_id::text),
 CHECK ((payload->>'repository_registration_id') IS NOT DISTINCT FROM repository_registration_id::text),
 CHECK ((payload->>'profile') IS NOT DISTINCT FROM profile),
 CHECK ((payload->>'held_intent_id') IS NOT DISTINCT FROM held_intent_id::text),
 CHECK ((payload->>'held_service_id') IS NOT DISTINCT FROM held_service_id::text),
 CHECK ((payload->>'plan_sha256') IS NOT DISTINCT FROM plan_sha256),
 CHECK ((payload->'activation_held') IS NOT DISTINCT FROM 'true'::jsonb),
 CHECK ((payload->'active_service_registered') IS NOT DISTINCT FROM 'false'::jsonb),
 CHECK ((payload->'native_043_binding_proven') IS NOT DISTINCT FROM 'false'::jsonb)
);

CREATE TABLE janua_staging_inert_objects (
 operation_id uuid NOT NULL REFERENCES janua_staging_inert_operations(id) ON DELETE RESTRICT,
 object_key text NOT NULL,
 uid text NOT NULL UNIQUE CHECK(length(uid)>0),
 object_sha256 text NOT NULL CHECK(object_sha256 ~ '^[a-f0-9]{64}$'),
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),
 PRIMARY KEY(operation_id,object_key),
 CHECK (object_key IN ('AppProject:argocd/janua-staging','Application:argocd/janua-staging',
  'Deployment:janua-staging/janua-api-staging','Deployment:janua-staging/janua-staging-redis',
  'Service:janua-staging/janua-api-staging','Service:janua-staging/janua-staging-redis'))
);

CREATE TABLE janua_staging_inert_authorities (
 id uuid PRIMARY KEY REFERENCES janua_staging_inert_operations(id) ON DELETE RESTRICT,
 held_service_id uuid NOT NULL UNIQUE,
 project_id uuid NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 environment_id uuid NOT NULL REFERENCES environments(id) ON DELETE RESTRICT,
 repository_registration_id uuid NOT NULL REFERENCES onboarding_registrations(id) ON DELETE RESTRICT,
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((payload->>'id') IS NOT DISTINCT FROM id::text),
 CHECK ((payload->>'project_id') IS NOT DISTINCT FROM project_id::text),
 CHECK ((payload->>'environment_id') IS NOT DISTINCT FROM environment_id::text),
 CHECK ((payload->>'repository_registration_id') IS NOT DISTINCT FROM repository_registration_id::text),
 CHECK ((payload->>'held_service_id') IS NOT DISTINCT FROM held_service_id::text),
 CHECK ((payload->'activation_held') IS NOT DISTINCT FROM 'true'::jsonb),
 CHECK ((payload->'active_service_registered') IS NOT DISTINCT FROM 'false'::jsonb),
 CHECK ((payload->'native_043_binding_proven') IS NOT DISTINCT FROM 'false'::jsonb)
);

CREATE FUNCTION validate_janua_inert_operation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS (
  SELECT 1 FROM janua_staging_preregistration_intents i
  JOIN projects p ON p.id=i.project_id
  JOIN environments e ON e.id=i.environment_id AND e.project_id=p.id
  JOIN onboarding_registrations r ON r.id=i.repository_registration_id AND r.project_id=p.id
  JOIN users u ON u.id=NEW.actor_id
  WHERE i.id=NEW.held_intent_id AND p.id=NEW.project_id AND p.slug='janua'
   AND e.id=NEW.environment_id AND e.name='staging' AND e.kube_namespace='janua-staging'
   AND r.id=NEW.repository_registration_id AND r.repo_full_name='madfam-org/janua' AND r.onboard_status='completed'
   AND u.is_platform_admin AND u.active
 ) THEN RAISE EXCEPTION 'owned held Janua identity required'; END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER janua_inert_operation_identity BEFORE INSERT ON janua_staging_inert_operations
 FOR EACH ROW EXECUTE FUNCTION validate_janua_inert_operation();

CREATE FUNCTION validate_janua_inert_authority() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM janua_staging_inert_operations o
  WHERE o.id=NEW.id AND o.held_service_id=NEW.held_service_id AND o.project_id=NEW.project_id
   AND o.environment_id=NEW.environment_id AND o.repository_registration_id=NEW.repository_registration_id
   AND (SELECT count(*) FROM janua_staging_inert_objects x WHERE x.operation_id=o.id)=6
 ) THEN RAISE EXCEPTION 'complete inert native object receipts required'; END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER janua_inert_authority_identity BEFORE INSERT ON janua_staging_inert_authorities
 FOR EACH ROW EXECUTE FUNCTION validate_janua_inert_authority();

CREATE FUNCTION refuse_janua_inert_rewrite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'inert staging history is immutable; reviewed retirement required'; END;
$$;
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['janua_staging_inert_operations','janua_staging_inert_objects','janua_staging_inert_authorities'] LOOP
  EXECUTE format('CREATE TRIGGER immutable_history BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION refuse_janua_inert_rewrite()',t);
  EXECUTE format('CREATE TRIGGER immutable_no_truncate BEFORE TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION refuse_janua_inert_rewrite()',t);
 END LOOP;
END $$;
