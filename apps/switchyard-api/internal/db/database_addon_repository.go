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

// DatabaseAddonRepository handles database addon CRUD operations
type DatabaseAddonRepository struct {
	db DBTX
}

// NewDatabaseAddonRepository creates a new database addon repository
func NewDatabaseAddonRepository(db DBTX) *DatabaseAddonRepository {
	return &DatabaseAddonRepository{db: db}
}

// NewDatabaseAddonRepositoryWithTx creates a repository using a transaction
func NewDatabaseAddonRepositoryWithTx(tx DBTX) *DatabaseAddonRepository {
	return &DatabaseAddonRepository{db: tx}
}

// Create creates a new database addon
func (r *DatabaseAddonRepository) Create(ctx context.Context, addon *types.DatabaseAddon) error {
	addon.ID = uuid.New()
	addon.CreatedAt = time.Now()
	addon.UpdatedAt = time.Now()
	addon.Status = types.DatabaseAddonStatusPending

	configJSON, err := json.Marshal(addon.Config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Plan defaults to standard-0 for backward compatibility with any legacy
	// callers that construct DatabaseAddon without explicitly setting Plan.
	// New code paths (AddonService.CreateAddon post-P3.1) always set it.
	plan := addon.Plan
	if plan == "" {
		plan = "standard-0"
		addon.Plan = plan
	}

	query := `
		INSERT INTO database_addons (
			id, project_id, environment_id, type, name, plan, status, status_message,
			config, k8s_namespace, k8s_resource_name, connection_secret,
			host, port, database_name, username,
			storage_used_bytes, connections_active,
			created_by, created_by_email, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22)
	`
	_, err = r.db.ExecContext(ctx, query,
		addon.ID, addon.ProjectID, addon.EnvironmentID,
		addon.Type, addon.Name, plan, addon.Status, addon.StatusMessage,
		configJSON, addon.K8sNamespace, addon.K8sResourceName, addon.ConnectionSecret,
		addon.Host, addon.Port, addon.DatabaseName, addon.Username,
		addon.StorageUsedBytes, addon.ConnectionsActive,
		addon.CreatedBy, addon.CreatedByEmail, addon.CreatedAt, addon.UpdatedAt,
	)
	return err
}

// GetByID retrieves a database addon by ID
func (r *DatabaseAddonRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.DatabaseAddon, error) {
	addon := &types.DatabaseAddon{}
	var configJSON []byte
	var envID, createdBy sql.NullString
	var statusMsg, k8sNs, k8sRes, connSecret, host, dbName, username, createdByEmail sql.NullString
	var port sql.NullInt64
	var provisionedAt, deletedAt, lastBackupAt sql.NullTime

	query := `
		SELECT id, project_id, environment_id, type, name, plan, status, status_message,
		       config, k8s_namespace, k8s_resource_name, connection_secret,
		       host, port, database_name, username,
		       storage_used_bytes, connections_active, last_backup_at,
		       created_by, created_by_email, created_at, updated_at, provisioned_at, deleted_at
		FROM database_addons WHERE id = $1 AND deleted_at IS NULL
	`

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&addon.ID, &addon.ProjectID, &envID, &addon.Type, &addon.Name, &addon.Plan, &addon.Status, &statusMsg,
		&configJSON, &k8sNs, &k8sRes, &connSecret,
		&host, &port, &dbName, &username,
		&addon.StorageUsedBytes, &addon.ConnectionsActive, &lastBackupAt,
		&createdBy, &createdByEmail, &addon.CreatedAt, &addon.UpdatedAt, &provisionedAt, &deletedAt,
	)
	if err != nil {
		return nil, err
	}

	// Parse nullable fields
	if envID.Valid {
		parsed, _ := uuid.Parse(envID.String)
		addon.EnvironmentID = &parsed
	}
	if createdBy.Valid {
		parsed, _ := uuid.Parse(createdBy.String)
		addon.CreatedBy = &parsed
	}
	if statusMsg.Valid {
		addon.StatusMessage = statusMsg.String
	}
	if k8sNs.Valid {
		addon.K8sNamespace = k8sNs.String
	}
	if k8sRes.Valid {
		addon.K8sResourceName = k8sRes.String
	}
	if connSecret.Valid {
		addon.ConnectionSecret = connSecret.String
	}
	if host.Valid {
		addon.Host = host.String
	}
	if port.Valid {
		addon.Port = int(port.Int64)
	}
	if dbName.Valid {
		addon.DatabaseName = dbName.String
	}
	if username.Valid {
		addon.Username = username.String
	}
	if createdByEmail.Valid {
		addon.CreatedByEmail = createdByEmail.String
	}
	if provisionedAt.Valid {
		addon.ProvisionedAt = &provisionedAt.Time
	}
	if deletedAt.Valid {
		addon.DeletedAt = &deletedAt.Time
	}
	if lastBackupAt.Valid {
		addon.LastBackupAt = &lastBackupAt.Time
	}

	// Parse config JSON
	if len(configJSON) > 0 {
		if err := json.Unmarshal(configJSON, &addon.Config); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config: %w", err)
		}
	}

	return addon, nil
}

