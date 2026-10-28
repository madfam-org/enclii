package db

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

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

	if err := rows.Err(); err != nil {
		return nil, err
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

	if err := rows.Err(); err != nil {
		return nil, err
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

// GetNonDeletedBindingsByAddon includes suspended bindings: a reconstructed
// kernel's old addon is not unbound while any binding may still be resumed.
func (r *DatabaseAddonRepository) GetNonDeletedBindingsByAddon(ctx context.Context, addonID uuid.UUID) ([]*types.DatabaseAddonBinding, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,addon_id,service_id,env_var_name,status,created_at,updated_at FROM database_addon_bindings WHERE addon_id=$1 AND status <> 'deleted' ORDER BY created_at DESC`, addonID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []*types.DatabaseAddonBinding
	for rows.Next() {
		b := &types.DatabaseAddonBinding{}
		if err = rows.Scan(&b.ID, &b.AddonID, &b.ServiceID, &b.EnvVarName, &b.Status, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
