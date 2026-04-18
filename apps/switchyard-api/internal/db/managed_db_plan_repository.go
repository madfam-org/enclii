package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ManagedDBPlan represents a row in managed_db_plans.
//
// The plan catalog is a server-side enforced enum: CLI submits a `plan` code,
// the API resolves it via this repository before any provisioning touches K8s.
// Sprint 3 billing reads PriceCentsMonth from here as the per-addon rate card.
type ManagedDBPlan struct {
	Code            string    `json:"code"`
	Engine          string    `json:"engine"`
	DisplayName     string    `json:"display_name"`
	Tier            string    `json:"tier"`
	StorageGB       int       `json:"storage_gb"`
	CPURequest      string    `json:"cpu_request"`
	MemoryRequest   string    `json:"memory_request"`
	MaxConnections  int       `json:"max_connections"`
	HAEnabled       bool      `json:"ha_enabled"`
	ReplicaCount    int       `json:"replica_count"`
	Available       bool      `json:"available"`
	PriceCentsMonth int64     `json:"price_cents_month"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// ManagedDBPlanRepository handles read access to the plan catalog.
// Plans are operationally managed (seeded by migration, edited directly in
// production DB); there is no programmatic Create/Update surface in Sprint 1.
type ManagedDBPlanRepository struct {
	db DBTX
}

// NewManagedDBPlanRepository creates a plan catalog reader.
func NewManagedDBPlanRepository(db DBTX) *ManagedDBPlanRepository {
	return &ManagedDBPlanRepository{db: db}
}

// NewManagedDBPlanRepositoryWithTx creates a transaction-scoped reader.
func NewManagedDBPlanRepositoryWithTx(tx DBTX) *ManagedDBPlanRepository {
	return &ManagedDBPlanRepository{db: tx}
}

const managedDBPlanColumns = `
	code, engine, display_name, tier, storage_gb,
	cpu_request, memory_request, max_connections,
	ha_enabled, replica_count, available,
	price_cents_month, created_at, updated_at
`

// GetByCode returns a plan by its code (e.g. "standard-0").
// Returns sql.ErrNoRows if no such plan exists. Callers should treat
// that error as "unknown plan" and surface a 400 to the customer.
func (r *ManagedDBPlanRepository) GetByCode(ctx context.Context, code string) (*ManagedDBPlan, error) {
	if code == "" {
		return nil, errors.New("plan code is required")
	}

	query := `SELECT ` + managedDBPlanColumns + ` FROM managed_db_plans WHERE code = $1`

	plan := &ManagedDBPlan{}
	err := r.db.QueryRowContext(ctx, query, code).Scan(
		&plan.Code, &plan.Engine, &plan.DisplayName, &plan.Tier, &plan.StorageGB,
		&plan.CPURequest, &plan.MemoryRequest, &plan.MaxConnections,
		&plan.HAEnabled, &plan.ReplicaCount, &plan.Available,
		&plan.PriceCentsMonth, &plan.CreatedAt, &plan.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return plan, nil
}

// ListAvailable returns plans where available=true, for the given engine.
// If engine is empty, returns all available plans across engines.
// Results ordered by storage_gb ascending — "smallest first" matches how
// pricing pages typically render.
func (r *ManagedDBPlanRepository) ListAvailable(ctx context.Context, engine string) ([]*ManagedDBPlan, error) {
	var (
		rows *sql.Rows
		err  error
	)

	if engine == "" {
		query := `
			SELECT ` + managedDBPlanColumns + `
			FROM managed_db_plans
			WHERE available = true
			ORDER BY engine, storage_gb`
		rows, err = r.db.QueryContext(ctx, query)
	} else {
		query := `
			SELECT ` + managedDBPlanColumns + `
			FROM managed_db_plans
			WHERE available = true AND engine = $1
			ORDER BY storage_gb`
		rows, err = r.db.QueryContext(ctx, query, engine)
	}

	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return scanPlans(rows)
}

// ListAll returns every plan, including unavailable ones, for admin tooling.
func (r *ManagedDBPlanRepository) ListAll(ctx context.Context) ([]*ManagedDBPlan, error) {
	query := `SELECT ` + managedDBPlanColumns + ` FROM managed_db_plans ORDER BY engine, storage_gb`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return scanPlans(rows)
}

func scanPlans(rows *sql.Rows) ([]*ManagedDBPlan, error) {
	plans := []*ManagedDBPlan{}
	for rows.Next() {
		plan := &ManagedDBPlan{}
		if err := rows.Scan(
			&plan.Code, &plan.Engine, &plan.DisplayName, &plan.Tier, &plan.StorageGB,
			&plan.CPURequest, &plan.MemoryRequest, &plan.MaxConnections,
			&plan.HAEnabled, &plan.ReplicaCount, &plan.Available,
			&plan.PriceCentsMonth, &plan.CreatedAt, &plan.UpdatedAt,
		); err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	return plans, rows.Err()
}
