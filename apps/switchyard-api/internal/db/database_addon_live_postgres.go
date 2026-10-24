package db

import (
	"context"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ListLivePostgres retrieves every provisioned managed-Postgres addon that is
// not on its way out, for the reconciler's NetworkPolicy sweep.
func (r *DatabaseAddonRepository) ListLivePostgres(ctx context.Context) ([]*types.DatabaseAddon, error) {
	query := `
		SELECT id, project_id, environment_id, type, name, plan, status, status_message,
		       config, k8s_namespace, k8s_resource_name, connection_secret,
		       host, port, database_name, username,
		       storage_used_bytes, connections_active, last_backup_at,
		       created_by, created_by_email, created_at, updated_at, provisioned_at,
		       deletion_scheduled_at, deleted_at
		FROM database_addons
		WHERE type = 'postgres' AND status IN ('ready', 'provisioning')
		  AND k8s_namespace <> '' AND k8s_resource_name <> '' AND deleted_at IS NULL
		ORDER BY created_at ASC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return r.scanAddons(rows)
}
