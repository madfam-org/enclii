package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// FunctionRepository handles serverless function CRUD operations
type FunctionRepository struct {
	db DBTX
}

// NewFunctionRepository creates a new function repository
func NewFunctionRepository(db DBTX) *FunctionRepository {
	return &FunctionRepository{db: db}
}

// NewFunctionRepositoryWithTx creates a repository using a transaction
func NewFunctionRepositoryWithTx(tx DBTX) *FunctionRepository {
	return &FunctionRepository{db: tx}
}

// Create creates a new serverless function
func (r *FunctionRepository) Create(ctx context.Context, fn *types.Function) error {
	fn.ID = uuid.New()
	fn.CreatedAt = time.Now()
	fn.UpdatedAt = time.Now()
	fn.Status = types.FunctionStatusPending

	// Apply default config values
	r.applyDefaults(&fn.Config)

	configJSON, err := json.Marshal(fn.Config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	query := `
		INSERT INTO functions (
			id, project_id, name, config, status, status_message,
			k8s_namespace, k8s_resource_name, image_uri, endpoint,
			available_replicas, invocation_count, avg_duration_ms, last_invoked_at,
			created_by, created_by_email, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
	`
	_, err = r.db.ExecContext(ctx, query,
		fn.ID, fn.ProjectID, fn.Name, configJSON, fn.Status, fn.StatusMessage,
		fn.K8sNamespace, fn.K8sResourceName, fn.ImageURI, fn.Endpoint,
		fn.AvailableReplicas, fn.InvocationCount, fn.AvgDurationMs, fn.LastInvokedAt,
		fn.CreatedBy, fn.CreatedByEmail, fn.CreatedAt, fn.UpdatedAt,
	)
	return err
}

// applyDefaults applies default values to function config
func (r *FunctionRepository) applyDefaults(config *types.FunctionConfig) {
	if config.Memory == "" {
		config.Memory = types.FunctionDefaults.Memory
	}
	if config.CPU == "" {
		config.CPU = types.FunctionDefaults.CPU
	}
	if config.Timeout == 0 {
		config.Timeout = types.FunctionDefaults.Timeout
	}
	if config.MaxReplicas == 0 {
		config.MaxReplicas = types.FunctionDefaults.MaxReplicas
	}
	if config.CooldownPeriod == 0 {
		config.CooldownPeriod = types.FunctionDefaults.CooldownPeriod
	}
	if config.Concurrency == 0 {
		config.Concurrency = types.FunctionDefaults.Concurrency
	}
	// MinReplicas defaults to 0 (scale-to-zero enabled)
	// Handler defaults are per-runtime and set by caller if needed
}

// GetByID retrieves a function by ID
func (r *FunctionRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.Function, error) {
	fn := &types.Function{}
	var configJSON []byte
	var createdBy sql.NullString
	var statusMsg, k8sNs, k8sRes, imageURI, endpoint, createdByEmail sql.NullString
	var lastInvokedAt, deployedAt, deletedAt sql.NullTime

	query := `
		SELECT id, project_id, name, config, status, status_message,
		       k8s_namespace, k8s_resource_name, image_uri, endpoint,
		       available_replicas, invocation_count, avg_duration_ms, last_invoked_at,
		       created_by, created_by_email, created_at, updated_at, deployed_at, deleted_at
		FROM functions WHERE id = $1 AND deleted_at IS NULL
	`

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&fn.ID, &fn.ProjectID, &fn.Name, &configJSON, &fn.Status, &statusMsg,
		&k8sNs, &k8sRes, &imageURI, &endpoint,
		&fn.AvailableReplicas, &fn.InvocationCount, &fn.AvgDurationMs, &lastInvokedAt,
		&createdBy, &createdByEmail, &fn.CreatedAt, &fn.UpdatedAt, &deployedAt, &deletedAt,
	)
	if err != nil {
		return nil, err
	}

	// Parse nullable fields
	r.parseNullableFields(fn, createdBy, statusMsg, k8sNs, k8sRes, imageURI, endpoint, createdByEmail, lastInvokedAt, deployedAt, deletedAt)

	// Parse config JSON
	if len(configJSON) > 0 {
		if err := json.Unmarshal(configJSON, &fn.Config); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config: %w", err)
		}
	}

	return fn, nil
}