// GetByName retrieves a database addon by project ID and name
func (r *DatabaseAddonRepository) GetByName(ctx context.Context, projectID uuid.UUID, name string) (*types.DatabaseAddon, error) {
	addon := &types.DatabaseAddon{}
	var configJSON []byte
	var envID, createdBy sql.NullString
	var statusMsg, k8sNs, k8sRes, connSecret, host, dbName, username, createdByEmail sql.NullString
	var port sql.NullInt64
	var provisionedAt, deletedAt, lastBackupAt sql.NullTime

	query := `
		SELECT id, project_id, environment_id, type, name, plan, status, status_message,
		       config, k8s_namespace, k8s_resource_name, connection_secret,
		       host, port, database_name, username,
		       storage_used_bytes, connections_active, last_backup_at,
		       created_by, created_by_email, created_at, updated_at, provisioned_at, deleted_at
		FROM database_addons
		WHERE project_id = $1 AND name = $2 AND deleted_at IS NULL
	`

	err := r.db.QueryRowContext(ctx, query, projectID, name).Scan(
		&addon.ID, &addon.ProjectID, &envID, &addon.Type, &addon.Name, &addon.Plan, &addon.Status, &statusMsg,
		&configJSON, &k8sNs, &k8sRes, &connSecret,
		&host, &port, &dbName, &username,
		&addon.StorageUsedBytes, &addon.ConnectionsActive, &lastBackupAt,
		&createdBy, &createdByEmail, &addon.CreatedAt, &addon.UpdatedAt, &provisionedAt, &deletedAt,
	)
	if err != nil {
		return nil, err
	}

	// Parse nullable fields
	if envID.Valid {
		parsed, _ := uuid.Parse(envID.String)
		addon.EnvironmentID = &parsed
	}
	if createdBy.Valid {
		parsed, _ := uuid.Parse(createdBy.String)
		addon.CreatedBy = &parsed
	}
	if statusMsg.Valid {
		addon.StatusMessage = statusMsg.String
	}
	if k8sNs.Valid {
		addon.K8sNamespace = k8sNs.String
	}
	if k8sRes.Valid {
		addon.K8sResourceName = k8sRes.String
	}
	if connSecret.Valid {
		addon.ConnectionSecret = connSecret.String
	}
	if host.Valid {
		addon.Host = host.String
	}
	if port.Valid {
		addon.Port = int(port.Int64)
	}
	if dbName.Valid {
		addon.DatabaseName = dbName.String
	}
	if username.Valid {
		addon.Username = username.String
	}
	if createdByEmail.Valid {
		addon.CreatedByEmail = createdByEmail.String
	}
	if provisionedAt.Valid {
		addon.ProvisionedAt = &provisionedAt.Time
	}
	if deletedAt.Valid {
		addon.DeletedAt = &deletedAt.Time
	}
	if lastBackupAt.Valid {
		addon.LastBackupAt = &lastBackupAt.Time
	}

	// Parse config JSON
	if len(configJSON) > 0 {
		if err := json.Unmarshal(configJSON, &addon.Config); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config: %w", err)
		}
	}

	return addon, nil
}

