package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// CanaryRolloutRepository persists canary release state transitions.
//
// The table has a partial unique index `idx_canary_one_active_per_service`
// that enforces "at most one active rollout per service" at the database
// level. A second Create() call for a service with a non-terminal rollout
// returns a unique-violation error — callers should translate this into a
// 409 Conflict.
type CanaryRolloutRepository struct {
	db DBTX
}

func NewCanaryRolloutRepository(db DBTX) *CanaryRolloutRepository {
	return &CanaryRolloutRepository{db: db}
}

func NewCanaryRolloutRepositoryWithTx(tx DBTX) *CanaryRolloutRepository {
	return &CanaryRolloutRepository{db: tx}
}

// Create inserts a new rollout in `pending` state. Caller must set:
//   - ServiceID, EnvironmentID, StableDeploymentID, CanaryDeploymentID
//   - CanaryDigest, CanaryPercentage, TotalReplicas, CanaryReplicas,
//     StableReplicas, ValidationWindowSeconds, ErrorRateThreshold
//
// Create assigns ID, CreatedAt, UpdatedAt, and returns the unique-violation
// error when another rollout is already active for the service.
func (r *CanaryRolloutRepository) Create(ctx context.Context, ro *types.CanaryRollout) error {
	if ro.ID == uuid.Nil {
		ro.ID = uuid.New()
	}
	if ro.State == "" {
		ro.State = types.CanaryStatePending
	}
	now := time.Now().UTC()
	ro.CreatedAt = now
	ro.UpdatedAt = now

	query := `
		INSERT INTO canary_rollouts (
			id, service_id, environment_id,
			stable_deployment_id, canary_deployment_id,
			canary_digest, canary_percentage,
			total_replicas, canary_replicas, stable_replicas,
			validation_window_seconds, smoke_endpoint, error_rate_threshold,
			state, initiated_by, change_ticket_url,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18
		)
	`
	_, err := r.db.ExecContext(ctx, query,
		ro.ID, ro.ServiceID, ro.EnvironmentID,
		ro.StableDeploymentID, ro.CanaryDeploymentID,
		ro.CanaryDigest, ro.CanaryPercentage,
		ro.TotalReplicas, ro.CanaryReplicas, ro.StableReplicas,
		ro.ValidationWindowSeconds, nullableString(ro.SmokeEndpoint), ro.ErrorRateThreshold,
		ro.State, ro.InitiatedBy, nullableString(ro.ChangeTicketURL),
		ro.CreatedAt, ro.UpdatedAt,
	)
	return err
}

// GetByID returns a rollout by its UUID.
func (r *CanaryRolloutRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.CanaryRollout, error) {
	row := r.db.QueryRowContext(ctx, canarySelectColumns+" WHERE id = $1", id)
	return scanCanaryRollout(row)
}

// GetActiveByService returns the currently in-flight rollout for a service,
// or (nil, sql.ErrNoRows) if none.
func (r *CanaryRolloutRepository) GetActiveByService(ctx context.Context, serviceID uuid.UUID) (*types.CanaryRollout, error) {
	row := r.db.QueryRowContext(ctx,
		canarySelectColumns+` WHERE service_id = $1
			AND state IN ('pending', 'running', 'validating', 'promoting')
			ORDER BY created_at DESC LIMIT 1`, serviceID)
	return scanCanaryRollout(row)
}

// ListByService returns rollouts for a service, newest-first.
func (r *CanaryRolloutRepository) ListByService(ctx context.Context, serviceID uuid.UUID, limit int) ([]*types.CanaryRollout, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx,
		canarySelectColumns+" WHERE service_id = $1 ORDER BY created_at DESC LIMIT $2",
		serviceID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []*types.CanaryRollout
	for rows.Next() {
		ro, err := scanCanaryRollout(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ro)
	}
	return out, rows.Err()
}