// GetByName retrieves a function by project ID and name
func (r *FunctionRepository) GetByName(ctx context.Context, projectID uuid.UUID, name string) (*types.Function, error) {
	fn := &types.Function{}
	var configJSON []byte
	var createdBy sql.NullString
	var statusMsg, k8sNs, k8sRes, imageURI, endpoint, createdByEmail sql.NullString
	var lastInvokedAt, deployedAt, deletedAt sql.NullTime

	query := `
		SELECT id, project_id, name, config, status, status_message,
		       k8s_namespace, k8s_resource_name, image_uri, endpoint,
		       available_replicas, invocation_count, avg_duration_ms, last_invoked_at,
		       created_by, created_by_email, created_at, updated_at, deployed_at, deleted_at
		FROM functions
		WHERE project_id = $1 AND name = $2 AND deleted_at IS NULL
	`

	err := r.db.QueryRowContext(ctx, query, projectID, name).Scan(
		&fn.ID, &fn.ProjectID, &fn.Name, &configJSON, &fn.Status, &statusMsg,
		&k8sNs, &k8sRes, &imageURI, &endpoint,
		&fn.AvailableReplicas, &fn.InvocationCount, &fn.AvgDurationMs, &lastInvokedAt,
		&createdBy, &createdByEmail, &fn.CreatedAt, &fn.UpdatedAt, &deployedAt, &deletedAt,
	)
	if err != nil {
		return nil, err
	}

	// Parse nullable fields
	r.parseNullableFields(fn, createdBy, statusMsg, k8sNs, k8sRes, imageURI, endpoint, createdByEmail, lastInvokedAt, deployedAt, deletedAt)

	// Parse config JSON
	if len(configJSON) > 0 {
		if err := json.Unmarshal(configJSON, &fn.Config); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config: %w", err)
		}
	}

	return fn, nil
}

// ListByProject retrieves all functions for a project
func (r *FunctionRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]*types.Function, error) {
	query := `
		SELECT id, project_id, name, config, status, status_message,
		       k8s_namespace, k8s_resource_name, image_uri, endpoint,
		       available_replicas, invocation_count, avg_duration_ms, last_invoked_at,
		       created_by, created_by_email, created_at, updated_at, deployed_at, deleted_at
		FROM functions
		WHERE project_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return r.scanFunctions(rows)
}

// ListByProjects retrieves all functions for multiple projects
func (r *FunctionRepository) ListByProjects(ctx context.Context, projectIDs []uuid.UUID) ([]*types.Function, error) {
	if len(projectIDs) == 0 {
		return []*types.Function{}, nil
	}

	query := `
		SELECT id, project_id, name, config, status, status_message,
		       k8s_namespace, k8s_resource_name, image_uri, endpoint,
		       available_replicas, invocation_count, avg_duration_ms, last_invoked_at,
		       created_by, created_by_email, created_at, updated_at, deployed_at, deleted_at
		FROM functions
		WHERE project_id = ANY($1) AND deleted_at IS NULL
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(projectIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return r.scanFunctions(rows)
}

// ListByStatus retrieves all functions with a specific status (for reconciler)
func (r *FunctionRepository) ListByStatus(ctx context.Context, statuses ...types.FunctionStatus) ([]*types.Function, error) {
	if len(statuses) == 0 {
		return []*types.Function{}, nil
	}

	// Convert to string slice for query
	statusStrings := make([]string, len(statuses))
	for i, s := range statuses {
		statusStrings[i] = string(s)
	}

	query := `
		SELECT id, project_id, name, config, status, status_message,
		       k8s_namespace, k8s_resource_name, image_uri, endpoint,
		       available_replicas, invocation_count, avg_duration_ms, last_invoked_at,
		       created_by, created_by_email, created_at, updated_at, deployed_at, deleted_at
		FROM functions
		WHERE status = ANY($1) AND deleted_at IS NULL
		ORDER BY created_at ASC
	`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(statusStrings))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return r.scanFunctions(rows)
}

