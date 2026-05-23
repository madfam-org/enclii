package db

import (
	"context"

	"github.com/google/uuid"
)

// WaybillThrottleRepository reads active deploy throttle rows (P2.2 budgets).
type WaybillThrottleRepository struct {
	db DBTX
}

// NewWaybillThrottleRepository creates a throttle repository.
func NewWaybillThrottleRepository(db DBTX) *WaybillThrottleRepository {
	return &WaybillThrottleRepository{db: db}
}

// NewWaybillThrottleRepositoryWithTx creates a repository using a transaction.
func NewWaybillThrottleRepositoryWithTx(tx DBTX) *WaybillThrottleRepository {
	return NewWaybillThrottleRepository(tx)
}

// HasActive returns true when an uncleared throttle exists for project + env scope.
func (r *WaybillThrottleRepository) HasActive(ctx context.Context, projectID uuid.UUID, envScope string) (bool, error) {
	const q = `
		SELECT EXISTS(
			SELECT 1 FROM waybill_throttles
			WHERE project_id = $1 AND env_scope = $2 AND cleared_at IS NULL
		)`
	var exists bool
	if err := r.db.QueryRowContext(ctx, q, projectID, envScope).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}
