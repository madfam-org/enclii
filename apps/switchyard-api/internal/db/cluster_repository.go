package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

type ClusterRepository struct {
	db DBTX
}

func NewClusterRepository(db DBTX) *ClusterRepository {
	return &ClusterRepository{db: db}
}

func NewClusterRepositoryWithTx(tx DBTX) *ClusterRepository {
	return &ClusterRepository{db: tx}
}

func (r *ClusterRepository) Create(ctx context.Context, c *types.Cluster) error {
	c.ID = uuid.New()
	query := `INSERT INTO clusters (id, name, slug, type, endpoint, kubeconfig_secret_ref, region, status, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING created_at, updated_at`
	metadata, _ := json.Marshal(c.Metadata)
	return r.db.QueryRowContext(ctx, query,
		c.ID, c.Name, c.Slug, c.Type, c.Endpoint, c.KubeconfigSecretRef, c.Region, c.Status, metadata,
	).Scan(&c.CreatedAt, &c.UpdatedAt)
}

func (r *ClusterRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.Cluster, error) {
	query := `SELECT id, name, slug, type, endpoint, kubeconfig_secret_ref, region, status, metadata, created_at, updated_at
		FROM clusters WHERE id = $1`
	c := &types.Cluster{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&c.ID, &c.Name, &c.Slug, &c.Type, &c.Endpoint, &c.KubeconfigSecretRef, &c.Region, &c.Status, &c.Metadata, &c.CreatedAt, &c.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster: %w", err)
	}
	return c, nil
}

func (r *ClusterRepository) List(ctx context.Context) ([]*types.Cluster, error) {
	query := `SELECT id, name, slug, type, endpoint, kubeconfig_secret_ref, region, status, metadata, created_at, updated_at
		FROM clusters ORDER BY name`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list clusters: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var clusters []*types.Cluster
	for rows.Next() {
		c := &types.Cluster{}
		if err := rows.Scan(&c.ID, &c.Name, &c.Slug, &c.Type, &c.Endpoint, &c.KubeconfigSecretRef, &c.Region, &c.Status, &c.Metadata, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan cluster: %w", err)
		}
		clusters = append(clusters, c)
	}
	return clusters, nil
}

func (r *ClusterRepository) Update(ctx context.Context, c *types.Cluster) error {
	query := `UPDATE clusters SET name=$2, slug=$3, type=$4, endpoint=$5, kubeconfig_secret_ref=$6, region=$7, status=$8, metadata=$9
		WHERE id=$1 RETURNING updated_at`
	metadata, _ := json.Marshal(c.Metadata)
	return r.db.QueryRowContext(ctx, query,
		c.ID, c.Name, c.Slug, c.Type, c.Endpoint, c.KubeconfigSecretRef, c.Region, c.Status, metadata,
	).Scan(&c.UpdatedAt)
}

func (r *ClusterRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM clusters WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete cluster: %w", err)
	}
	return nil
}