// ListActive returns all in-flight rollouts across every service. Used by the
// reconciler controller to drive periodic state advancement.
func (r *CanaryRolloutRepository) ListActive(ctx context.Context) ([]*types.CanaryRollout, error) {
	rows, err := r.db.QueryContext(ctx,
		canarySelectColumns+` WHERE state IN ('pending', 'running', 'validating', 'promoting')
			ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []*types.CanaryRollout
	for rows.Next() {
		ro, err := scanCanaryRollout(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ro)
	}
	return out, rows.Err()
}

// UpdateState transitions the rollout to a new state and records timestamps
// of key milestones. Caller is responsible for state-machine legality — see
// reconciler.isLegalCanaryTransition for the authoritative rules.
func (r *CanaryRolloutRepository) UpdateState(ctx context.Context, id uuid.UUID, newState types.CanaryRolloutState, lastErr string) error {
	now := time.Now().UTC()

	// Set the corresponding milestone timestamp (only the first time we enter
	// a state — subsequent writes from reconciler loops should not overwrite).
	var setClause string
	switch newState {
	case types.CanaryStateRunning:
		setClause = ", started_at = COALESCE(started_at, $4)"
	case types.CanaryStateValidating:
		setClause = ", validating_started_at = COALESCE(validating_started_at, $4)"
	case types.CanaryStatePromoting:
		setClause = ", promoting_started_at = COALESCE(promoting_started_at, $4)"
	case types.CanaryStateSucceeded,
		types.CanaryStateAutoRolledBack,
		types.CanaryStateManualRolledBack,
		types.CanaryStateFailed:
		setClause = ", terminal_at = COALESCE(terminal_at, $4)"
	default:
		setClause = ", updated_at = $4"
	}

	query := fmt.Sprintf(`
		UPDATE canary_rollouts
		SET state = $2,
		    last_error = $3,
		    updated_at = $4
		    %s
		WHERE id = $1
	`, setClause)

	_, err := r.db.ExecContext(ctx, query, id, newState, nullableString(lastErr), now)
	return err
}

// SetNewStableDeployment records the deployment ID of the promoted (new)
// stable after auto-promotion builds it.
func (r *CanaryRolloutRepository) SetNewStableDeployment(ctx context.Context, id, newStableID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE canary_rollouts SET new_stable_deployment_id = $2, updated_at = NOW() WHERE id = $1`,
		id, newStableID)
	return err
}

// SetRollbackReason annotates a rollback with a human-readable cause.
func (r *CanaryRolloutRepository) SetRollbackReason(ctx context.Context, id uuid.UUID, reason string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE canary_rollouts SET rollback_reason = $2, updated_at = NOW() WHERE id = $1`,
		id, nullableString(reason))
	return err
}

// -------------------------------------------------------------------------
// Internals
// -------------------------------------------------------------------------

const canarySelectColumns = `
	SELECT id, service_id, environment_id,
	       stable_deployment_id, canary_deployment_id, new_stable_deployment_id,
	       canary_digest, canary_percentage,
	       total_replicas, canary_replicas, stable_replicas,
	       validation_window_seconds, COALESCE(smoke_endpoint, '') AS smoke_endpoint,
	       error_rate_threshold,
	       state, started_at, validating_started_at, promoting_started_at, terminal_at,
	       initiated_by, COALESCE(change_ticket_url, '') AS change_ticket_url,
	       COALESCE(last_error, '') AS last_error, COALESCE(rollback_reason, '') AS rollback_reason,
	       created_at, updated_at
	FROM canary_rollouts
`

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanCanaryRollout(row rowScanner) (*types.CanaryRollout, error) {
	ro := &types.CanaryRollout{}
	err := row.Scan(
		&ro.ID, &ro.ServiceID, &ro.EnvironmentID,
		&ro.StableDeploymentID, &ro.CanaryDeploymentID, &ro.NewStableDeploymentID,
		&ro.CanaryDigest, &ro.CanaryPercentage,
		&ro.TotalReplicas, &ro.CanaryReplicas, &ro.StableReplicas,
		&ro.ValidationWindowSeconds, &ro.SmokeEndpoint, &ro.ErrorRateThreshold,
		&ro.State, &ro.StartedAt, &ro.ValidatingStartedAt, &ro.PromotingStartedAt, &ro.TerminalAt,
		&ro.InitiatedBy, &ro.ChangeTicketURL,
		&ro.LastError, &ro.RollbackReason,
		&ro.CreatedAt, &ro.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return ro, nil
}

func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// Ensure the sql.Row type satisfies rowScanner (compile-time check).
var _ rowScanner = (*sql.Row)(nil)
