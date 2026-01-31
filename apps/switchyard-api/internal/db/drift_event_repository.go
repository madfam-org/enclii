package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

type DriftEventRepository struct {
	db DBTX
}

func NewDriftEventRepository(db DBTX) *DriftEventRepository {
	return &DriftEventRepository{db: db}
}

func NewDriftEventRepositoryWithTx(tx DBTX) *DriftEventRepository {
	return &DriftEventRepository{db: tx}
}

func (r *DriftEventRepository) Create(ctx context.Context, de *types.DriftEvent) error {
	de.ID = uuid.New()
	query := `INSERT INTO drift_events (id, source, resource_type, resource_name, cluster_id, drift_details, severity)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING detected_at, created_at`
	return r.db.QueryRowContext(ctx, query,
		de.ID, de.Source, de.ResourceType, de.ResourceName, de.ClusterID, de.DriftDetails, de.Severity,
	).Scan(&de.DetectedAt, &de.CreatedAt)
}

func (r *DriftEventRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.DriftEvent, error) {
	query := `SELECT id, source, resource_type, resource_name, cluster_id, drift_details, severity, resolved, resolved_at, detected_at, created_at
		FROM drift_events WHERE id = $1`
	de := &types.DriftEvent{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&de.ID, &de.Source, &de.ResourceType, &de.ResourceName, &de.ClusterID, &de.DriftDetails, &de.Severity, &de.Resolved, &de.ResolvedAt, &de.DetectedAt, &de.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get drift event: %w", err)
	}
	return de, nil
}

func (r *DriftEventRepository) List(ctx context.Context, resolved *bool) ([]*types.DriftEvent, error) {
	query := `SELECT id, source, resource_type, resource_name, cluster_id, drift_details, severity, resolved, resolved_at, detected_at, created_at
		FROM drift_events WHERE ($1::boolean IS NULL OR resolved = $1) ORDER BY detected_at DESC`
	rows, err := r.db.QueryContext(ctx, query, resolved)
	if err != nil {
		return nil, fmt.Errorf("failed to list drift events: %w", err)
	}
	defer rows.Close()
	var events []*types.DriftEvent
	for rows.Next() {
		de := &types.DriftEvent{}
		if err := rows.Scan(&de.ID, &de.Source, &de.ResourceType, &de.ResourceName, &de.ClusterID, &de.DriftDetails, &de.Severity, &de.Resolved, &de.ResolvedAt, &de.DetectedAt, &de.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan drift event: %w", err)
		}
		events = append(events, de)
	}
	return events, nil
}

func (r *DriftEventRepository) Resolve(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `UPDATE drift_events SET resolved=true, resolved_at=NOW() WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("failed to resolve drift event: %w", err)
	}
	return nil
}
