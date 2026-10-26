-- Immutable encrypted plans and append-only dispatch/recovery evidence. This
-- migration grants no provider or Kubernetes permission and stores no key.
CREATE TABLE argocd_backup_operations (
    operation_id uuid PRIMARY KEY CHECK (operation_id <> '00000000-0000-0000-0000-000000000000'),
    plan_id uuid NOT NULL UNIQUE CHECK (plan_id <> '00000000-0000-0000-0000-000000000000'),
    actor_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    namespace_uid uuid NOT NULL CHECK (namespace_uid <> '00000000-0000-0000-0000-000000000000'),
    profile text NOT NULL CHECK (profile = 'enclii-argocd-protected-backup-v1'),
    topology text NOT NULL CHECK (topology = 'NON_HA_LAYOUT_OBSERVED'),
    backup_id uuid NOT NULL UNIQUE CHECK (backup_id <> '00000000-0000-0000-0000-000000000000'),
    key_origin_id uuid NOT NULL CHECK (key_origin_id <> '00000000-0000-0000-0000-000000000000' AND (get_byte(uuid_send(key_origin_id),6) >> 4)=4 AND (get_byte(uuid_send(key_origin_id),8) & 192)=128),
    key_version integer NOT NULL CHECK (key_version > 0),
    fixed_count integer NOT NULL CHECK (fixed_count = 60),
    secret_count integer NOT NULL CHECK (secret_count BETWEEN 2 AND 128),
    configmap_count integer NOT NULL CHECK (configmap_count BETWEEN 7 AND 128),
    application_count integer NOT NULL CHECK (application_count BETWEEN 1 AND 512),
    applicationset_count integer NOT NULL CHECK (applicationset_count BETWEEN 1 AND 128),
    appproject_count integer NOT NULL CHECK (appproject_count BETWEEN 1 AND 128),
    auxiliary_count integer NOT NULL CHECK (auxiliary_count BETWEEN 0 AND 9),
    total_count integer NOT NULL CHECK (total_count = 60 + secret_count-2 + configmap_count-7 + application_count + applicationset_count + appproject_count + auxiliary_count),
    reason text NOT NULL CHECK (octet_length(reason) BETWEEN 1 AND 1000 AND btrim(reason) <> '' AND reason !~ '[[:cntrl:]]'),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > created_at),
    ciphertext bytea NOT NULL CHECK (octet_length(ciphertext) BETWEEN 29 AND 33554460),
    ciphertext_sha256 text NOT NULL CHECK (ciphertext_sha256 ~ '^[0-9a-f]{64}$' AND ciphertext_sha256 = encode(sha256(ciphertext), 'hex')),
    plan_sha256 text NOT NULL CHECK (plan_sha256 ~ '^[0-9a-f]{64}$')
);

CREATE TABLE argocd_backup_events (
    operation_id uuid NOT NULL REFERENCES argocd_backup_operations(operation_id) ON DELETE RESTRICT,
    sequence smallint NOT NULL CHECK (sequence BETWEEN 1 AND 11),
    kind text NOT NULL CHECK (kind IN ('PREPARED','DISPATCH_RESERVED','DISPATCH_CLAIMED','PUT_ACKNOWLEDGED','PUT_EXISTS','PUT_CONFLICT','PUT_REFUSED','PUT_UNKNOWN','READBACK_UNQUALIFIED','READBACK_AUTHENTICATED','PREPARATION_RETIRED')),
    actor_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    ciphertext_sha256 text,
    retirement_reason text,
    PRIMARY KEY (operation_id, sequence),
    UNIQUE (operation_id, kind),
    CHECK ((kind = 'READBACK_AUTHENTICATED' AND ciphertext_sha256 IS NOT NULL AND ciphertext_sha256 ~ '^[0-9a-f]{64}$') OR (kind <> 'READBACK_AUTHENTICATED' AND ciphertext_sha256 IS NULL)),
    CHECK ((kind = 'PREPARATION_RETIRED' AND retirement_reason IS NOT NULL AND retirement_reason IN ('expired','source_drift','operator_withdrew_before_dispatch')) OR (kind <> 'PREPARATION_RETIRED' AND retirement_reason IS NULL))
);
CREATE UNIQUE INDEX argocd_backup_one_provider_outcome ON argocd_backup_events(operation_id)
    WHERE kind IN ('PUT_ACKNOWLEDGED','PUT_EXISTS','PUT_CONFLICT','PUT_REFUSED','PUT_UNKNOWN');

CREATE FUNCTION argocd_backup_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'argocd protected backup journal is append only';
END $$;
CREATE TRIGGER argocd_backup_operations_immutable BEFORE UPDATE OR DELETE ON argocd_backup_operations FOR EACH ROW EXECUTE FUNCTION argocd_backup_append_only();
CREATE TRIGGER argocd_backup_operations_no_truncate BEFORE TRUNCATE ON argocd_backup_operations FOR EACH STATEMENT EXECUTE FUNCTION argocd_backup_append_only();
CREATE TRIGGER argocd_backup_events_immutable BEFORE UPDATE OR DELETE ON argocd_backup_events FOR EACH ROW EXECUTE FUNCTION argocd_backup_append_only();
CREATE TRIGGER argocd_backup_events_no_truncate BEFORE TRUNCATE ON argocd_backup_events FOR EACH STATEMENT EXECUTE FUNCTION argocd_backup_append_only();

