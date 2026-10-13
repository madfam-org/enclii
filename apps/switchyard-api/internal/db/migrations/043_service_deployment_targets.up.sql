-- Explicit namespace/Deployment identity is independent of health discovery.
-- No rows are backfilled, guessed from labels, or adopted from a legacy name.
CREATE TABLE service_deployment_targets (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    service_id uuid NOT NULL REFERENCES services(id) ON DELETE RESTRICT,
    environment_id uuid NOT NULL REFERENCES environments(id) ON DELETE RESTRICT,
    registration_id uuid NOT NULL REFERENCES onboarding_registrations(id) ON DELETE RESTRICT,
    namespace_uid text NOT NULL CHECK (length(namespace_uid) BETWEEN 1 AND 128),
    deployment_uid text NOT NULL CHECK (length(deployment_uid) BETWEEN 1 AND 128),
    actor_id text NOT NULL CHECK (length(trim(actor_id)) BETWEEN 1 AND 128),
    reason text NOT NULL CHECK (length(trim(reason)) BETWEEN 1 AND 1000),
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload)='object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (service_id, environment_id),
    UNIQUE (namespace_uid, deployment_uid),
    CHECK ((payload->>'id') IS NOT DISTINCT FROM id::text),
    CHECK ((payload#>>'{identity,project_id}') IS NOT DISTINCT FROM project_id::text),
    CHECK ((payload#>>'{identity,service_id}') IS NOT DISTINCT FROM service_id::text),
    CHECK ((payload#>>'{identity,environment_id}') IS NOT DISTINCT FROM environment_id::text),
    CHECK ((payload#>>'{identity,registration_id}') IS NOT DISTINCT FROM registration_id::text),
    CHECK ((payload#>>'{identity,namespace_uid}') IS NOT DISTINCT FROM namespace_uid),
    CHECK ((payload#>>'{identity,deployment_uid}') IS NOT DISTINCT FROM deployment_uid)
);

CREATE FUNCTION refuse_service_deployment_target_rewrite() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'explicit deployment target is immutable; reviewed retirement required';
END;
$$;

CREATE TRIGGER service_deployment_target_immutable
BEFORE UPDATE OR DELETE ON service_deployment_targets
FOR EACH ROW EXECUTE FUNCTION refuse_service_deployment_target_rewrite();

CREATE TRIGGER service_deployment_target_no_truncate
BEFORE TRUNCATE ON service_deployment_targets
FOR EACH STATEMENT EXECUTE FUNCTION refuse_service_deployment_target_rewrite();

-- Successful apply audit and UUID idempotency identity commit with the binding.
CREATE TABLE service_deployment_target_events (
 id uuid PRIMARY KEY,
 binding_id uuid NOT NULL UNIQUE REFERENCES service_deployment_targets(id) ON DELETE RESTRICT,
 actor_id text NOT NULL CHECK(length(trim(actor_id)) BETWEEN 1 AND 128),
 request_sha256 text NOT NULL CHECK(request_sha256 ~ '^[a-f0-9]{64}$'),
 reason text NOT NULL CHECK(length(trim(reason)) BETWEEN 1 AND 1000),
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK(id=binding_id),
 CHECK((payload->>'id') IS NOT DISTINCT FROM id::text),
 CHECK((payload->>'actor_id') IS NOT DISTINCT FROM actor_id),
 CHECK((payload->>'reason') IS NOT DISTINCT FROM reason),
 CHECK((payload->>'request_sha256') IS NOT DISTINCT FROM request_sha256),
 CHECK((payload#>>'{binding,id}') IS NOT DISTINCT FROM binding_id::text)
);
CREATE TRIGGER service_deployment_target_event_immutable BEFORE UPDATE OR DELETE ON service_deployment_target_events FOR EACH ROW EXECUTE FUNCTION refuse_service_deployment_target_rewrite();
CREATE TRIGGER service_deployment_target_event_no_truncate BEFORE TRUNCATE ON service_deployment_target_events FOR EACH STATEMENT EXECUTE FUNCTION refuse_service_deployment_target_rewrite();
