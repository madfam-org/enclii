package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// TenantExportRepository owns the tenant_exports table (P3.6).
//
// Rows represent customer-initiated export requests. The pipeline writes
// status transitions via Update*; callers only Create and List.
type TenantExportRepository struct {
	db DBTX
}

// ErrTenantExportNotFound is returned from Get when the row is missing.
var ErrTenantExportNotFound = errors.New("tenant export not found")

// NewTenantExportRepository wires the repo against the root *sql.DB.
func NewTenantExportRepository(db DBTX) *TenantExportRepository {
	return &TenantExportRepository{db: db}
}

// Create inserts a new row. Caller is responsible for setting Status to
// either `pending` (prod HITL path) or `running` (non-prod). ID and
// timestamps are assigned here if zero.
func (r *TenantExportRepository) Create(ctx context.Context, e *types.TenantExport) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	now := time.Now().UTC()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	if e.RequestedAt.IsZero() {
		e.RequestedAt = now
	}
	e.UpdatedAt = now

	if e.PartCount == 0 {
		e.PartCount = 1
	}

	query := `
		INSERT INTO tenant_exports (
			id, project_id, status,
			requested_by, requested_at,
			approved_by, approved_at,
			tarball_r2_key, tarball_size_bytes, sha256, part_count,
			error_message, started_at, completed_at,
			expires_at, created_at, updated_at
		) VALUES (
			$1, $2, $3,
			$4, $5,
			$6, $7,
			$8, $9, $10, $11,
			$12, $13, $14,
			$15, $16, $17
		)
	`

	_, err := r.db.ExecContext(ctx, query,
		e.ID, e.ProjectID, e.Status,
		e.RequestedBy, e.RequestedAt,
		e.ApprovedBy, e.ApprovedAt,
		e.TarballR2Key, e.TarballSizeBytes, e.SHA256, e.PartCount,
		e.ErrorMessage, e.StartedAt, e.CompletedAt,
		e.ExpiresAt, e.CreatedAt, e.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert tenant_exports: %w", err)
	}
	return nil
}

// GetByID loads one row.
func (r *TenantExportRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.TenantExport, error) {
	query := `
		SELECT id, project_id, status,
		       requested_by, requested_at,
		       approved_by, approved_at,
		       tarball_r2_key, tarball_size_bytes, sha256, part_count,
		       error_message, started_at, completed_at,
		       expires_at, created_at, updated_at
		FROM tenant_exports
		WHERE id = $1
	`
	row := r.db.QueryRowContext(ctx, query, id)
	return scanTenantExport(row)
}

