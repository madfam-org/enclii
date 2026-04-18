package types

import (
	"time"

	"github.com/google/uuid"
)

// TenantExportStatus is the lifecycle of a tenant export request.
//
// pending  — awaiting HITL approval (production only).
// running  — pipeline is gathering + packing.
// ready    — tarball in R2, sha256 recorded, valid pre-signed URL issuable.
// failed   — pipeline errored; partial tarballs have been purged.
// expired  — past the 14-day retention window; R2 object deleted.
// deleted  — soft-deleted by an admin ahead of natural expiry.
type TenantExportStatus string

const (
	TenantExportStatusPending TenantExportStatus = "pending"
	TenantExportStatusRunning TenantExportStatus = "running"
	TenantExportStatusReady   TenantExportStatus = "ready"
	TenantExportStatusFailed  TenantExportStatus = "failed"
	TenantExportStatusExpired TenantExportStatus = "expired"
	TenantExportStatusDeleted TenantExportStatus = "deleted"
)

// IsTerminal returns true for statuses the pipeline will never leave.
func (s TenantExportStatus) IsTerminal() bool {
	switch s {
	case TenantExportStatusReady,
		TenantExportStatusFailed,
		TenantExportStatusExpired,
		TenantExportStatusDeleted:
		return true
	}
	return false
}

// TenantExport is a row in the tenant_exports table — a customer-initiated
// request to hand back everything Enclii holds about a project.
//
// See docs/architecture/tenant-export.md for scope and retention.
type TenantExport struct {
	ID        uuid.UUID `json:"id" db:"id"`
	ProjectID uuid.UUID `json:"project_id" db:"project_id"`

	Status TenantExportStatus `json:"status" db:"status"`

	// Attribution
	RequestedBy string     `json:"requested_by" db:"requested_by"`
	RequestedAt time.Time  `json:"requested_at" db:"requested_at"`
	ApprovedBy  *string    `json:"approved_by,omitempty" db:"approved_by"`
	ApprovedAt  *time.Time `json:"approved_at,omitempty" db:"approved_at"`

	// Output. Populated once status=ready.
	TarballR2Key     *string `json:"tarball_r2_key,omitempty" db:"tarball_r2_key"`
	TarballSizeBytes *int64  `json:"tarball_size_bytes,omitempty" db:"tarball_size_bytes"`
	SHA256           *string `json:"sha256,omitempty" db:"sha256"`
	PartCount        int     `json:"part_count" db:"part_count"`

	// Failure/forensics
	ErrorMessage *string    `json:"error_message,omitempty" db:"error_message"`
	StartedAt    *time.Time `json:"started_at,omitempty" db:"started_at"`
	CompletedAt  *time.Time `json:"completed_at,omitempty" db:"completed_at"`

	// Retention (14 days after ready).
	ExpiresAt *time.Time `json:"expires_at,omitempty" db:"expires_at"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// TenantExportDownload is the shape returned from GET /v1/exports/:id when
// the caller is authorized to download. The URL is freshly pre-signed per
// request and expires in 15 minutes.
type TenantExportDownload struct {
	Export      *TenantExport `json:"export"`
	DownloadURL string        `json:"download_url,omitempty"`
	ExpiresIn   int           `json:"expires_in_seconds,omitempty"` // pre-signed URL TTL
}