// ListPending retrieves all functions in pending/building/deploying state (for reconciler)
func (r *FunctionRepository) ListPending(ctx context.Context) ([]*types.Function, error) {
	return r.ListByStatus(ctx, types.FunctionStatusPending, types.FunctionStatusBuilding, types.FunctionStatusDeploying, types.FunctionStatusDeleting)
}

// scanFunctions scans multiple function rows
func (r *FunctionRepository) scanFunctions(rows *sql.Rows) ([]*types.Function, error) {
	var functions []*types.Function

	for rows.Next() {
		fn := &types.Function{}
		var configJSON []byte
		var createdBy sql.NullString
		var statusMsg, k8sNs, k8sRes, imageURI, endpoint, createdByEmail sql.NullString
		var lastInvokedAt, deployedAt, deletedAt sql.NullTime

		err := rows.Scan(
			&fn.ID, &fn.ProjectID, &fn.Name, &configJSON, &fn.Status, &statusMsg,
			&k8sNs, &k8sRes, &imageURI, &endpoint,
			&fn.AvailableReplicas, &fn.InvocationCount, &fn.AvgDurationMs, &lastInvokedAt,
			&createdBy, &createdByEmail, &fn.CreatedAt, &fn.UpdatedAt, &deployedAt, &deletedAt,
		)
		if err != nil {
			return nil, err
		}

		// Parse nullable fields
		r.parseNullableFields(fn, createdBy, statusMsg, k8sNs, k8sRes, imageURI, endpoint, createdByEmail, lastInvokedAt, deployedAt, deletedAt)

		// Parse config JSON
		if len(configJSON) > 0 {
			if err := json.Unmarshal(configJSON, &fn.Config); err != nil {
				return nil, fmt.Errorf("failed to unmarshal config: %w", err)
			}
		}

		functions = append(functions, fn)
	}

	return functions, nil
}

// parseNullableFields parses nullable SQL fields into function struct
func (r *FunctionRepository) parseNullableFields(
	fn *types.Function,
	createdBy, statusMsg, k8sNs, k8sRes, imageURI, endpoint, createdByEmail sql.NullString,
	lastInvokedAt, deployedAt, deletedAt sql.NullTime,
) {
	if createdBy.Valid {
		parsed, _ := uuid.Parse(createdBy.String)
		fn.CreatedBy = &parsed
	}
	if statusMsg.Valid {
		fn.StatusMessage = statusMsg.String
	}
	if k8sNs.Valid {
		fn.K8sNamespace = k8sNs.String
	}
	if k8sRes.Valid {
		fn.K8sResourceName = k8sRes.String
	}
	if imageURI.Valid {
		fn.ImageURI = imageURI.String
	}
	if endpoint.Valid {
		fn.Endpoint = endpoint.String
	}
	if createdByEmail.Valid {
		fn.CreatedByEmail = createdByEmail.String
	}
	if lastInvokedAt.Valid {
		fn.LastInvokedAt = &lastInvokedAt.Time
	}
	if deployedAt.Valid {
		fn.DeployedAt = &deployedAt.Time
	}
	if deletedAt.Valid {
		fn.DeletedAt = &deletedAt.Time
	}
}

