// Package jobholds serializes operator containment with job creation/reconciliation.
package jobholds

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const Annotation = "enclii.dev/operator-job-hold"

type Binding struct {
	ProjectID     uuid.UUID
	ServiceID     uuid.UUID
	EnvironmentID uuid.UUID
	ServiceName   string
}

type Hold struct {
	Binding
	Reason          string
	Actor           string
	UID             string
	ResourceVersion string
}

type Store struct{ DB *sql.DB }
type Session struct{ conn *sql.Tx }

// WithLock serializes hold writes, service reconciliation and manual triggers.
// Transaction locks also work behind transaction-pooled PostgreSQL proxies.
// Suspension commits its hold in one call before applying Kubernetes in another.
func (s *Store) WithLock(ctx context.Context, namespace, name string, fn func(context.Context, *Session) error) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("job hold store unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "enclii.job-hold:"+namespace+"/"+name); err != nil {
		return err
	}
	if err = fn(ctx, &Session{conn: tx}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Session) Resolve(ctx context.Context, project string, service uuid.UUID, namespace string) (Binding, error) {
	var b Binding
	err := s.conn.QueryRowContext(ctx, `SELECT p.id, s.id, e.id, s.name
 FROM services s JOIN projects p ON p.id=s.project_id
 JOIN environments e ON e.project_id=p.id
 WHERE p.slug=$1 AND s.id=$2 AND e.kube_namespace=$3
 AND (SELECT count(*) FROM environments WHERE kube_namespace=$3)=1`, project, service, namespace).Scan(&b.ProjectID, &b.ServiceID, &b.EnvironmentID, &b.ServiceName)
	return b, err
}

func (s *Session) Get(ctx context.Context, namespace, name string) (*Hold, error) {
	var h Hold
	err := s.conn.QueryRowContext(ctx, `SELECT project_id, service_id, environment_id, service_name,
 reason, actor_id, reviewed_uid, reviewed_resource_version
 FROM service_job_holds WHERE namespace=$1 AND cronjob_name=$2`, namespace, name).Scan(&h.ProjectID, &h.ServiceID, &h.EnvironmentID, &h.ServiceName, &h.Reason, &h.Actor, &h.UID, &h.ResourceVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &h, nil
}

func (s *Session) Put(ctx context.Context, namespace, name string, h Hold) error {
	_, err := s.conn.ExecContext(ctx, `INSERT INTO service_job_holds
 (namespace, cronjob_name, project_id, service_id, environment_id, service_name,
 reason, actor_id, reviewed_uid, reviewed_resource_version)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, namespace, name, h.ProjectID, h.ServiceID, h.EnvironmentID, h.ServiceName, h.Reason, h.Actor, h.UID, h.ResourceVersion)
	return err
}
