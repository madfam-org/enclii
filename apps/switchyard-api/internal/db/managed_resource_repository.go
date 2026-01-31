package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

type ManagedResourceRepository struct {
	db DBTX
}

func NewManagedResourceRepository(db DBTX) *ManagedResourceRepository {
	return &ManagedResourceRepository{db: db}
}

func NewManagedResourceRepositoryWithTx(tx DBTX) *ManagedResourceRepository {
	return &ManagedResourceRepository{db: tx}
}

func (r *ManagedResourceRepository) Create(ctx context.Context, mr *types.ManagedResource) error {
	mr.ID = uuid.New()
	query := `INSERT INTO managed_resources (id, name, api_version, kind, provider, cluster_id, management_policy, sync_status, conditions, spec_hash, metadata)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING created_at, updated_at`
	cond, _ := json.Marshal(mr.Conditions)
	meta, _ := json.Marshal(mr.Metadata)
	return r.db.QueryRowContext(ctx, query,
		mr.ID, mr.Name, mr.APIVersion, mr.Kind, mr.Provider, mr.ClusterID, mr.ManagementPolicy, mr.SyncStatus, cond, mr.SpecHash, meta,
	).Scan(&mr.CreatedAt, &mr.UpdatedAt)
}

func (r *ManagedResourceRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.ManagedResource, error) {
	query := `SELECT id, name, api_version, kind, provider, cluster_id, management_policy, sync_status, conditions, spec_hash, metadata, created_at, updated_at
		FROM managed_resources WHERE id = $1`
	mr := &types.ManagedResource{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&mr.ID, &mr.Name, &mr.APIVersion, &mr.Kind, &mr.Provider, &mr.ClusterID, &mr.ManagementPolicy, &mr.SyncStatus, &mr.Conditions, &mr.SpecHash, &mr.Metadata, &mr.CreatedAt, &mr.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get managed resource: %w", err)
	}
	return mr, nil
}

func (r *ManagedResourceRepository) List(ctx context.Context, provider, kind string, status types.SyncStatus) ([]*types.ManagedResource, error) {
	query := `SELECT id, name, api_version, kind, provider, cluster_id, management_policy, sync_status, conditions, spec_hash, metadata, created_at, updated_at
		FROM managed_resources WHERE ($1 = '' OR provider = $1) AND ($2 = '' OR kind = $2) AND ($3 = '' OR sync_status = $3) ORDER BY name`
	rows, err := r.db.QueryContext(ctx, query, provider, kind, string(status))
	if err != nil {
		return nil, fmt.Errorf("failed to list managed resources: %w", err)
	}
	defer rows.Close()
	var resources []*types.ManagedResource
	for rows.Next() {
		mr := &types.ManagedResource{}
		if err := rows.Scan(&mr.ID, &mr.Name, &mr.APIVersion, &mr.Kind, &mr.Provider, &mr.ClusterID, &mr.ManagementPolicy, &mr.SyncStatus, &mr.Conditions, &mr.SpecHash, &mr.Metadata, &mr.CreatedAt, &mr.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan managed resource: %w", err)
		}
		resources = append(resources, mr)
	}
	return resources, nil
}

func (r *ManagedResourceRepository) UpdateSyncStatus(ctx context.Context, id uuid.UUID, status types.SyncStatus, conditions json.RawMessage) error {
	_, err := r.db.ExecContext(ctx, `UPDATE managed_resources SET sync_status=$2, conditions=$3 WHERE id=$1`, id, status, conditions)
	if err != nil {
		return fmt.Errorf("failed to update sync status: %w", err)
	}
	return nil
}

func (r *ManagedResourceRepository) UpdatePolicy(ctx context.Context, id uuid.UUID, policy types.ManagementPolicy) error {
	_, err := r.db.ExecContext(ctx, `UPDATE managed_resources SET management_policy=$2 WHERE id=$1`, id, policy)
	if err != nil {
		return fmt.Errorf("failed to update management policy: %w", err)
	}
	return nil
}

func (r *ManagedResourceRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM managed_resources WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete managed resource: %w", err)
	}
	return nil
}
