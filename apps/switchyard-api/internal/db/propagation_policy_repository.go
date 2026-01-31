package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

type PropagationPolicyRepository struct {
	db DBTX
}

func NewPropagationPolicyRepository(db DBTX) *PropagationPolicyRepository {
	return &PropagationPolicyRepository{db: db}
}

func NewPropagationPolicyRepositoryWithTx(tx DBTX) *PropagationPolicyRepository {
	return &PropagationPolicyRepository{db: tx}
}

func (r *PropagationPolicyRepository) Create(ctx context.Context, pp *types.PropagationPolicy) error {
	pp.ID = uuid.New()
	query := `INSERT INTO propagation_policies (id, name, cluster_ids, resource_selectors, placement_strategy, gpu_required, priority)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING created_at, updated_at`
	rs, _ := json.Marshal(pp.ResourceSelectors)
	clusterIDs := make([]string, len(pp.ClusterIDs))
	for i, id := range pp.ClusterIDs {
		clusterIDs[i] = id.String()
	}
	return r.db.QueryRowContext(ctx, query,
		pp.ID, pp.Name, pq.Array(clusterIDs), rs, pp.PlacementStrategy, pp.GPURequired, pp.Priority,
	).Scan(&pp.CreatedAt, &pp.UpdatedAt)
}

func (r *PropagationPolicyRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.PropagationPolicy, error) {
	query := `SELECT id, name, cluster_ids, resource_selectors, placement_strategy, gpu_required, priority, created_at, updated_at
		FROM propagation_policies WHERE id = $1`
	pp := &types.PropagationPolicy{}
	var clusterIDs []string
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&pp.ID, &pp.Name, pq.Array(&clusterIDs), &pp.ResourceSelectors, &pp.PlacementStrategy, &pp.GPURequired, &pp.Priority, &pp.CreatedAt, &pp.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get propagation policy: %w", err)
	}
	pp.ClusterIDs = make([]uuid.UUID, len(clusterIDs))
	for i, s := range clusterIDs {
		parsed, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("invalid cluster ID %q: %w", s, err)
		}
		pp.ClusterIDs[i] = parsed
	}
	return pp, nil
}

func (r *PropagationPolicyRepository) List(ctx context.Context) ([]*types.PropagationPolicy, error) {
	query := `SELECT id, name, cluster_ids, resource_selectors, placement_strategy, gpu_required, priority, created_at, updated_at
		FROM propagation_policies ORDER BY priority DESC, name`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list propagation policies: %w", err)
	}
	defer rows.Close()
	var policies []*types.PropagationPolicy
	for rows.Next() {
		pp := &types.PropagationPolicy{}
		var clusterIDs []string
		if err := rows.Scan(&pp.ID, &pp.Name, pq.Array(&clusterIDs), &pp.ResourceSelectors, &pp.PlacementStrategy, &pp.GPURequired, &pp.Priority, &pp.CreatedAt, &pp.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan propagation policy: %w", err)
		}
		pp.ClusterIDs = make([]uuid.UUID, len(clusterIDs))
		for i, s := range clusterIDs {
			parsed, err := uuid.Parse(s)
			if err != nil {
				return nil, fmt.Errorf("invalid cluster ID %q: %w", s, err)
			}
			pp.ClusterIDs[i] = parsed
		}
		policies = append(policies, pp)
	}
	return policies, nil
}

func (r *PropagationPolicyRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM propagation_policies WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete propagation policy: %w", err)
	}
	return nil
}
