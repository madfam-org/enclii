-- Intent history only. No service row, secret, grant, source binding or runtime
-- activation is created. The global onboarding FK proves repository ownership,
-- never a separate staging Argo Application/source registration.
CREATE TABLE janua_staging_preregistration_intents (
 id uuid PRIMARY KEY,
 project_id uuid NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 environment_id uuid NOT NULL REFERENCES environments(id) ON DELETE RESTRICT,
 repository_registration_id uuid NOT NULL REFERENCES onboarding_registrations(id) ON DELETE RESTRICT,
 actor_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 activation_held boolean NOT NULL DEFAULT true CHECK (activation_held),
 repository_registration_scope text NOT NULL DEFAULT 'REPOSITORY_OWNERSHIP_ONLY' CHECK (repository_registration_scope='REPOSITORY_OWNERSHIP_ONLY'),
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
 CHECK ((payload->>'repository_registration_scope') IS NOT DISTINCT FROM repository_registration_scope),
 CHECK ((payload->>'state') IS NOT DISTINCT FROM 'HELD_PREREGISTRATION_ONLY'),
 CHECK ((payload->>'profile') IS NOT DISTINCT FROM 'janua-staging-preregistration-v1'),
 CHECK ((payload->>'proposed_service_name') IS NOT DISTINCT FROM 'janua-api-staging'),
 CHECK ((payload->>'proposed_deployment_name') IS NOT DISTINCT FROM 'janua-api-staging'),
 CHECK ((payload->>'proposed_repository') IS NOT DISTINCT FROM 'https://github.com/madfam-org/janua.git'),
 CHECK ((payload->>'proposed_app_path') IS NOT DISTINCT FROM 'apps/api'),
 CHECK ((payload->>'proposed_namespace') IS NOT DISTINCT FROM 'janua-staging'),
 CHECK ((payload->>'proposed_manifest_path') IS NOT DISTINCT FROM 'k8s/overlays/staging'),
 CHECK ((payload->>'proposed_ref') IS NOT DISTINCT FROM 'main'),
 CHECK ((payload->'activation_held') IS NOT DISTINCT FROM 'true'::jsonb),
 CHECK ((payload->'service_materialized') IS NOT DISTINCT FROM 'false'::jsonb),
 CHECK ((payload->'native_binding_proven') IS NOT DISTINCT FROM 'false'::jsonb),
 CHECK ((payload->'materialization_supported') IS NOT DISTINCT FROM 'false'::jsonb),
 CHECK ((payload->'recorded') IS NOT DISTINCT FROM 'true'::jsonb),
 CHECK (COALESCE(length(payload->>'recorded_at'),0)>0),
 CHECK ((payload->>'plan_sha256') IS NOT DISTINCT FROM plan_sha256),
 CHECK ((payload->'unresolved_gates') IS NOT DISTINCT FROM '["SOURCE_STAGE_REGISTRATION_NOT_PROVEN","STAGING_SOURCE_ALIGNMENT_NOT_QUALIFIED","SERVICE_MATERIALIZATION_NOT_IMPLEMENTED","BACKGROUND_WRITER_EXCLUSION_NOT_QUALIFIED","NATIVE_043_BINDING_NOT_PROVEN","RUNTIME_CUSTODY_MIGRATION_AND_SOAK_NOT_QUALIFIED"]'::jsonb)
);

CREATE FUNCTION validate_janua_staging_preregistration() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS (
  SELECT 1 FROM projects p JOIN environments e ON e.project_id=p.id
  JOIN onboarding_registrations r ON r.project_id=p.id
  JOIN users u ON u.id=NEW.actor_id
  WHERE p.id=NEW.project_id AND p.slug='janua'
   AND e.id=NEW.environment_id AND e.name='staging' AND e.kube_namespace='janua-staging'
   AND r.id=NEW.repository_registration_id AND r.repo_full_name='madfam-org/janua' AND r.onboard_status='completed'
   AND u.is_platform_admin AND u.active
 ) THEN RAISE EXCEPTION 'closed held staging identity required'; END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER janua_staging_preregistration_identity BEFORE INSERT ON janua_staging_preregistration_intents
 FOR EACH ROW EXECUTE FUNCTION validate_janua_staging_preregistration();

CREATE FUNCTION refuse_janua_staging_preregistration_rewrite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'held preregistration history is immutable; reviewed retirement required'; END;
$$;
CREATE TRIGGER janua_staging_preregistration_immutable BEFORE UPDATE OR DELETE ON janua_staging_preregistration_intents
 FOR EACH ROW EXECUTE FUNCTION refuse_janua_staging_preregistration_rewrite();
CREATE TRIGGER janua_staging_preregistration_no_truncate BEFORE TRUNCATE ON janua_staging_preregistration_intents
 FOR EACH STATEMENT EXECUTE FUNCTION refuse_janua_staging_preregistration_rewrite();
