package januastagenetwork

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastage"
)

type ObjectReceipt struct {
	Index           int    `json:"index"`
	Key             string `json:"object_key"`
	UID             string `json:"uid"`
	ResourceVersion string `json:"resource_version"`
	SHA256          string `json:"observed_sha256"`
}
type Receipt struct {
	Plan      Plan            `json:"-"`
	Attempted int             `json:"create_intents"`
	Objects   []ObjectReceipt `json:"objects"`
	Complete  bool            `json:"configuration_present"`
}
type Journal interface {
	Lock(context.Context, Plan) (func(), error)
	Claim(context.Context, Plan, string) error
	Intent(context.Context, Plan, int, Object) error
	Record(context.Context, Plan, ObjectReceipt) error
	Complete(context.Context, Plan) error
	Read(context.Context, uuid.UUID, uuid.UUID) (Receipt, error)
}
type Store struct{ DB *sql.DB }

func (s Store) Check(ctx context.Context, p Plan) error {
	if s.DB == nil {
		return ErrRefused
	}
	return check(ctx, s.DB, p, false)
}

func decodePlan(b []byte) (Plan, error) {
	var p Plan
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if len(b) > 65536 || d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF || p.Validate() != nil {
		return p, ErrRefused
	}
	return p, nil
}

type rowReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

const authorityQuery = `SELECT a.payload FROM janua_staging_inert_authorities a
 JOIN projects p ON p.id=a.project_id AND p.slug='janua'
 JOIN environments e ON e.id=a.environment_id AND e.project_id=p.id
 JOIN onboarding_registrations r ON r.id=a.repository_registration_id AND r.project_id=p.id
 JOIN users u ON u.id=$2 AND u.active AND u.is_platform_admin
 WHERE a.id=$1 AND e.name='staging' AND e.kube_namespace='janua-staging'
 AND r.repo_full_name='madfam-org/janua' AND r.onboard_status='completed'
 AND NOT EXISTS(SELECT 1 FROM services s WHERE s.id=a.held_service_id)
 AND NOT EXISTS(SELECT 1 FROM database_addons d WHERE d.deleted_at IS NULL AND (d.environment_id=e.id OR d.k8s_namespace=$3))`

func check(ctx context.Context, q rowReader, p Plan, lock bool) error {
	if p.Validate() != nil {
		return ErrRefused
	}
	query := authorityQuery
	if lock {
		query += " FOR SHARE OF a,p,e,r,u"
	}
	var raw []byte
	if q.QueryRowContext(ctx, query, p.Authority.ID, p.ActorID, namespace(p)).Scan(&raw) != nil {
		return ErrRefused
	}
	var a januastage.Authority
	if json.Unmarshal(raw, &a) != nil || januastage.ValidateAuthorityShape(a) != nil {
		return ErrRefused
	}
	a.ObservedAt = ""
	if !reflect.DeepEqual(a, p.Authority) {
		return ErrRefused
	}
	return nil
}

