package workloadtargets

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

// Store reads immutable records. Writers require a locked Session.
type Queryer interface {
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
}
type Store struct {
	DB     *sql.DB
	Reader Queryer
}

func (s Store) queryer() Queryer {
	if s.Reader != nil {
		return s.Reader
	}
	if s.DB == nil {
		return nil
	}
	return s.DB
}

// NamespaceIntentExclusive checks only claims for this exact namespace. It
// does not scan or adopt fleet resources, rewrite labels or infer ownership.
func (s Store) NamespaceIntentExclusive(ctx context.Context, projectID uuid.UUID, namespace string) (bool, error) {
	if s.queryer() == nil || projectID == uuid.Nil || namespace == "" {
		return false, ErrUnavailable
	}
	var other bool
	err := s.queryer().QueryRowContext(ctx, `SELECT EXISTS (
 SELECT 1 FROM environments WHERE kube_namespace=$1 AND project_id<>$2
 UNION ALL
 SELECT 1 FROM onboarding_registrations WHERE config_snapshot->>'namespace'=$1 AND project_id<>$2
)`, namespace, projectID).Scan(&other)
	if err != nil {
		return false, ErrUnavailable
	}
	return !other, nil
}

func (s Store) Get(ctx context.Context, serviceID, environmentID uuid.UUID) (*Binding, error) {
	if s.queryer() == nil || serviceID == uuid.Nil || environmentID == uuid.Nil {
		return nil, ErrUnavailable
	}
	var raw []byte
	err := s.queryer().QueryRowContext(ctx, `SELECT payload FROM service_deployment_targets WHERE service_id=$1 AND environment_id=$2`, serviceID, environmentID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, ErrUnavailable
	}
	var binding Binding
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if len(raw) > 65536 || decoder.Decode(&binding) != nil || binding.Validate() != nil || binding.Identity.ServiceID != serviceID || binding.Identity.EnvironmentID != environmentID {
		return nil, ErrConflict
	}
	return &binding, nil
}
