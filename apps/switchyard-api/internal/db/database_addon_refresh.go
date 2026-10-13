package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ErrAddonRefreshConflict means a cached Ready snapshot changed before repair.
// Callers must retain the current lifecycle state and must not retry blindly.
var ErrAddonRefreshConflict = errors.New("addon changed during readiness refresh")

// UpdateReadinessIfUnchanged repairs only observed status/connection metadata.
// It never rewrites configuration, custody, usage or retention columns. The
// loaded timestamp fences those concurrent writes as well as lifecycle changes.
func (r *DatabaseAddonRepository) UpdateReadinessIfUnchanged(ctx context.Context, addon *types.DatabaseAddon, status types.DatabaseAddonStatus, updatedAt time.Time) error {
	addon.UpdatedAt = time.Now()
	result, err := r.db.ExecContext(ctx, `
  UPDATE database_addons
  SET status = $1, status_message = $2, host = $3, port = $4,
      database_name = $5, username = $6, updated_at = $7, provisioned_at = $8
  WHERE id = $9 AND status = $10 AND updated_at = $11 AND deleted_at IS NULL
 `, addon.Status, addon.StatusMessage, addon.Host, addon.Port,
		addon.DatabaseName, addon.Username, addon.UpdatedAt, addon.ProvisionedAt,
		addon.ID, status, updatedAt)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("readiness refresh persistence is uncertain: %w", err)
	}
	if rows == 0 {
		return ErrAddonRefreshConflict
	}
	return nil
}
