package januastage

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var ErrUnknown = errors.New("inert materialization outcome unconfirmed; inspect the same operation UUID")

type Store struct{ DB *sql.DB }

func DecodePlan(raw []byte) (Plan, error) {
	var plan Plan
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if len(raw) > 262144 || decoder.Decode(&plan) != nil || decoder.Decode(new(any)) != io.EOF || ValidatePlan(plan) != nil {
		return Plan{}, ErrRefused
	}
	return plan, nil
}

func ValidatePlan(p Plan) error {
	copy := p
	copy.SHA256 = ""
	if p.ID == uuid.Nil || p.ProjectID == uuid.Nil || p.EnvironmentID == uuid.Nil ||
		p.RepositoryRegistrationID == uuid.Nil || p.HeldIntentID == uuid.Nil ||
		p.HeldServiceID != HeldServiceID(p.ProjectID, p.HeldIntentID) || p.Profile != Profile ||
		!p.ActivationHeld || p.ActiveServiceRegistered || p.Native043BindingProven ||
		!validRevision(p.SourceRevision) || !hash.MatchString(p.SourceSHA256) ||
		p.NamespaceUID == "" || p.BaselineUID == "" || len(p.Objects) != 6 || Digest(copy) != p.SHA256 {
		return ErrRefused
	}
	for _, object := range p.Objects {
		if object.Body == nil || object.SHA256 != ObjectDigest(object.Body) ||
			object.Name != object.Body.GetName() || object.Kind != object.Body.GetKind() ||
			object.Namespace != object.Body.GetNamespace() || object.APIVersion != object.Body.GetAPIVersion() {
			return ErrRefused
		}
	}
	expected := map[string]string{
		"AppProject/" + Application: "argocd", "Application/" + Application: "argocd",
		"Deployment/" + Deployment: Namespace, "Deployment/janua-staging-redis": Namespace,
		"Service/" + Deployment: Namespace, "Service/janua-staging-redis": Namespace,
	}
	for _, object := range p.Objects {
		key := object.Kind + "/" + object.Name
		if expected[key] != object.Namespace || object.Body.GetLabels()["enclii.dev/managed-by"] != Manager ||
			object.Body.GetLabels()["enclii.dev/inert-operation"] != p.ID.String() ||
			object.Body.GetLabels()["enclii.dev/project"] != p.ProjectID.String() {
			return ErrRefused
		}
		delete(expected, key)
		if object.Kind == "Deployment" {
			n, found, err := unstructured.NestedInt64(object.Body.Object, "spec", "replicas")
			if err != nil || !found || n != 0 {
				return ErrRefused
			}
		}
	}
	if len(expected) != 0 {
		return ErrRefused
	}
	return nil
}

func requestHash(actor uuid.UUID, reason string, p Plan) string {
	return Digest([]string{actor.String(), reason, p.SHA256})
}

