package januastagedb

import (
	"context"
	"database/sql"
	"encoding/json"
	ktypes "k8s.io/apimachinery/pkg/types"
	"reflect"
	"time"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastage"
)

// SQLJournal is deliberately outside services/database_addons and all legacy
// background activation consumers. Migration048 depends on inert authority046.
type SQLJournal struct{ DB *sql.DB }

type queryRow interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

const authoritySQL = `SELECT a.payload FROM janua_staging_inert_authorities a
 JOIN users u ON u.id=$2 AND u.active AND u.is_platform_admin
 JOIN projects p ON p.id=a.project_id AND p.slug='janua'
 JOIN environments e ON e.id=a.environment_id AND e.project_id=p.id
 JOIN managed_db_plans m ON m.code=$3 AND m.available AND m.engine='postgres'
 WHERE a.id=$1 AND e.name='staging' AND e.kube_namespace='janua-staging'
 AND m.cpu_request=$4 AND m.memory_request=$5 AND m.storage_gb=$6
 AND m.replica_count=$7 AND m.max_connections=$8 AND m.price_cents_month=$10
 AND NOT EXISTS (SELECT 1 FROM services s WHERE s.id=a.held_service_id)
 AND NOT EXISTS (SELECT 1 FROM database_addons d WHERE d.deleted_at IS NULL
 AND (d.environment_id=e.id OR d.k8s_namespace=$9))`

func verifyStoredAuthority(ctx context.Context, q queryRow, p Plan, lock bool) error {
	if p.Validate() != nil {
		return ErrRefused
	}
	query := authoritySQL
	if lock {
		query += " FOR SHARE OF a,u,p,e,m"
	}
	var raw []byte
	if q.QueryRowContext(ctx, query, p.Authority.ID, p.ActorID, p.Allocation.CatalogPlan, p.Allocation.CPU, p.Allocation.Memory, p.Allocation.StorageGi, p.Allocation.Replicas, p.Allocation.MaxConnections, Namespace(p.Authority.ProjectID), p.Allocation.PriceCentsMonth).Scan(&raw) != nil {
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

func (j SQLJournal) Check(ctx context.Context, p Plan) error {
	if j.DB == nil {
		return ErrRefused
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return verifyStoredAuthority(bounded, j.DB, p, false)
}

func (j SQLJournal) Claim(ctx context.Context, p Plan) error {
	if j.DB == nil {
		return ErrRefused
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := j.DB.BeginTx(bounded, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return ErrRefused
	}
	defer tx.Rollback()
	if verifyStoredAuthority(bounded, tx, p, true) != nil {
		return ErrRefused
	}
	payload, err := json.Marshal(p)
	if err != nil {
		return ErrRefused
	}
	// No ON CONFLICT/adoption: the unique held-service slot and immutable ID
	// arbitrate all concurrent operations. Unknown commits require a read, not retry.
	_, err = tx.ExecContext(bounded, `INSERT INTO janua_staging_database_operations
 (id,authority_id,held_service_id,actor_id,profile,plan_sha256,reason,payload)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, p.OperationID, p.Authority.ID, p.Authority.HeldServiceID, p.ActorID, Profile, p.SHA256, p.Reason, payload)
	if err != nil || tx.Commit() != nil {
		return ErrRefused
	}
	return nil
}

func (j SQLJournal) Record(ctx context.Context, p Plan, phase, uid string) error {
	if j.DB == nil || uid == "" || len(uid) > 128 {
		return ErrRefused
	}
	switch phase {
	case "RUNTIME_SECRET_CREATED", "CLUSTER_CREATED", "CLUSTER_SUBMITTED_RUNTIME_UNQUALIFIED":
	default:
		return ErrRefused
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := j.DB.BeginTx(bounded, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return ErrRefused
	}
	defer tx.Rollback()
	if verifyStoredAuthority(bounded, tx, p, true) != nil {
		return ErrRefused
	}
	result, err := tx.ExecContext(bounded, `INSERT INTO janua_staging_database_objects(operation_id,phase,uid)
 SELECT id,$3,$4 FROM janua_staging_database_operations
 WHERE id=$1 AND plan_sha256=$2 AND actor_id=$5`, p.OperationID, p.SHA256, phase, uid, p.ActorID)
	if err != nil {
		return ErrRefused
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 || tx.Commit() != nil {
		return ErrRefused
	}
	return nil
}

// Receipt binds custody to the exact successful native UIDs recorded by Apply.
// Matching labels/spec on a recreated Cluster cannot establish this authority.
func (j SQLJournal) Receipt(ctx context.Context, p Plan) (NativeReceipt, error) {
	if j.DB == nil {
		return NativeReceipt{}, ErrRefused
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := j.DB.BeginTx(bounded, &sql.TxOptions{Isolation: sql.LevelSerializable, ReadOnly: true})
	if err != nil {
		return NativeReceipt{}, ErrRefused
	}
	defer tx.Rollback()
	if verifyStoredAuthority(bounded, tx, p, false) != nil {
		return NativeReceipt{}, ErrRefused
	}
	var secret, cluster, submitted string
	err = tx.QueryRowContext(bounded, `SELECT s.uid,c.uid,r.uid FROM janua_staging_database_operations o
 JOIN janua_staging_database_objects s ON s.operation_id=o.id AND s.phase='RUNTIME_SECRET_CREATED'
 JOIN janua_staging_database_objects c ON c.operation_id=o.id AND c.phase='CLUSTER_CREATED'
 JOIN janua_staging_database_objects r ON r.operation_id=o.id AND r.phase='CLUSTER_SUBMITTED_RUNTIME_UNQUALIFIED'
 WHERE o.id=$1 AND o.plan_sha256=$2 AND o.actor_id=$3`, p.OperationID, p.SHA256, p.ActorID).Scan(&secret, &cluster, &submitted)
	if err != nil || secret == "" || cluster == "" || cluster != submitted || secret == cluster || tx.Commit() != nil {
		return NativeReceipt{}, ErrRefused
	}
	return NativeReceipt{ClusterUID: ktypes.UID(cluster), RuntimeSecretUID: ktypes.UID(secret)}, nil
}