// Locks access rows for the entire effect session. A separate committed child
// receipt can take FK KEY SHARE locks without colliding with these SHARE locks.
// No operation-row FOR UPDATE is held across child transactions.
func (s Store) Lock(ctx context.Context, p Plan) (func(), error) {
	if s.DB == nil {
		return nil, ErrRefused
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, ErrRefused
	}
	close := func() { _ = tx.Rollback() }
	if _, e = tx.ExecContext(ctx, `SET LOCAL lock_timeout='3s'`); e != nil {
		close()
		return nil, ErrRefused
	}
	var locked bool
	if tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1,0))`, Profile+":"+p.Authority.ID.String()).Scan(&locked) != nil || !locked || check(ctx, tx, p, true) != nil {
		close()
		return nil, ErrRefused
	}
	return close, nil
}
func (s Store) Claim(ctx context.Context, p Plan, reason string) error {
	if s.DB == nil || check(ctx, s.DB, p, false) != nil || reason == "" || len(reason) > 512 || strings.TrimSpace(reason) != reason || strings.IndexFunc(reason, unicode.IsControl) >= 0 {
		return ErrRefused
	}
	raw, e := json.Marshal(p)
	if e != nil {
		return ErrRefused
	}
	// Unique authority slot: every existing operation is inspect-only, including
	// a caller's exact replay. There is no outcome-dependent create retry.
	_, e = s.DB.ExecContext(ctx, `INSERT INTO janua_staging_network_operations(id,authority_id,actor_id,profile,plan_sha256,reason,payload) VALUES($1,$2,$3,$4,$5,$6,$7)`, p.ID, p.Authority.ID, p.ActorID, Profile, p.SHA256, reason, raw)
	if e != nil {
		return ErrUnknown
	}
	return nil
}
func (s Store) Intent(ctx context.Context, p Plan, index int, object Object) error {
	if check(ctx, s.DB, p, false) != nil || index < 0 || index >= len(p.Objects()) || object.Key != p.Objects()[index].Key {
		return ErrRefused
	}
	_, _, sha, e := Projection(object.Body)
	if e != nil {
		return ErrRefused
	}
	_, e = s.DB.ExecContext(ctx, `INSERT INTO janua_staging_network_attempts(operation_id,ordinal,object_key,desired_sha256) VALUES($1,$2,$3,$4)`, p.ID, index, object.Key, sha)
	if e != nil {
		return ErrUnknown
	}
	return nil
}
func (s Store) Record(ctx context.Context, p Plan, r ObjectReceipt) error {
	if check(ctx, s.DB, p, false) != nil || r.UID == "" || r.ResourceVersion == "" || !digest.MatchString(r.SHA256) {
		return ErrRefused
	}
	_, e := s.DB.ExecContext(ctx, `INSERT INTO janua_staging_network_receipts(operation_id,ordinal,object_key,uid,resource_version,observed_sha256) VALUES($1,$2,$3,$4,$5,$6)`, p.ID, r.Index, r.Key, r.UID, r.ResourceVersion, r.SHA256)
	if e != nil {
		return ErrUnknown
	}
	return nil
}
func (s Store) Complete(ctx context.Context, p Plan) error {
	if check(ctx, s.DB, p, false) != nil {
		return ErrRefused
	}
	_, e := s.DB.ExecContext(ctx, `INSERT INTO janua_staging_network_completions(operation_id,plan_sha256) VALUES($1,$2)`, p.ID, p.SHA256)
	if e != nil {
		return ErrUnknown
	}
	return nil
}
func (s Store) Read(ctx context.Context, id, actor uuid.UUID) (Receipt, error) {
	var r Receipt
	var raw []byte
	if s.DB == nil || s.DB.QueryRowContext(ctx, `SELECT payload FROM janua_staging_network_operations WHERE id=$1 AND actor_id=$2`, id, actor).Scan(&raw) != nil {
		return r, ErrRefused
	}
	p, e := decodePlan(raw)
	if e != nil || check(ctx, s.DB, p, false) != nil {
		return r, ErrRefused
	}
	r.Plan = p
	if s.DB.QueryRowContext(ctx, `SELECT count(*) FROM janua_staging_network_attempts WHERE operation_id=$1`, id).Scan(&r.Attempted) != nil {
		return r, ErrRefused
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT ordinal,object_key,uid,resource_version,observed_sha256 FROM janua_staging_network_receipts WHERE operation_id=$1 ORDER BY ordinal`, id)
	if e != nil {
		return r, ErrRefused
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var x ObjectReceipt
		if rows.Scan(&x.Index, &x.Key, &x.UID, &x.ResourceVersion, &x.SHA256) != nil {
			return r, ErrRefused
		}
		r.Objects = append(r.Objects, x)
	}
	if rows.Err() != nil || s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM janua_staging_network_completions WHERE operation_id=$1)`, id).Scan(&r.Complete) != nil {
		return r, ErrRefused
	}
	return r, nil
}