// Prepare retains exact review intent before any native create. It does not
// insert services, source registration aliases, deployment targets or grants.
func (s Store) Prepare(ctx context.Context, actor uuid.UUID, reason, expected string, p Plan) error {
	if s.DB == nil || actor == uuid.Nil || strings.TrimSpace(reason) != reason || reason == "" ||
		len(reason) > 512 || strings.IndexFunc(reason, unicode.IsControl) >= 0 || expected != p.SHA256 || ValidatePlan(p) != nil {
		return ErrRefused
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return ErrRefused
	}
	defer tx.Rollback() //nolint:errcheck
	if err = currentAuthority(ctx, tx, actor, p); err != nil {
		return err
	}
	var existing []byte
	var existingRequest string
	err = tx.QueryRowContext(ctx, `SELECT payload,request_sha256 FROM janua_staging_inert_operations WHERE id=$1 FOR UPDATE`, p.ID).Scan(&existing, &existingRequest)
	if err == nil {
		prior, parseErr := DecodePlan(existing)
		if parseErr != nil || prior.SHA256 != p.SHA256 || existingRequest != requestHash(actor, reason, p) {
			return ErrRefused
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		raw, _ := json.Marshal(p)
		_, err = tx.ExecContext(ctx, `INSERT INTO janua_staging_inert_operations(id,held_intent_id,project_id,environment_id,repository_registration_id,actor_id,held_service_id,profile,plan_sha256,request_sha256,reason,payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
			p.ID, p.HeldIntentID, p.ProjectID, p.EnvironmentID, p.RepositoryRegistrationID, actor, p.HeldServiceID, Profile, p.SHA256, requestHash(actor, reason, p), reason, raw)
		if err != nil {
			return ErrRefused
		}
	} else {
		return ErrRefused
	}
	if tx.Commit() != nil {
		return ErrUnknown
	}
	return nil
}

func currentAuthority(ctx context.Context, tx *sql.Tx, actor uuid.UUID, p Plan) error {
	if _, err := tx.ExecContext(ctx, `SET LOCAL lock_timeout='3s'`); err != nil {
		return ErrRefused
	}
	if _, err := tx.ExecContext(ctx, `SET LOCAL statement_timeout='5s'`); err != nil {
		return ErrRefused
	}
	var project uuid.UUID
	if tx.QueryRowContext(ctx, `SELECT id FROM projects WHERE id=$1 AND slug='janua' FOR UPDATE`, p.ProjectID).Scan(&project) != nil || project != p.ProjectID {
		return ErrRefused
	}
	var operator bool
	if tx.QueryRowContext(ctx, `SELECT is_platform_admin AND active FROM users WHERE id=$1 FOR SHARE`, actor).Scan(&operator) != nil || !operator {
		return ErrRefused
	}
	var intent, environment, registration uuid.UUID
	if tx.QueryRowContext(ctx, `SELECT i.id,e.id,r.id FROM janua_staging_preregistration_intents i JOIN environments e ON e.id=i.environment_id JOIN onboarding_registrations r ON r.id=i.repository_registration_id WHERE i.id=$1 AND i.project_id=$2 AND e.id=$3 AND e.name='staging' AND e.kube_namespace='janua-staging' AND r.id=$4 AND r.repo_full_name='madfam-org/janua' AND r.onboard_status='completed' FOR SHARE OF i,e,r`, p.HeldIntentID, p.ProjectID, p.EnvironmentID, p.RepositoryRegistrationID).Scan(&intent, &environment, &registration) != nil || intent != p.HeldIntentID || environment != p.EnvironmentID || registration != p.RepositoryRegistrationID {
		return ErrRefused
	}
	var collision bool
	if tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM services WHERE id=$1 OR name IN ('janua-api-staging','janua-staging-redis') OR k8s_namespace='janua-staging')`, p.HeldServiceID).Scan(&collision) != nil || collision {
		return ErrRefused
	}
	return nil
}

// Session locks the durable operation while all native effects remain inert.
// Prepared intent survives a rolled-back/uncertain native sequence.
type Session struct {
	tx   *sql.Tx
	Plan Plan
}

func (s Store) Begin(ctx context.Context, actor uuid.UUID, reason string, id uuid.UUID) (*Session, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, ErrRefused
	}
	bad := func() (*Session, error) { _ = tx.Rollback(); return nil, ErrRefused }
	var raw []byte
	var request string
	if tx.QueryRowContext(ctx, `SELECT payload,request_sha256 FROM janua_staging_inert_operations WHERE id=$1`, id).Scan(&raw, &request) != nil {
		return bad()
	}
	plan, err := DecodePlan(raw)
	if err != nil || request != requestHash(actor, reason, plan) || currentAuthority(ctx, tx, actor, plan) != nil {
		return bad()
	}
	// Project first, operation second: same order as Prepare, including retries.
	var locked uuid.UUID
	if tx.QueryRowContext(ctx, `SELECT id FROM janua_staging_inert_operations WHERE id=$1 FOR UPDATE`, id).Scan(&locked) != nil || locked != id {
		return bad()
	}
	return &Session{tx: tx, Plan: plan}, nil
}

func (s *Session) Close() { _ = s.tx.Rollback() }

func (s *Session) RecordObject(ctx context.Context, object Object, live *unstructured.Unstructured) error {
	key := object.Kind + ":" + object.Namespace + "/" + object.Name
	var uid, digest string
	err := s.tx.QueryRowContext(ctx, `SELECT uid,object_sha256 FROM janua_staging_inert_objects WHERE operation_id=$1 AND object_key=$2`, s.Plan.ID, key).Scan(&uid, &digest)
	if err == nil {
		if uid != string(live.GetUID()) || digest != ObjectDigest(live) {
			return ErrRefused
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ErrRefused
	}
	raw, _ := json.Marshal(live.Object)
	_, err = s.tx.ExecContext(ctx, `INSERT INTO janua_staging_inert_objects(operation_id,object_key,uid,object_sha256,payload) VALUES($1,$2,$3,$4,$5)`, s.Plan.ID, key, string(live.GetUID()), ObjectDigest(live), raw)
	return err
}

func (s *Session) Complete(ctx context.Context, a Authority) error {
	if ValidateAuthorityShape(a) != nil || a.ID != s.Plan.ID || a.PlanSHA256 != s.Plan.SHA256 ||
		a.ProjectID != s.Plan.ProjectID || a.EnvironmentID != s.Plan.EnvironmentID ||
		a.RepositoryRegistrationID != s.Plan.RepositoryRegistrationID || a.HeldIntentID != s.Plan.HeldIntentID ||
		a.HeldServiceID != s.Plan.HeldServiceID || a.NamespaceUID != s.Plan.NamespaceUID ||
		a.SourceRevision != s.Plan.SourceRevision || a.SourceSHA256 != s.Plan.SourceSHA256 {
		return ErrRefused
	}
	expected := AuthorityObjectUIDs(a)
	rows, err := s.tx.QueryContext(ctx, `SELECT object_key,uid FROM janua_staging_inert_objects WHERE operation_id=$1`, s.Plan.ID)
	if err != nil {
		return ErrRefused
	}
	for rows.Next() {
		var key, uid string
		if rows.Scan(&key, &uid) != nil || expected[key] != uid {
			_ = rows.Close()
			return ErrRefused
		}
		delete(expected, key)
	}
	failed := rows.Err() != nil || len(expected) != 0
	_ = rows.Close()
	if failed {
		return ErrRefused
	}
	raw, _ := json.Marshal(a)
	_, err = s.tx.ExecContext(ctx, `INSERT INTO janua_staging_inert_authorities(id,held_service_id,project_id,environment_id,repository_registration_id,payload) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(id) DO NOTHING`, a.ID, a.HeldServiceID, a.ProjectID, a.EnvironmentID, a.RepositoryRegistrationID, raw)
	if err != nil {
		return ErrRefused
	}
	if s.tx.Commit() != nil {
		return ErrUnknown
	}
	return nil
}

func AuthorityObjectUIDs(a Authority) map[string]string {
	return map[string]string{
		"AppProject:argocd/" + Application:                 a.AppProjectUID,
		"Application:argocd/" + Application:                a.ApplicationUID,
		"Deployment:" + Namespace + "/" + Deployment:       a.DeploymentUID,
		"Service:" + Namespace + "/" + Deployment:          a.APIServiceUID,
		"Deployment:" + Namespace + "/janua-staging-redis": a.RedisDeploymentUID,
		"Service:" + Namespace + "/janua-staging-redis":    a.RedisServiceUID,
	}
}

func (s Store) Read(ctx context.Context, id uuid.UUID) (Plan, Authority, error) {
	var p, a []byte
	if s.DB.QueryRowContext(ctx, `SELECT o.payload,a.payload FROM janua_staging_inert_operations o JOIN janua_staging_inert_authorities a ON a.id=o.id WHERE o.id=$1`, id).Scan(&p, &a) != nil {
		return Plan{}, Authority{}, ErrRefused
	}
	plan, err := DecodePlan(p)
	var authority Authority
	decoder := json.NewDecoder(bytes.NewReader(a))
	decoder.DisallowUnknownFields()
	if err != nil || len(a) > 8192 || decoder.Decode(&authority) != nil || decoder.Decode(new(any)) != io.EOF || ValidateAuthorityShape(authority) != nil || authority.PlanSHA256 != plan.SHA256 || authority.ID != id ||
		authority.ProjectID != plan.ProjectID || authority.EnvironmentID != plan.EnvironmentID ||
		authority.RepositoryRegistrationID != plan.RepositoryRegistrationID || authority.HeldIntentID != plan.HeldIntentID ||
		authority.HeldServiceID != plan.HeldServiceID || authority.NamespaceUID != plan.NamespaceUID ||
		authority.SourceRevision != plan.SourceRevision || authority.SourceSHA256 != plan.SourceSHA256 {
		return Plan{}, Authority{}, ErrRefused
	}
	return plan, authority, nil
}