// ListByProject retrieves all database addons for a project
func (r *DatabaseAddonRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]*types.DatabaseAddon, error) {
	query := `
		SELECT id, project_id, environment_id, type, name, plan, status, status_message,
		       config, k8s_namespace, k8s_resource_name, connection_secret,
		       host, port, database_name, username,
		       storage_used_bytes, connections_active, last_backup_at,
		       created_by, created_by_email, created_at, updated_at, provisioned_at, deleted_at
		FROM database_addons
		WHERE project_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return r.scanAddons(rows)
}

// ListByProjects retrieves all database addons for multiple projects
func (r *DatabaseAddonRepository) ListByProjects(ctx context.Context, projectIDs []uuid.UUID) ([]*types.DatabaseAddon, error) {
	if len(projectIDs) == 0 {
		return []*types.DatabaseAddon{}, nil
	}

	// Build query with IN clause. lib/pq does not auto-convert []uuid.UUID
	// to a postgres array — pq.Array() wraps it so `= ANY($1)` works. Without
	// this wrap, /v1/databases returns 500 with
	// `unsupported type []uuid.UUID, a slice of array`.
	query := `
		SELECT id, project_id, environment_id, type, name, plan, status, status_message,
		       config, k8s_namespace, k8s_resource_name, connection_secret,
		       host, port, database_name, username,
		       storage_used_bytes, connections_active, last_backup_at,
		       created_by, created_by_email, created_at, updated_at, provisioned_at, deleted_at
		FROM database_addons
		WHERE project_id = ANY($1) AND deleted_at IS NULL
		ORDER BY created_at DESC
	`

	// Convert []uuid.UUID to []string for pq.Array — pq's UUID array support
	// requires the elements to be stringable; uuid.UUID's String() handles it.
	idStrings := make([]string, len(projectIDs))
	for i, id := range projectIDs {
		idStrings[i] = id.String()
	}

	rows, err := r.db.QueryContext(ctx, query, pq.Array(idStrings))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return r.scanAddons(rows)
}

// ListByType retrieves all database addons of a specific type for a project
func (r *DatabaseAddonRepository) ListByType(ctx context.Context, projectID uuid.UUID, addonType types.DatabaseAddonType) ([]*types.DatabaseAddon, error) {
	query := `
		SELECT id, project_id, environment_id, type, name, plan, status, status_message,
		       config, k8s_namespace, k8s_resource_name, connection_secret,
		       host, port, database_name, username,
		       storage_used_bytes, connections_active, last_backup_at,
		       created_by, created_by_email, created_at, updated_at, provisioned_at, deleted_at
		FROM database_addons
		WHERE project_id = $1 AND type = $2 AND deleted_at IS NULL
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, projectID, addonType)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return r.scanAddons(rows)
}

