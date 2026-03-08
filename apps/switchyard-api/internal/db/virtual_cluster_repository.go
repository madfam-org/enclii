package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

type VirtualClusterRepository struct {
	db DBTX
}

func NewVirtualClusterRepository(db DBTX) *VirtualClusterRepository {
	return &VirtualClusterRepository{db: db}
}

func NewVirtualClusterRepositoryWithTx(tx DBTX) *VirtualClusterRepository {
	return &VirtualClusterRepository{db: tx}
}

func (r *VirtualClusterRepository) Create(ctx context.Context, vc *types.VirtualCluster) error {
	vc.ID = uuid.New()
	query := `INSERT INTO virtual_clusters (id, name, host_cluster_id, tenant_id, namespace, k8s_version, status, helm_release_name, resource_quota)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING created_at, updated_at`
	rq, _ := json.Marshal(vc.ResourceQuota)
	return r.db.QueryRowContext(ctx, query,
		vc.ID, vc.Name, vc.HostClusterID, vc.TenantID, vc.Namespace, vc.K8sVersion, vc.Status, vc.HelmReleaseName, rq,
	).Scan(&vc.CreatedAt, &vc.UpdatedAt)
}

func (r *VirtualClusterRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.VirtualCluster, error) {
	query := `SELECT id, name, host_cluster_id, tenant_id, namespace, k8s_version, status, helm_release_name, resource_quota, created_at, updated_at
		FROM virtual_clusters WHERE id = $1`
	vc := &types.VirtualCluster{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&vc.ID, &vc.Name, &vc.HostClusterID, &vc.TenantID, &vc.Namespace, &vc.K8sVersion, &vc.Status, &vc.HelmReleaseName, &vc.ResourceQuota, &vc.CreatedAt, &vc.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get virtual cluster: %w", err)
	}
	return vc, nil
}

func (r *VirtualClusterRepository) List(ctx context.Context) ([]*types.VirtualCluster, error) {
	query := `SELECT id, name, host_cluster_id, tenant_id, namespace, k8s_version, status, helm_release_name, resource_quota, created_at, updated_at
		FROM virtual_clusters ORDER BY name`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list virtual clusters: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var vcs []*types.VirtualCluster
	for rows.Next() {
		vc := &types.VirtualCluster{}
		if err := rows.Scan(&vc.ID, &vc.Name, &vc.HostClusterID, &vc.TenantID, &vc.Namespace, &vc.K8sVersion, &vc.Status, &vc.HelmReleaseName, &vc.ResourceQuota, &vc.CreatedAt, &vc.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan virtual cluster: %w", err)
		}
		vcs = append(vcs, vc)
	}
	return vcs, nil
}

func (r *VirtualClusterRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status types.VClusterStatus) error {
	_, err := r.db.ExecContext(ctx, `UPDATE virtual_clusters SET status=$2 WHERE id=$1`, id, status)
	if err != nil {
		return fmt.Errorf("failed to update virtual cluster status: %w", err)
	}
	return nil
}

func (r *VirtualClusterRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM virtual_clusters WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete virtual cluster: %w", err)
	}
	return nil
}
