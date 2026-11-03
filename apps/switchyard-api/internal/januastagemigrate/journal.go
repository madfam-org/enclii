package januastagemigrate

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"time"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastagedb"
	ktypes "k8s.io/apimachinery/pkg/types"
)

type SQLJournal struct{ DB *sql.DB }
type rowReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

const databaseAuthoritySQL = `SELECT d.payload,s.uid,c.uid,r.uid FROM janua_staging_database_operations d
 JOIN janua_staging_inert_authorities a ON a.id=d.authority_id
 JOIN users u ON u.id=d.actor_id AND u.active AND u.is_platform_admin
 JOIN projects p ON p.id=a.project_id AND p.slug='janua'
 JOIN environments e ON e.id=a.environment_id AND e.project_id=p.id AND e.name='staging' AND e.kube_namespace='janua-staging'
 JOIN onboarding_registrations o ON o.id=a.repository_registration_id AND o.project_id=p.id AND o.repo_full_name='madfam-org/janua' AND o.onboard_status='completed'
 JOIN managed_db_plans m ON m.code=$5 AND m.available AND m.engine='postgres'
 JOIN janua_staging_database_objects s ON s.operation_id=d.id AND s.phase='RUNTIME_SECRET_CREATED'
 JOIN janua_staging_database_objects c ON c.operation_id=d.id AND c.phase='CLUSTER_CREATED'
 JOIN janua_staging_database_objects r ON r.operation_id=d.id AND r.phase='CLUSTER_SUBMITTED_RUNTIME_UNQUALIFIED'
 WHERE d.id=$1 AND d.authority_id=$2 AND d.actor_id=$3 AND d.plan_sha256=$4
 AND m.cpu_request=$6 AND m.memory_request=$7 AND m.storage_gb=$8 AND m.replica_count=$9 AND m.max_connections=$10 AND m.price_cents_month=$11
 AND NOT EXISTS(SELECT 1 FROM services s WHERE s.id=a.held_service_id)
 AND NOT EXISTS(SELECT 1 FROM database_addons x WHERE x.deleted_at IS NULL AND (x.environment_id=e.id OR x.k8s_namespace=$12))`

