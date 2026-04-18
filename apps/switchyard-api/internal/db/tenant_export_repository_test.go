package db

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func TestTenantExportRepository_Create_AssignsDefaults(t *testing.T) {
	db, mock := newTestDB(t)
	repo := NewTenantExportRepository(db)

	mock.ExpectExec("INSERT INTO tenant_exports").
		WithArgs(
			sqlmock.AnyArg(), // id
			sqlmock.AnyArg(), // project_id
			types.TenantExportStatusRunning,
			"alice@example.com",
			sqlmock.AnyArg(), // requested_at
			sqlmock.AnyArg(), // approved_by
			sqlmock.AnyArg(), // approved_at
			sqlmock.AnyArg(), // tarball_r2_key
			sqlmock.AnyArg(), // tarball_size_bytes
			sqlmock.AnyArg(), // sha256
			1,
			sqlmock.AnyArg(), // error_message
			sqlmock.AnyArg(), // started_at
			sqlmock.AnyArg(), // completed_at
			sqlmock.AnyArg(), // expires_at
			sqlmock.AnyArg(), // created_at
			sqlmock.AnyArg(), // updated_at
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	e := &types.TenantExport{
		ProjectID:   uuid.New(),
		Status:      types.TenantExportStatusRunning,
		RequestedBy: "alice@example.com",
	}
	if err := repo.Create(context.Background(), e); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if e.ID == uuid.Nil {
		t.Errorf("ID not assigned")
	}
	if e.PartCount != 1 {
		t.Errorf("PartCount default = %d, want 1", e.PartCount)
	}
	if e.CreatedAt.IsZero() {
		t.Errorf("CreatedAt not set")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestTenantExportRepository_GetByID(t *testing.T) {
	db, mock := newTestDB(t)
	repo := NewTenantExportRepository(db)

	id := uuid.New()
	projID := uuid.New()
	now := time.Now().UTC()

	rows := sqlmock.NewRows([]string{
		"id", "project_id", "status",
		"requested_by", "requested_at",
		"approved_by", "approved_at",
		"tarball_r2_key", "tarball_size_bytes", "sha256", "part_count",
		"error_message", "started_at", "completed_at",
		"expires_at", "created_at", "updated_at",
	}).AddRow(
		id, projID, "ready",
		"alice@example.com", now,
		nil, nil,
		"tenant-exports/acme/xxx/part001.tar.gz", int64(12345), "sha256:abcdef", 1,
		nil, now, now,
		now.Add(14*24*time.Hour), now, now,
	)

	mock.ExpectQuery("SELECT .* FROM tenant_exports WHERE id").
		WithArgs(id).WillReturnRows(rows)

	e, err := repo.GetByID(context.Background(), id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if e.Status != types.TenantExportStatusReady {
		t.Errorf("Status = %q", e.Status)
	}
	if e.TarballSizeBytes == nil || *e.TarballSizeBytes != 12345 {
		t.Errorf("size wrong")
	}
}

func TestTenantExportRepository_GetByID_NotFound(t *testing.T) {
	db, mock := newTestDB(t)
	repo := NewTenantExportRepository(db)
	id := uuid.New()

	mock.ExpectQuery("SELECT .* FROM tenant_exports WHERE id").
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "project_id", "status",
			"requested_by", "requested_at",
			"approved_by", "approved_at",
			"tarball_r2_key", "tarball_size_bytes", "sha256", "part_count",
			"error_message", "started_at", "completed_at",
			"expires_at", "created_at", "updated_at",
		}))

	if _, err := repo.GetByID(context.Background(), id); err == nil {
		t.Errorf("expected not-found error")
	}
}

func TestTenantExportRepository_ListByProject(t *testing.T) {
	db, mock := newTestDB(t)
	repo := NewTenantExportRepository(db)

	projID := uuid.New()
	now := time.Now().UTC()

	rows := sqlmock.NewRows([]string{
		"id", "project_id", "status",
		"requested_by", "requested_at",
		"approved_by", "approved_at",
		"tarball_r2_key", "tarball_size_bytes", "sha256", "part_count",
		"error_message", "started_at", "completed_at",
		"expires_at", "created_at", "updated_at",
	}).AddRow(
		uuid.New(), projID, "ready",
		"alice@example.com", now,
		nil, nil, nil, nil, nil, 1,
		nil, nil, nil,
		nil, now, now,
	)

	mock.ExpectQuery("SELECT .* FROM tenant_exports").
		WithArgs(projID, 50).WillReturnRows(rows)

	out, err := repo.ListByProject(context.Background(), projID, 0)
	if err != nil {
		t.Fatalf("ListByProject: %v", err)
	}
	if len(out) != 1 {
		t.Errorf("got %d rows, want 1", len(out))
	}
}

func TestTenantExportRepository_Update_ToReady(t *testing.T) {
	db, mock := newTestDB(t)
	repo := NewTenantExportRepository(db)

	id := uuid.New()
	key := "tenant-exports/acme/xxx/part001.tar.gz"
	size := int64(42)
	sha := "sha256:deadbeef"
	partCount := 1
	expires := time.Now().Add(14 * 24 * time.Hour)

	mock.ExpectExec("UPDATE tenant_exports").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.Update(context.Background(), id, TenantExportUpdate{
		Status:           types.TenantExportStatusReady,
		TarballR2Key:     &key,
		TarballSizeBytes: &size,
		SHA256:           &sha,
		PartCount:        &partCount,
		ExpiresAt:        &expires,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet: %v", err)
	}
}

func TestTenantExportRepository_Update_NotFound(t *testing.T) {
	db, mock := newTestDB(t)
	repo := NewTenantExportRepository(db)

	mock.ExpectExec("UPDATE tenant_exports").
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.Update(context.Background(), uuid.New(), TenantExportUpdate{
		Status: types.TenantExportStatusFailed,
	})
	if err == nil {
		t.Errorf("expected not-found error for zero rows affected")
	}
}

func TestTenantExportStatus_IsTerminal(t *testing.T) {
	cases := map[types.TenantExportStatus]bool{
		types.TenantExportStatusPending: false,
		types.TenantExportStatusRunning: false,
		types.TenantExportStatusReady:   true,
		types.TenantExportStatusFailed:  true,
		types.TenantExportStatusExpired: true,
		types.TenantExportStatusDeleted: true,
	}
	for s, want := range cases {
		if got := s.IsTerminal(); got != want {
			t.Errorf("%s IsTerminal = %v, want %v", s, got, want)
		}
	}
}