// Update updates a function
func (r *FunctionRepository) Update(ctx context.Context, fn *types.Function) error {
	fn.UpdatedAt = time.Now()

	configJSON, err := json.Marshal(fn.Config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	query := `
		UPDATE functions
		SET config = $1, status = $2, status_message = $3,
		    k8s_namespace = $4, k8s_resource_name = $5, image_uri = $6, endpoint = $7,
		    available_replicas = $8, invocation_count = $9, avg_duration_ms = $10, last_invoked_at = $11,
		    updated_at = $12, deployed_at = $13
		WHERE id = $14
	`
	result, err := r.db.ExecContext(ctx, query,
		configJSON, fn.Status, fn.StatusMessage,
		fn.K8sNamespace, fn.K8sResourceName, fn.ImageURI, fn.Endpoint,
		fn.AvailableReplicas, fn.InvocationCount, fn.AvgDurationMs, fn.LastInvokedAt,
		fn.UpdatedAt, fn.DeployedAt,
		fn.ID,
	)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// UpdateStatus updates just the status of a function
func (r *FunctionRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status types.FunctionStatus, message string) error {
	query := `
		UPDATE functions
		SET status = $1, status_message = $2, updated_at = $3
		WHERE id = $4
	`
	result, err := r.db.ExecContext(ctx, query, status, message, time.Now(), id)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// MarkDeployed marks a function as ready with deployment info
func (r *FunctionRepository) MarkDeployed(ctx context.Context, id uuid.UUID, imageURI, endpoint, k8sNamespace, k8sResourceName string) error {
	now := time.Now()
	query := `
		UPDATE functions
		SET status = $1, image_uri = $2, endpoint = $3, k8s_namespace = $4, k8s_resource_name = $5,
		    updated_at = $6, deployed_at = $7
		WHERE id = $8
	`
	result, err := r.db.ExecContext(ctx, query,
		types.FunctionStatusReady, imageURI, endpoint, k8sNamespace, k8sResourceName, now, now, id,
	)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// UpdateReplicas updates the available replicas count
func (r *FunctionRepository) UpdateReplicas(ctx context.Context, id uuid.UUID, replicas int) error {
	query := `
		UPDATE functions
		SET available_replicas = $1, updated_at = $2
		WHERE id = $3
	`
	result, err := r.db.ExecContext(ctx, query, replicas, time.Now(), id)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// UpdateImageURI updates the image URI after a build completes
func (r *FunctionRepository) UpdateImageURI(ctx context.Context, id uuid.UUID, imageURI string) error {
	query := `
		UPDATE functions
		SET image_uri = $1, updated_at = $2
		WHERE id = $3
	`
	result, err := r.db.ExecContext(ctx, query, imageURI, time.Now(), id)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// RecordInvocation records a function invocation and updates metrics
func (r *FunctionRepository) RecordInvocation(ctx context.Context, id uuid.UUID, durationMs int64) error {
	now := time.Now()
	query := `
		UPDATE functions
		SET invocation_count = invocation_count + 1,
		    avg_duration_ms = (avg_duration_ms * invocation_count + $1) / (invocation_count + 1),
		    last_invoked_at = $2,
		    updated_at = $3
		WHERE id = $4
	`
	result, err := r.db.ExecContext(ctx, query, durationMs, now, now, id)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// SoftDelete marks a function as deleted (soft delete)
func (r *FunctionRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	now := time.Now()
	query := `
		UPDATE functions
		SET status = $1, deleted_at = $2, updated_at = $3
		WHERE id = $4 AND deleted_at IS NULL
	`
	result, err := r.db.ExecContext(ctx, query, types.FunctionStatusDeleting, now, now, id)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// Delete permanently deletes a function (use with caution)
func (r *FunctionRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM functions WHERE id = $1`
	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// ============================================================================
// INVOCATION OPERATIONS
// ============================================================================

// CreateInvocation creates a new invocation record
func (r *FunctionRepository) CreateInvocation(ctx context.Context, inv *types.FunctionInvocation) error {
	inv.ID = uuid.New()
	inv.CreatedAt = time.Now()

	query := `
		INSERT INTO function_invocations (id, function_id, started_at, duration_ms, status_code, cold_start, error_type, request_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := r.db.ExecContext(ctx, query,
		inv.ID, inv.FunctionID, inv.StartedAt, inv.DurationMs, inv.StatusCode, inv.ColdStart, inv.ErrorType, inv.RequestID, inv.CreatedAt,
	)
	return err
}

// GetInvocationsByFunction retrieves invocations for a function
func (r *FunctionRepository) GetInvocationsByFunction(ctx context.Context, functionID uuid.UUID, limit int, since *time.Time) ([]*types.FunctionInvocation, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	var query string
	var args []interface{}

	if since != nil {
		query = `
			SELECT id, function_id, started_at, duration_ms, status_code, cold_start, error_type, request_id, created_at
			FROM function_invocations
			WHERE function_id = $1 AND started_at >= $2
			ORDER BY started_at DESC
			LIMIT $3
		`
		args = []interface{}{functionID, *since, limit}
	} else {
		query = `
			SELECT id, function_id, started_at, duration_ms, status_code, cold_start, error_type, request_id, created_at
			FROM function_invocations
			WHERE function_id = $1
			ORDER BY started_at DESC
			LIMIT $2
		`
		args = []interface{}{functionID, limit}
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var invocations []*types.FunctionInvocation
	for rows.Next() {
		inv := &types.FunctionInvocation{}
		var durationMs, statusCode sql.NullInt64
		var errorType, requestID sql.NullString

		err := rows.Scan(
			&inv.ID, &inv.FunctionID, &inv.StartedAt, &durationMs, &statusCode, &inv.ColdStart, &errorType, &requestID, &inv.CreatedAt,
		)
		if err != nil {
			return nil, err
		}

		if durationMs.Valid {
			d := durationMs.Int64
			inv.DurationMs = &d
		}
		if statusCode.Valid {
			s := int(statusCode.Int64)
			inv.StatusCode = &s
		}
		if errorType.Valid {
			inv.ErrorType = &errorType.String
		}
		if requestID.Valid {
			inv.RequestID = requestID.String
		}

		invocations = append(invocations, inv)
	}

	return invocations, nil
}

// ============================================================================
// METRICS OPERATIONS
// ============================================================================

// GetMetrics retrieves aggregated metrics for a function
func (r *FunctionRepository) GetMetrics(ctx context.Context, functionID uuid.UUID, period string, since time.Time) ([]*types.FunctionMetrics, error) {
	query := `
		SELECT id, function_id, period, period_start, period_end,
		       total_invocations, success_count, error_count, cold_start_count,
		       avg_duration_ms, p50_duration_ms, p95_duration_ms, p99_duration_ms, created_at
		FROM function_metrics
		WHERE function_id = $1 AND period = $2 AND period_start >= $3
		ORDER BY period_start DESC
	`

	rows, err := r.db.QueryContext(ctx, query, functionID, period, since)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var metrics []*types.FunctionMetrics
	for rows.Next() {
		m := &types.FunctionMetrics{}
		var id uuid.UUID
		var createdAt time.Time

		err := rows.Scan(
			&id, &m.FunctionID, &m.Period, &m.PeriodStart, &m.PeriodEnd,
			&m.TotalInvocations, &m.SuccessCount, &m.ErrorCount, &m.ColdStartCount,
			&m.AvgDurationMs, &m.P50DurationMs, &m.P95DurationMs, &m.P99DurationMs, &createdAt,
		)
		if err != nil {
			return nil, err
		}

		metrics = append(metrics, m)
	}

	return metrics, nil
}

// UpsertMetrics creates or updates aggregated metrics
func (r *FunctionRepository) UpsertMetrics(ctx context.Context, m *types.FunctionMetrics) error {
	query := `
		INSERT INTO function_metrics (id, function_id, period, period_start, period_end,
		    total_invocations, success_count, error_count, cold_start_count,
		    avg_duration_ms, p50_duration_ms, p95_duration_ms, p99_duration_ms, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (function_id, period, period_start)
		DO UPDATE SET
		    total_invocations = EXCLUDED.total_invocations,
		    success_count = EXCLUDED.success_count,
		    error_count = EXCLUDED.error_count,
		    cold_start_count = EXCLUDED.cold_start_count,
		    avg_duration_ms = EXCLUDED.avg_duration_ms,
		    p50_duration_ms = EXCLUDED.p50_duration_ms,
		    p95_duration_ms = EXCLUDED.p95_duration_ms,
		    p99_duration_ms = EXCLUDED.p99_duration_ms
	`

	_, err := r.db.ExecContext(ctx, query,
		uuid.New(), m.FunctionID, m.Period, m.PeriodStart, m.PeriodEnd,
		m.TotalInvocations, m.SuccessCount, m.ErrorCount, m.ColdStartCount,
		m.AvgDurationMs, m.P50DurationMs, m.P95DurationMs, m.P99DurationMs, time.Now(),
	)
	return err
}

// Build trigger 1768798621
