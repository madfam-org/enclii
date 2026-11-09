// Package stagingintents records held repository-ownership intentions only.
// It never reads credentials or invokes services, builds, native APIs or grants.
package stagingintents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

var ErrRefused = errors.New("held staging intent refused or unavailable")
var ErrCommitUnknown = errors.New("held intent commit unconfirmed; read the same operation UUID")
var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Store struct {
	DB        *sql.DB
	ProjectID uuid.UUID
}
type authority struct{ Project, Environment, Repository uuid.UUID }
type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

const authoritySQL = `SELECT p.id,e.id,r.id FROM projects p JOIN environments e ON e.project_id=p.id JOIN onboarding_registrations r ON r.project_id=p.id WHERE p.slug='janua' AND e.name='staging' AND e.kube_namespace='janua-staging' AND r.repo_full_name='madfam-org/janua' AND r.onboard_status='completed'`

func readAuthority(ctx context.Context, q queryer, lock bool) (authority, error) {
	var a authority
	statement := authoritySQL
	if lock {
		statement += " FOR SHARE OF p,e,r"
	}
	if q.QueryRowContext(ctx, statement).Scan(&a.Project, &a.Environment, &a.Repository) != nil || a.Project == uuid.Nil || a.Environment == uuid.Nil || a.Repository == uuid.Nil {
		return a, ErrRefused
	}
	return a, nil
}
func hash(v any) string {
	b, _ := json.Marshal(v)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
func plan(a authority, actor, id uuid.UUID) types.JanuaStagingIntent {
	r := types.NewJanuaStagingIntent()
	r.ID = id
	r.ProjectID = a.Project
	r.EnvironmentID = a.Environment
	r.RepositoryRegistrationID = a.Repository
	r.PlanSHA256 = hash(struct {
		Receipt types.JanuaStagingIntent
		Actor   uuid.UUID
	}{r, actor})
	return r
}
func requestHash(actor, id uuid.UUID, expected, reason string) string {
	return hash(struct {
		Actor, ID        uuid.UUID
		Expected, Reason string
	}{actor, id, expected, reason})
}
func validReason(reason string) bool {
	return strings.TrimSpace(reason) == reason && reason != "" && len(reason) <= 512 && strings.IndexFunc(reason, unicode.IsControl) < 0
}

func (s Store) Plan(ctx context.Context, actor, id uuid.UUID) (types.JanuaStagingIntent, error) {
	if s.DB == nil || s.ProjectID == uuid.Nil || actor == uuid.Nil || id == uuid.Nil {
		return types.JanuaStagingIntent{}, ErrRefused
	}
	a, err := readAuthority(ctx, s.DB, false)
	if err != nil {
		return types.JanuaStagingIntent{}, err
	}
	if a.Project != s.ProjectID {
		return types.JanuaStagingIntent{}, ErrRefused
	}
	r := plan(a, actor, id)
	if types.ValidateJanuaStagingIntent(r, id) != nil {
		return types.JanuaStagingIntent{}, ErrRefused
	}
	return r, nil
}

type retained struct {
	Receipt             types.JanuaStagingIntent
	Actor               uuid.UUID
	RequestHash, Reason string
}

func read(ctx context.Context, q queryer, id uuid.UUID) (*retained, error) {
	var raw []byte
	var r retained
	err := q.QueryRowContext(ctx, `SELECT payload,actor_id,request_sha256,reason FROM janua_staging_preregistration_intents WHERE id=$1`, id).Scan(&raw, &r.Actor, &r.RequestHash, &r.Reason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, ErrRefused
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if len(raw) > 8192 || d.Decode(&r.Receipt) != nil || d.Decode(new(any)) != io.EOF || types.ValidateJanuaStagingIntent(r.Receipt, id) != nil || !*r.Receipt.Recorded || r.Actor == uuid.Nil || !validReason(r.Reason) {
		return nil, ErrRefused
	}
	want := plan(authority{r.Receipt.ProjectID, r.Receipt.EnvironmentID, r.Receipt.RepositoryRegistrationID}, r.Actor, id)
	if r.Receipt.PlanSHA256 != want.PlanSHA256 || r.RequestHash != requestHash(r.Actor, id, r.Receipt.PlanSHA256, r.Reason) {
		return nil, ErrRefused
	}
	return &r, nil
}
func (s Store) Read(ctx context.Context, id uuid.UUID) (types.JanuaStagingIntent, error) {
	if s.DB == nil || s.ProjectID == uuid.Nil || id == uuid.Nil {
		return types.JanuaStagingIntent{}, ErrRefused
	}
	r, err := read(ctx, s.DB, id)
	if err != nil || r == nil || r.Receipt.ProjectID != s.ProjectID {
		return types.JanuaStagingIntent{}, ErrRefused
	}
	return r.Receipt, nil
}

func (s Store) Record(ctx context.Context, actor, id uuid.UUID, expected, reason string) (types.JanuaStagingIntent, error) {
	bad := func() (types.JanuaStagingIntent, error) { return types.JanuaStagingIntent{}, ErrRefused }
	if s.DB == nil || s.ProjectID == uuid.Nil || actor == uuid.Nil || id == uuid.Nil || !hashPattern.MatchString(expected) || !validReason(reason) {
		return bad()
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return bad()
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SET LOCAL lock_timeout='3s'`); err != nil {
		return bad()
	}
	if _, err = tx.ExecContext(ctx, `SET LOCAL statement_timeout='5s'`); err != nil {
		return bad()
	}
	var operator bool
	if tx.QueryRowContext(ctx, `SELECT is_platform_admin AND active FROM users WHERE id=$1 FOR SHARE`, actor).Scan(&operator) != nil || !operator {
		return bad()
	}
	// Project-first lock also orders concurrent fixed-slot records. No service
	// or source row is updated, and no runtime writer consumes this table.
	var project uuid.UUID
	if tx.QueryRowContext(ctx, `SELECT id FROM projects WHERE id=$1 AND slug='janua' FOR UPDATE`, s.ProjectID).Scan(&project) != nil || project != s.ProjectID {
		return bad()
	}
	existing, err := read(ctx, tx, id)
	if err != nil {
		return bad()
	}
	if existing != nil {
		if existing.Actor != actor || existing.Receipt.ProjectID != project || existing.RequestHash != requestHash(actor, id, expected, reason) {
			return bad()
		}
		if tx.Commit() != nil {
			return types.JanuaStagingIntent{}, ErrCommitUnknown
		}
		return existing.Receipt, nil
	}
	a, err := readAuthority(ctx, tx, true)
	if err != nil || a.Project != project {
		return bad()
	}
	r := plan(a, actor, id)
	if r.PlanSHA256 != expected {
		return bad()
	}
	recorded := true
	r.Recorded = &recorded
	r.RecordedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if types.ValidateJanuaStagingIntent(r, id) != nil {
		return bad()
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return bad()
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO janua_staging_preregistration_intents(id,project_id,environment_id,repository_registration_id,actor_id,plan_sha256,request_sha256,reason,payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, id, a.Project, a.Environment, a.Repository, actor, expected, requestHash(actor, id, expected, reason), reason, raw)
	if err != nil {
		return bad()
	}
	if tx.Commit() != nil {
		return types.JanuaStagingIntent{}, ErrCommitUnknown
	}
	return r, nil
}