// ListPending retrieves all addons in pending/provisioning state (for reconciler)
func (r *DatabaseAddonRepository) ListPending(ctx context.Context) ([]*types.DatabaseAddon, error) {
	query := `
		SELECT id, project_id, environment_id, type, name, plan, status, status_message,
		       config, k8s_namespace, k8s_resource_name, connection_secret,
		       host, port, database_name, username,
		       storage_used_bytes, connections_active, last_backup_at,
		       created_by, created_by_email, created_at, updated_at, provisioned_at, deleted_at
		FROM database_addons
		WHERE status IN ('pending', 'provisioning', 'deleting') AND deleted_at IS NULL
		ORDER BY created_at ASC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return r.scanAddons(rows)
}

// scanAddons scans multiple addon rows
func (r *DatabaseAddonRepository) scanAddons(rows *sql.Rows) ([]*types.DatabaseAddon, error) {
	var addons []*types.DatabaseAddon

	for rows.Next() {
		addon := &types.DatabaseAddon{}
		var configJSON []byte
		var envID, createdBy sql.NullString
		var statusMsg, k8sNs, k8sRes, connSecret, host, dbName, username, createdByEmail sql.NullString
		var port sql.NullInt64
		var provisionedAt, deletedAt, lastBackupAt sql.NullTime

		err := rows.Scan(
			&addon.ID, &addon.ProjectID, &envID, &addon.Type, &addon.Name, &addon.Plan, &addon.Status, &statusMsg,
			&configJSON, &k8sNs, &k8sRes, &connSecret,
			&host, &port, &dbName, &username,
			&addon.StorageUsedBytes, &addon.ConnectionsActive, &lastBackupAt,
			&createdBy, &createdByEmail, &addon.CreatedAt, &addon.UpdatedAt, &provisionedAt, &deletedAt,
		)
		if err != nil {
			return nil, err
		}

		// Parse nullable fields
		if envID.Valid {
			parsed, _ := uuid.Parse(envID.String)
			addon.EnvironmentID = &parsed
		}
		if createdBy.Valid {
			parsed, _ := uuid.Parse(createdBy.String)
			addon.CreatedBy = &parsed
		}
		if statusMsg.Valid {
			addon.StatusMessage = statusMsg.String
		}
		if k8sNs.Valid {
			addon.K8sNamespace = k8sNs.String
		}
		if k8sRes.Valid {
			addon.K8sResourceName = k8sRes.String
		}
		if connSecret.Valid {
			addon.ConnectionSecret = connSecret.String
		}
		if host.Valid {
			addon.Host = host.String
		}
		if port.Valid {
			addon.Port = int(port.Int64)
		}
		if dbName.Valid {
			addon.DatabaseName = dbName.String
		}
		if username.Valid {
			addon.Username = username.String
		}
		if createdByEmail.Valid {
			addon.CreatedByEmail = createdByEmail.String
		}
		if provisionedAt.Valid {
			addon.ProvisionedAt = &provisionedAt.Time
		}
		if deletedAt.Valid {
			addon.DeletedAt = &deletedAt.Time
		}
		if lastBackupAt.Valid {
			addon.LastBackupAt = &lastBackupAt.Time
		}

		// Parse config JSON
		if len(configJSON) > 0 {
			if err := json.Unmarshal(configJSON, &addon.Config); err != nil {
				return nil, fmt.Errorf("failed to unmarshal config: %w", err)
			}
		}

		addons = append(addons, addon)
	}

	return addons, nil
}

// Update updates a database addon
func (r *DatabaseAddonRepository) Update(ctx context.Context, addon *types.DatabaseAddon) error {
	addon.UpdatedAt = time.Now()

	configJSON, err := json.Marshal(addon.Config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	query := `
		UPDATE database_addons
		SET status = $1, status_message = $2, config = $3,
		    k8s_namespace = $4, k8s_resource_name = $5, connection_secret = $6,
		    host = $7, port = $8, database_name = $9, username = $10,
		    storage_used_bytes = $11, connections_active = $12, last_backup_at = $13,
		    updated_at = $14, provisioned_at = $15
		WHERE id = $16
	`
	result, err := r.db.ExecContext(ctx, query,
		addon.Status, addon.StatusMessage, configJSON,
		addon.K8sNamespace, addon.K8sResourceName, addon.ConnectionSecret,
		addon.Host, addon.Port, addon.DatabaseName, addon.Username,
		addon.StorageUsedBytes, addon.ConnectionsActive, addon.LastBackupAt,
		addon.UpdatedAt, addon.ProvisionedAt,
		addon.ID,
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

// UpdateStatus updates just the status of a database addon
func (r *DatabaseAddonRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status types.DatabaseAddonStatus, message string) error {
	query := `
		UPDATE database_addons
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

// MarkProvisioned marks an addon as ready with connection info
func (r *DatabaseAddonRepository) MarkProvisioned(ctx context.Context, id uuid.UUID, host string, port int, dbName, username, connSecret string) error {
	now := time.Now()
	query := `
		UPDATE database_addons
		SET status = $1, host = $2, port = $3, database_name = $4, username = $5,
		    connection_secret = $6, updated_at = $7, provisioned_at = $8
		WHERE id = $9
	`
	result, err := r.db.ExecContext(ctx, query,
		types.DatabaseAddonStatusReady, host, port, dbName, username, connSecret, now, now, id,
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

// SoftDelete marks a database addon as deleted (soft delete)
func (r *DatabaseAddonRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	now := time.Now()
	query := `
		UPDATE database_addons
		SET status = $1, deleted_at = $2, updated_at = $3
		WHERE id = $4 AND deleted_at IS NULL
	`
	result, err := r.db.ExecContext(ctx, query, types.DatabaseAddonStatusDeleted, now, now, id)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// Delete permanently deletes a database addon (use with caution)
func (r *DatabaseAddonRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM database_addons WHERE id = $1`
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
// BINDING OPERATIONS
// ============================================================================

// CreateBinding creates a new service binding for an addon
func (r *DatabaseAddonRepository) CreateBinding(ctx context.Context, binding *types.DatabaseAddonBinding) error {
	binding.ID = uuid.New()
	binding.CreatedAt = time.Now()
	binding.UpdatedAt = time.Now()
	binding.Status = types.DatabaseAddonBindingStatusActive

	// Set default env var name based on addon type
	if binding.EnvVarName == "" {
		binding.EnvVarName = "DATABASE_URL"
	}

	query := `
		INSERT INTO database_addon_bindings (id, addon_id, service_id, env_var_name, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := r.db.ExecContext(ctx, query,
		binding.ID, binding.AddonID, binding.ServiceID, binding.EnvVarName, binding.Status, binding.CreatedAt, binding.UpdatedAt,
	)
	return err
}

// GetBindingsByAddon retrieves all service bindings for an addon
func (r *DatabaseAddonRepository) GetBindingsByAddon(ctx context.Context, addonID uuid.UUID) ([]*types.DatabaseAddonBinding, error) {
	query := `
		SELECT id, addon_id, service_id, env_var_name, status, created_at, updated_at
		FROM database_addon_bindings
		WHERE addon_id = $1 AND status = 'active'
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, addonID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var bindings []*types.DatabaseAddonBinding
	for rows.Next() {
		binding := &types.DatabaseAddonBinding{}
		err := rows.Scan(&binding.ID, &binding.AddonID, &binding.ServiceID, &binding.EnvVarName, &binding.Status, &binding.CreatedAt, &binding.UpdatedAt)
		if err != nil {
			return nil, err
		}
		bindings = append(bindings, binding)
	}

	return bindings, nil
}

// GetBindingsByService retrieves all addon bindings for a service
func (r *DatabaseAddonRepository) GetBindingsByService(ctx context.Context, serviceID uuid.UUID) ([]*types.DatabaseAddonBinding, error) {
	query := `
		SELECT id, addon_id, service_id, env_var_name, status, created_at, updated_at
		FROM database_addon_bindings
		WHERE service_id = $1 AND status = 'active'
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, serviceID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var bindings []*types.DatabaseAddonBinding
	for rows.Next() {
		binding := &types.DatabaseAddonBinding{}
		err := rows.Scan(&binding.ID, &binding.AddonID, &binding.ServiceID, &binding.EnvVarName, &binding.Status, &binding.CreatedAt, &binding.UpdatedAt)
		if err != nil {
			return nil, err
		}
		bindings = append(bindings, binding)
	}

	return bindings, nil
}

// DeleteBinding removes a service binding by binding ID
func (r *DatabaseAddonRepository) DeleteBinding(ctx context.Context, bindingID uuid.UUID) error {
	query := `
		UPDATE database_addon_bindings
		SET status = $1, updated_at = $2
		WHERE id = $3
	`
	result, err := r.db.ExecContext(ctx, query, types.DatabaseAddonBindingStatusDeleted, time.Now(), bindingID)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// DeleteBindingByAddonAndService removes a service binding by addon and service IDs
func (r *DatabaseAddonRepository) DeleteBindingByAddonAndService(ctx context.Context, addonID, serviceID uuid.UUID) error {
	query := `
		UPDATE database_addon_bindings
		SET status = $1, updated_at = $2
		WHERE addon_id = $3 AND service_id = $4 AND status = 'active'
	`
	result, err := r.db.ExecContext(ctx, query, types.DatabaseAddonBindingStatusDeleted, time.Now(), addonID, serviceID)
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
// BACKUP OPERATIONS
// ============================================================================

// CreateBackup creates a new backup record
func (r *DatabaseAddonRepository) CreateBackup(ctx context.Context, backup *types.DatabaseAddonBackup) error {
	backup.ID = uuid.New()
	backup.CreatedAt = time.Now()
	backup.Status = types.DatabaseAddonBackupStatusPending

	query := `
		INSERT INTO database_addon_backups (id, addon_id, backup_type, status, status_message, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := r.db.ExecContext(ctx, query,
		backup.ID, backup.AddonID, backup.BackupType, backup.Status, backup.StatusMessage, backup.CreatedAt,
	)
	return err
}

// UpdateBackup updates a backup record
func (r *DatabaseAddonRepository) UpdateBackup(ctx context.Context, backup *types.DatabaseAddonBackup) error {
	query := `
		UPDATE database_addon_backups
		SET status = $1, status_message = $2, storage_path = $3, size_bytes = $4,
		    started_at = $5, completed_at = $6, expires_at = $7
		WHERE id = $8
	`
	result, err := r.db.ExecContext(ctx, query,
		backup.Status, backup.StatusMessage, backup.StoragePath, backup.SizeBytes,
		backup.StartedAt, backup.CompletedAt, backup.ExpiresAt,
		backup.ID,
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

// GetBackupsByAddon retrieves all backups for an addon
func (r *DatabaseAddonRepository) GetBackupsByAddon(ctx context.Context, addonID uuid.UUID, limit int) ([]*types.DatabaseAddonBackup, error) {
	if limit <= 0 {
		limit = 50
	}

	query := `
		SELECT id, addon_id, backup_type, status, status_message, storage_path, size_bytes,
		       started_at, completed_at, expires_at, created_at
		FROM database_addon_backups
		WHERE addon_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`

	rows, err := r.db.QueryContext(ctx, query, addonID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var backups []*types.DatabaseAddonBackup
	for rows.Next() {
		backup := &types.DatabaseAddonBackup{}
		var statusMsg, storagePath sql.NullString
		var sizeBytes sql.NullInt64
		var startedAt, completedAt, expiresAt sql.NullTime

		err := rows.Scan(
			&backup.ID, &backup.AddonID, &backup.BackupType, &backup.Status, &statusMsg, &storagePath, &sizeBytes,
			&startedAt, &completedAt, &expiresAt, &backup.CreatedAt,
		)
		if err != nil {
			return nil, err
		}

		if statusMsg.Valid {
			backup.StatusMessage = statusMsg.String
		}
		if storagePath.Valid {
			backup.StoragePath = storagePath.String
		}
		if sizeBytes.Valid {
			backup.SizeBytes = sizeBytes.Int64
		}
		if startedAt.Valid {
			backup.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			backup.CompletedAt = &completedAt.Time
		}
		if expiresAt.Valid {
			backup.ExpiresAt = &expiresAt.Time
		}

		backups = append(backups, backup)
	}

	return backups, nil
}