// ListByProject returns project exports newest-first within the retention
// window (90 days — the row retains past R2 deletion so the history UI
// doesn't go blank).
func (r *TenantExportRepository) ListByProject(ctx context.Context, projectID uuid.UUID, limit int) ([]*types.TenantExport, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	query := `
		SELECT id, project_id, status,
		       requested_by, requested_at,
		       approved_by, approved_at,
		       tarball_r2_key, tarball_size_bytes, sha256, part_count,
		       error_message, started_at, completed_at,
		       expires_at, created_at, updated_at
		FROM tenant_exports
		WHERE project_id = $1
		  AND created_at > now() - interval '90 days'
		ORDER BY created_at DESC
		LIMIT $2
	`
	rows, err := r.db.QueryContext(ctx, query, projectID, limit)
	if err != nil {
		return nil, fmt.Errorf("list tenant_exports: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []*types.TenantExport
	for rows.Next() {
		e, err := scanTenantExport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListExpired returns rows whose R2 objects should be purged (status=ready,
// expires_at < now). Used by the cleanup cron.
func (r *TenantExportRepository) ListExpired(ctx context.Context, limit int) ([]*types.TenantExport, error) {
	if limit <= 0 {
		limit = 100
	}
	query := `
		SELECT id, project_id, status,
		       requested_by, requested_at,
		       approved_by, approved_at,
		       tarball_r2_key, tarball_size_bytes, sha256, part_count,
		       error_message, started_at, completed_at,
		       expires_at, created_at, updated_at
		FROM tenant_exports
		WHERE status = 'ready'
		  AND expires_at IS NOT NULL
		  AND expires_at < now()
		ORDER BY expires_at ASC
		LIMIT $1
	`
	rows, err := r.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("list expired tenant_exports: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []*types.TenantExport
	for rows.Next() {
		e, err := scanTenantExport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// UpdateStatus performs a status transition plus the specific fields that
// follow from that transition. Keeps the call site small at the service
// layer — the pipeline only needs a few verbs.
//
// `running` transition sets started_at; `ready` sets tarball_r2_key,
// tarball_size_bytes, sha256, part_count, completed_at, expires_at;
// `failed`/`expired`/`deleted` set completed_at and error_message (if any).
type TenantExportUpdate struct {
	Status           types.TenantExportStatus
	ApprovedBy       *string
	TarballR2Key     *string
	TarballSizeBytes *int64
	SHA256           *string
	PartCount        *int
	ErrorMessage     *string
	ExpiresAt        *time.Time
}

// Update applies a transition. It's intentionally one method — callers at
// the service layer feed in a prebuilt TenantExportUpdate that reflects
// which fields the transition is responsible for. Unset pointer fields
// are left alone on the row (no silent blanking).
func (r *TenantExportRepository) Update(ctx context.Context, id uuid.UUID, u TenantExportUpdate) error {
	now := time.Now().UTC()

	// Start with the common always-set transitions.
	args := []interface{}{now, u.Status, id}
	query := `
		UPDATE tenant_exports
		SET updated_at = $1,
		    status = $2`

	// Lifecycle timestamps inferred from the target status.
	switch u.Status {
	case types.TenantExportStatusRunning:
		query += `, started_at = COALESCE(started_at, $1)`
		if u.ApprovedBy != nil {
			args = append(args, *u.ApprovedBy, now)
			query += fmt.Sprintf(", approved_by = $%d, approved_at = $%d", len(args)-1, len(args))
		}
	case types.TenantExportStatusReady:
		query += `, completed_at = $1`
		if u.TarballR2Key != nil {
			args = append(args, *u.TarballR2Key)
			query += fmt.Sprintf(", tarball_r2_key = $%d", len(args))
		}
		if u.TarballSizeBytes != nil {
			args = append(args, *u.TarballSizeBytes)
			query += fmt.Sprintf(", tarball_size_bytes = $%d", len(args))
		}
		if u.SHA256 != nil {
			args = append(args, *u.SHA256)
			query += fmt.Sprintf(", sha256 = $%d", len(args))
		}
		if u.PartCount != nil {
			args = append(args, *u.PartCount)
			query += fmt.Sprintf(", part_count = $%d", len(args))
		}
		if u.ExpiresAt != nil {
			args = append(args, *u.ExpiresAt)
			query += fmt.Sprintf(", expires_at = $%d", len(args))
		}
	case types.TenantExportStatusFailed,
		types.TenantExportStatusExpired,
		types.TenantExportStatusDeleted:
		query += `, completed_at = COALESCE(completed_at, $1)`
		if u.ErrorMessage != nil {
			args = append(args, *u.ErrorMessage)
			query += fmt.Sprintf(", error_message = $%d", len(args))
		}
	}

	query += ` WHERE id = $3`

	res, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update tenant_exports: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update tenant_exports rows affected: %w", err)
	}
	if n == 0 {
		return ErrTenantExportNotFound
	}
	return nil
}

// scanTenantExport reads one row into a TenantExport. Works for both
// *sql.Row and *sql.Rows thanks to the common Scan signature (uses the
// package-local rowScanner declared in canary_rollout_repository.go).
func scanTenantExport(row rowScanner) (*types.TenantExport, error) {
	e := &types.TenantExport{}
	err := row.Scan(
		&e.ID, &e.ProjectID, &e.Status,
		&e.RequestedBy, &e.RequestedAt,
		&e.ApprovedBy, &e.ApprovedAt,
		&e.TarballR2Key, &e.TarballSizeBytes, &e.SHA256, &e.PartCount,
		&e.ErrorMessage, &e.StartedAt, &e.CompletedAt,
		&e.ExpiresAt, &e.CreatedAt, &e.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTenantExportNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan tenant_exports: %w", err)
	}
	return e, nil
}