func verifyDatabase(ctx context.Context, q rowReader, p Plan, lock bool) error {
	if p.Validate() != nil {
		return ErrRefused
	}
	query := databaseAuthoritySQL
	if lock {
		query += " FOR SHARE OF d,a,u,p,e,o,m"
	}
	d := p.Database
	a := d.Allocation
	var raw []byte
	var secret, cluster, submitted string
	if q.QueryRowContext(ctx, query, d.OperationID, d.Authority.ID, d.ActorID, d.SHA256, a.CatalogPlan, a.CPU, a.Memory, a.StorageGi, a.Replicas, a.MaxConnections, a.PriceCentsMonth, januastagedb.Namespace(d.Authority.ProjectID)).Scan(&raw, &secret, &cluster, &submitted) != nil || cluster != string(p.ClusterUID) || cluster != submitted || secret != string(p.RuntimeSecretUID) {
		return ErrRefused
	}
	var retained januastagedb.Plan
	if json.Unmarshal(raw, &retained) != nil {
		return ErrRefused
	}
	retained.Reason = d.Reason
	if retained.Validate() != nil || !reflect.DeepEqual(retained, d) {
		return ErrRefused
	}
	return nil
}
func (j SQLJournal) Check(ctx context.Context, p Plan) error {
	if j.DB == nil {
		return ErrRefused
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return verifyDatabase(ctx, j.DB, p, false)
}
func (j SQLJournal) Claim(ctx context.Context, p Plan) error {
	if j.DB == nil {
		return ErrRefused
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, e := j.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if e != nil {
		return ErrRefused
	}
	defer tx.Rollback()
	if verifyDatabase(ctx, tx, p, true) != nil {
		return ErrRefused
	}
	raw, e := json.Marshal(p)
	if e != nil {
		return ErrRefused
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO janua_staging_migration_operations(id,database_operation_id,authority_id,actor_id,profile,plan_sha256,reason,payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, p.OperationID, p.Database.OperationID, p.Database.Authority.ID, p.Database.ActorID, Profile, p.SHA256, p.Reason, raw)
	if e != nil || tx.Commit() != nil {
		return ErrRefused
	}
	return nil
}

// Historical read/credential removal does not execute a new migration or rely
// on current active-source/CI/network availability. It still requires current
// exact operator rank and the complete immutable retained operation plan.
func verifyCleanup(ctx context.Context, q rowReader, p Plan) error {
	if p.Validate() != nil {
		return ErrRefused
	}
	var raw []byte
	if q.QueryRowContext(ctx, `SELECT m.payload FROM janua_staging_migration_operations m JOIN users u ON u.id=m.actor_id AND u.active AND u.is_platform_admin WHERE m.id=$1 AND m.plan_sha256=$2 AND m.actor_id=$3`, p.OperationID, p.SHA256, p.Database.ActorID).Scan(&raw) != nil {
		return ErrRefused
	}
	var retained Plan
	if json.Unmarshal(raw, &retained) != nil {
		return ErrRefused
	}
	retained.Reason = p.Reason
	retained.Database.Reason = p.Database.Reason
	if retained.Validate() != nil || !reflect.DeepEqual(retained, p) {
		return ErrRefused
	}
	return nil
}
func (j SQLJournal) CheckCleanup(ctx context.Context, p Plan) error {
	if j.DB == nil {
		return ErrRefused
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return verifyCleanup(ctx, j.DB, p)
}
func (j SQLJournal) Record(ctx context.Context, p Plan, phase string, uid ktypes.UID, sha string) error {
	if j.DB == nil || len(uid) == 0 || len(uid) > 128 || !digest.MatchString(sha) {
		return ErrRefused
	}
	switch phase {
	case "SUSPENDED_JOB_CREATED", "OWNER_SECRET_CREATED", "JOB_STARTED", "OWNER_SECRET_REMOVED":
	default:
		return ErrRefused
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, e := j.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if e != nil {
		return ErrRefused
	}
	defer tx.Rollback()
	if phase == "OWNER_SECRET_REMOVED" {
		if verifyCleanup(ctx, tx, p) != nil {
			return ErrRefused
		}
	} else {
		if verifyDatabase(ctx, tx, p, true) != nil || verifyCleanup(ctx, tx, p) != nil {
			return ErrRefused
		}
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO janua_staging_migration_objects(operation_id,phase,uid,object_sha256) VALUES($1,$2,$3,$4)`, p.OperationID, phase, string(uid), sha)
	if e != nil || tx.Commit() != nil {
		return ErrRefused
	}
	return nil
}
func (j SQLJournal) Receipt(ctx context.Context, p Plan) (NativeReceipt, error) {
	if j.DB == nil {
		return NativeReceipt{}, ErrRefused
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, e := j.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable, ReadOnly: true})
	if e != nil {
		return NativeReceipt{}, ErrRefused
	}
	defer tx.Rollback()
	if verifyCleanup(ctx, tx, p) != nil {
		return NativeReceipt{}, ErrRefused
	}
	var job, secret, secretSHA string
	var started, removed bool
	e = tx.QueryRowContext(ctx, `SELECT j.uid,s.uid,s.object_sha256,EXISTS(SELECT 1 FROM janua_staging_migration_objects r WHERE r.operation_id=m.id AND r.phase='JOB_STARTED' AND r.uid=j.uid),EXISTS(SELECT 1 FROM janua_staging_migration_objects x WHERE x.operation_id=m.id AND x.phase='OWNER_SECRET_REMOVED' AND x.uid=s.uid) FROM janua_staging_migration_operations m JOIN janua_staging_migration_objects j ON j.operation_id=m.id AND j.phase='SUSPENDED_JOB_CREATED' JOIN janua_staging_migration_objects s ON s.operation_id=m.id AND s.phase='OWNER_SECRET_CREATED' WHERE m.id=$1`, p.OperationID).Scan(&job, &secret, &secretSHA, &started, &removed)
	if e != nil || job == "" || secret == "" || job == secret || tx.Commit() != nil {
		return NativeReceipt{}, ErrRefused
	}
	return NativeReceipt{JobUID: ktypes.UID(job), OwnerSecretUID: ktypes.UID(secret), Started: started, OwnerRemoved: removed, OwnerSecretSHA256: secretSHA}, nil
}
func (j SQLJournal) Outcome(ctx context.Context, p Plan, outcome Outcome) error {
	if j.DB == nil || outcome.Validate(p) != nil {
		return ErrRefused
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, e := j.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if e != nil {
		return ErrRefused
	}
	defer tx.Rollback()
	if verifyCleanup(ctx, tx, p) != nil {
		return ErrRefused
	}
	raw, e := json.Marshal(outcome)
	if e != nil {
		return ErrRefused
	}
	// A repeated identical read may confirm existing history; a different native
	// or schema outcome never overwrites it.
	result, e := tx.ExecContext(ctx, `INSERT INTO janua_staging_migration_outcomes(operation_id,state,payload) VALUES($1,$2,$3) ON CONFLICT(operation_id) DO NOTHING`, p.OperationID, outcome.State, raw)
	if e != nil {
		return ErrRefused
	}
	count, e := result.RowsAffected()
	if e != nil {
		return ErrRefused
	}
	if count == 0 {
		var prior []byte
		if tx.QueryRowContext(ctx, `SELECT payload FROM janua_staging_migration_outcomes WHERE operation_id=$1`, p.OperationID).Scan(&prior) != nil {
			return ErrRefused
		}
		var existing Outcome
		if json.Unmarshal(prior, &existing) != nil || !reflect.DeepEqual(existing, outcome) {
			return ErrRefused
		}
	}
	if tx.Commit() != nil {
		return ErrRefused
	}
	return nil
}

func (j SQLJournal) ReadOutcome(ctx context.Context, p Plan) (*Outcome, error) {
	if j.DB == nil {
		return nil, ErrRefused
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, e := j.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable, ReadOnly: true})
	if e != nil {
		return nil, ErrRefused
	}
	defer tx.Rollback()
	if verifyCleanup(ctx, tx, p) != nil {
		return nil, ErrRefused
	}
	var raw []byte
	e = tx.QueryRowContext(ctx, `SELECT payload FROM janua_staging_migration_outcomes WHERE operation_id=$1`, p.OperationID).Scan(&raw)
	if e == sql.ErrNoRows {
		if tx.Commit() != nil {
			return nil, ErrRefused
		}
		return nil, nil
	}
	if e != nil {
		return nil, ErrRefused
	}
	var outcome Outcome
	if json.Unmarshal(raw, &outcome) != nil || outcome.Validate(p) != nil || tx.Commit() != nil {
		return nil, ErrRefused
	}
	return &outcome, nil
}
