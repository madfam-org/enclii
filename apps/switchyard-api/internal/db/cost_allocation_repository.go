package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

type CostAllocationRepository struct {
	db DBTX
}

func NewCostAllocationRepository(db DBTX) *CostAllocationRepository {
	return &CostAllocationRepository{db: db}
}

func NewCostAllocationRepositoryWithTx(tx DBTX) *CostAllocationRepository {
	return &CostAllocationRepository{db: tx}
}

func (r *CostAllocationRepository) Create(ctx context.Context, ca *types.CostAllocation) error {
	ca.ID = uuid.New()
	query := `INSERT INTO cost_allocations (id, bare_metal_host_id, tenant_id, allocation_percent, period_start, period_end, cost_cents)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING created_at`
	return r.db.QueryRowContext(ctx, query,
		ca.ID, ca.BareMetalHostID, ca.TenantID, ca.AllocationPercent, ca.PeriodStart, ca.PeriodEnd, ca.CostCents,
	).Scan(&ca.CreatedAt)
}

func (r *CostAllocationRepository) ListByTenant(ctx context.Context, tenantID string, start, end time.Time) ([]*types.CostAllocation, error) {
	query := `SELECT id, bare_metal_host_id, tenant_id, allocation_percent, period_start, period_end, cost_cents, created_at
		FROM cost_allocations WHERE tenant_id=$1 AND period_start >= $2 AND period_end <= $3 ORDER BY period_start`
	rows, err := r.db.QueryContext(ctx, query, tenantID, start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to list cost allocations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var allocations []*types.CostAllocation
	for rows.Next() {
		ca := &types.CostAllocation{}
		if err := rows.Scan(&ca.ID, &ca.BareMetalHostID, &ca.TenantID, &ca.AllocationPercent, &ca.PeriodStart, &ca.PeriodEnd, &ca.CostCents, &ca.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan cost allocation: %w", err)
		}
		allocations = append(allocations, ca)
	}
	return allocations, nil
}

func (r *CostAllocationRepository) ListByHost(ctx context.Context, hostID uuid.UUID, start, end time.Time) ([]*types.CostAllocation, error) {
	query := `SELECT id, bare_metal_host_id, tenant_id, allocation_percent, period_start, period_end, cost_cents, created_at
		FROM cost_allocations WHERE bare_metal_host_id=$1 AND period_start >= $2 AND period_end <= $3 ORDER BY period_start`
	rows, err := r.db.QueryContext(ctx, query, hostID, start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to list cost allocations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var allocations []*types.CostAllocation
	for rows.Next() {
		ca := &types.CostAllocation{}
		if err := rows.Scan(&ca.ID, &ca.BareMetalHostID, &ca.TenantID, &ca.AllocationPercent, &ca.PeriodStart, &ca.PeriodEnd, &ca.CostCents, &ca.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan cost allocation: %w", err)
		}
		allocations = append(allocations, ca)
	}
	return allocations, nil
}

func (r *CostAllocationRepository) UpdateCostCents(ctx context.Context, id uuid.UUID, costCents int) error {
	_, err := r.db.ExecContext(ctx, `UPDATE cost_allocations SET cost_cents=$2 WHERE id=$1`, id, costCents)
	if err != nil {
		return fmt.Errorf("failed to update cost allocation: %w", err)
	}
	return nil
}

func (r *CostAllocationRepository) GetSummary(ctx context.Context, start, end time.Time) ([]*types.CostAllocation, error) {
	query := `SELECT '' AS id, bare_metal_host_id, tenant_id, SUM(allocation_percent) as allocation_percent, MIN(period_start) as period_start, MAX(period_end) as period_end, SUM(cost_cents) as cost_cents, MIN(created_at) as created_at
		FROM cost_allocations WHERE period_start >= $1 AND period_end <= $2
		GROUP BY bare_metal_host_id, tenant_id ORDER BY tenant_id`
	rows, err := r.db.QueryContext(ctx, query, start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to get cost summary: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var allocations []*types.CostAllocation
	for rows.Next() {
		ca := &types.CostAllocation{}
		var idStr string
		if err := rows.Scan(&idStr, &ca.BareMetalHostID, &ca.TenantID, &ca.AllocationPercent, &ca.PeriodStart, &ca.PeriodEnd, &ca.CostCents, &ca.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan cost summary: %w", err)
		}
		allocations = append(allocations, ca)
	}
	return allocations, nil
}