-- The fence is fixed to this installation/profile, not the observed Namespace
-- UID. Namespace recreation cannot bypass an unresolved encrypted operation.
CREATE FUNCTION argocd_backup_operation_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('enclii:argocd:protected-backup:v1', 0));
    IF NOT EXISTS (SELECT 1 FROM users WHERE id=NEW.actor_id AND is_platform_admin AND active) OR
       EXISTS (SELECT 1 FROM argocd_backup_operations o WHERE NOT EXISTS (
           SELECT 1 FROM argocd_backup_events e WHERE e.operation_id=o.operation_id AND e.kind IN ('READBACK_AUTHENTICATED','PREPARATION_RETIRED'))) THEN
        RAISE EXCEPTION 'argocd protected backup operation held';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER argocd_backup_operation_fence BEFORE INSERT ON argocd_backup_operations FOR EACH ROW EXECUTE FUNCTION argocd_backup_operation_guard();

CREATE FUNCTION argocd_backup_event_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    op argocd_backup_operations%ROWTYPE;
    next_sequence integer;
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('enclii:argocd:protected-backup:v1', 0));
    SELECT * INTO STRICT op FROM argocd_backup_operations WHERE operation_id=NEW.operation_id;
    IF NEW.actor_id<>op.actor_id OR NOT EXISTS (SELECT 1 FROM users WHERE id=NEW.actor_id AND is_platform_admin AND active) THEN
        RAISE EXCEPTION 'argocd protected backup operator held';
    END IF;
    SELECT COALESCE(MAX(sequence),0)+1 INTO next_sequence FROM argocd_backup_events WHERE operation_id=NEW.operation_id;
    IF NEW.sequence<>next_sequence THEN RAISE EXCEPTION 'argocd protected backup sequence held'; END IF;
    IF EXISTS (SELECT 1 FROM argocd_backup_events WHERE operation_id=NEW.operation_id AND kind='PREPARATION_RETIRED') THEN
        RAISE EXCEPTION 'argocd protected backup retired';
    END IF;
    IF NEW.kind='PREPARED' AND next_sequence<>1 THEN RAISE EXCEPTION 'argocd protected backup transition held'; END IF;
    IF NEW.kind<>'PREPARED' AND NOT EXISTS (SELECT 1 FROM argocd_backup_events WHERE operation_id=NEW.operation_id AND kind='PREPARED') THEN
        RAISE EXCEPTION 'argocd protected backup transition held';
    END IF;
    IF NEW.kind='DISPATCH_RESERVED' AND (op.expires_at<=clock_timestamp() OR EXISTS (
        SELECT 1 FROM argocd_backup_events WHERE operation_id=NEW.operation_id AND kind IN ('DISPATCH_RESERVED','DISPATCH_CLAIMED','READBACK_AUTHENTICATED'))) THEN
        RAISE EXCEPTION 'argocd protected backup dispatch held';
    END IF;
    IF NEW.kind='DISPATCH_CLAIMED' AND (NOT EXISTS (SELECT 1 FROM argocd_backup_events WHERE operation_id=NEW.operation_id AND kind='DISPATCH_RESERVED') OR EXISTS (
        SELECT 1 FROM argocd_backup_events WHERE operation_id=NEW.operation_id AND kind='READBACK_AUTHENTICATED')) THEN
        RAISE EXCEPTION 'argocd protected backup claim held';
    END IF;
    IF NEW.kind IN ('PUT_ACKNOWLEDGED','PUT_EXISTS','PUT_CONFLICT','PUT_REFUSED','PUT_UNKNOWN') AND NOT EXISTS (
        SELECT 1 FROM argocd_backup_events WHERE operation_id=NEW.operation_id AND kind='DISPATCH_CLAIMED') THEN
        RAISE EXCEPTION 'argocd protected backup outcome held';
    END IF;
    IF NEW.kind='READBACK_AUTHENTICATED' AND NEW.ciphertext_sha256 IS DISTINCT FROM op.ciphertext_sha256 THEN
        RAISE EXCEPTION 'argocd protected backup reference held';
    END IF;
    IF NEW.kind='PREPARATION_RETIRED' AND (next_sequence<>2 OR (NEW.retirement_reason='expired' AND op.expires_at>clock_timestamp())) THEN
        RAISE EXCEPTION 'argocd protected backup retirement held';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER argocd_backup_event_transition BEFORE INSERT ON argocd_backup_events FOR EACH ROW EXECUTE FUNCTION argocd_backup_event_guard();
