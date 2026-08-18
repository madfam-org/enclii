package db

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// DataAPIRepository is CRUD for managed_db_data_apis — the per-addon data-API
// (PostgREST) state. One row per addon that has the data-API enabled.
// See docs/architecture/data-api-postgrest.md.
type DataAPIRepository struct {
	db DBTX
}

// NewDataAPIRepository creates a data-API repository.
func NewDataAPIRepository(db DBTX) *DataAPIRepository {
	return &DataAPIRepository{db: db}
}

// NewDataAPIRepositoryWithTx creates a transaction-scoped repository.
func NewDataAPIRepositoryWithTx(tx DBTX) *DataAPIRepository {
	return &DataAPIRepository{db: tx}
}

const dataAPIColumns = `
	addon_id, project_id, status, status_message, schemas, anon_role, db_pool,
	jwt_secret_name, host, k8s_resource_name,
	created_at, updated_at, enabled_at, disabled_at
`

// Upsert inserts a new data-API row or updates the mutable fields of an existing
// one (keyed on addon_id). Used by enable — a re-enable of a previously-disabled
// data-API reuses the same row.
func (r *DataAPIRepository) Upsert(ctx context.Context, d *types.DataAPI) error {
	now := time.Now()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	d.UpdatedAt = now

	query := `
		INSERT INTO managed_db_data_apis (
			addon_id, project_id, status, status_message, schemas, anon_role, db_pool,
			jwt_secret_name, host, k8s_resource_name,
			created_at, updated_at, enabled_at, disabled_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (addon_id) DO UPDATE SET
			project_id        = EXCLUDED.project_id,
			status            = EXCLUDED.status,
			status_message    = EXCLUDED.status_message,
			schemas           = EXCLUDED.schemas,
			anon_role         = EXCLUDED.anon_role,
			db_pool           = EXCLUDED.db_pool,
			jwt_secret_name   = EXCLUDED.jwt_secret_name,
			host              = EXCLUDED.host,
			k8s_resource_name = EXCLUDED.k8s_resource_name,
			updated_at        = EXCLUDED.updated_at,
			enabled_at        = EXCLUDED.enabled_at,
			disabled_at       = EXCLUDED.disabled_at
	`
	_, err := r.db.ExecContext(ctx, query,
		d.AddonID, d.ProjectID, d.Status, d.StatusMessage, d.Schemas, d.AnonRole, d.DBPool,
		d.JWTSecretName, d.Host, d.K8sResourceName,
		d.CreatedAt, d.UpdatedAt, d.EnabledAt, d.DisabledAt,
	)
	return err
}

// GetByAddon returns the data-API row for an addon, or sql.ErrNoRows if the
// addon has no data-API.
func (r *DataAPIRepository) GetByAddon(ctx context.Context, addonID uuid.UUID) (*types.DataAPI, error) {
	query := `SELECT ` + dataAPIColumns + ` FROM managed_db_data_apis WHERE addon_id = $1`
	row := r.db.QueryRowContext(ctx, query, addonID)
	return scanDataAPI(row)
}

// UpdateStatus sets just the status + message (used by the reconciler on each
// transition).
func (r *DataAPIRepository) UpdateStatus(ctx context.Context, addonID uuid.UUID, status types.DataAPIStatus, message string) error {
	query := `
		UPDATE managed_db_data_apis
		SET status = $1, status_message = $2, updated_at = $3
		WHERE addon_id = $4
	`
	result, err := r.db.ExecContext(ctx, query, status, message, time.Now(), addonID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// MarkDisabled flips a row to disabled and stamps disabled_at. Idempotent.
func (r *DataAPIRepository) MarkDisabled(ctx context.Context, addonID uuid.UUID) error {
	now := time.Now()
	query := `
		UPDATE managed_db_data_apis
		SET status = $1, disabled_at = $2, updated_at = $3
		WHERE addon_id = $4
	`
	result, err := r.db.ExecContext(ctx, query, types.DataAPIStatusDisabled, now, now, addonID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListReconcilable returns rows in a non-terminal state (pending/provisioning/
// disabling) for the reconciler loop.
func (r *DataAPIRepository) ListReconcilable(ctx context.Context) ([]*types.DataAPI, error) {
	query := `
		SELECT ` + dataAPIColumns + `
		FROM managed_db_data_apis
		WHERE status IN ('pending', 'provisioning', 'disabling')
		ORDER BY created_at ASC
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []*types.DataAPI
	for rows.Next() {
		d, err := scanDataAPIRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ListByProject returns every data-API row for a project (all statuses).
func (r *DataAPIRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]*types.DataAPI, error) {
	query := `
		SELECT ` + dataAPIColumns + `
		FROM managed_db_data_apis
		WHERE project_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []*types.DataAPI
	for rows.Next() {
		d, err := scanDataAPIRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Delete hard-deletes a data-API row. Rarely used — disable keeps the row for
// audit; this exists for addon teardown / GC.
func (r *DataAPIRepository) Delete(ctx context.Context, addonID uuid.UUID) error {
	query := `DELETE FROM managed_db_data_apis WHERE addon_id = $1`
	result, err := r.db.ExecContext(ctx, query, addonID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type dataAPIScanner interface {
	Scan(dest ...interface{}) error
}

func scanDataAPI(s dataAPIScanner) (*types.DataAPI, error) {
	d := &types.DataAPI{}
	var statusMsg, jwtSecret, host, k8sRes sql.NullString
	var enabledAt, disabledAt sql.NullTime

	if err := s.Scan(
		&d.AddonID, &d.ProjectID, &d.Status, &statusMsg, &d.Schemas, &d.AnonRole, &d.DBPool,
		&jwtSecret, &host, &k8sRes,
		&d.CreatedAt, &d.UpdatedAt, &enabledAt, &disabledAt,
	); err != nil {
		return nil, err
	}

	if statusMsg.Valid {
		d.StatusMessage = statusMsg.String
	}
	if jwtSecret.Valid {
		d.JWTSecretName = jwtSecret.String
	}
	if host.Valid {
		d.Host = host.String
	}
	if k8sRes.Valid {
		d.K8sResourceName = k8sRes.String
	}
	if enabledAt.Valid {
		d.EnabledAt = &enabledAt.Time
	}
	if disabledAt.Valid {
		d.DisabledAt = &disabledAt.Time
	}
	return d, nil
}

func scanDataAPIRows(rows *sql.Rows) (*types.DataAPI, error) {
	return scanDataAPI(rows)
}
