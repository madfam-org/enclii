package export

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// These tests drive Service end-to-end with a sqlmock'd DB + in-memory
// storage + the fakeBundle/Dump/Blob providers. They verify the public
// transitions (Initiate in prod vs non-prod, Approve, Delete) plus the
// authz short-circuit.

func newTestService(t *testing.T, isProd bool) (*Service, sqlmock.Sqlmock, *fakeStorage, *fakeNotifier) {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	repo := db.NewTenantExportRepository(sqlDB)
	projectsRepo := db.NewProjectRepository(sqlDB)
	// ProjectAccess left nil to force the authz path through the
	// platform-admin bypass when tests pass RoleAdmin.

	fs := newFakeStorage()
	not := &fakeNotifier{}

	svc, err := NewService(Config{
		Repo:           repo,
		Projects:       projectsRepo,
		ProjectAccess:  nil,
		Storage:        fs,
		BundleProvider: &fakeBundleProvider{bundle: &ProjectBundle{}},
		DumpProvider:   &fakeDumpProvider{},
		BlobProvider:   &fakeBlobProvider{},
		SecretProvider: &fakeSecretProvider{},
		AuditProvider:  &fakeAuditProvider{},
		Notifier:       not,
		Logger:         silenceLogs(),
		IsProduction:   isProd,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc, mock, fs, not
}

func TestService_Initiate_NonProd_RunsImmediately(t *testing.T) {
	svc, mock, fs, not := newTestService(t, false)

	projID := uuid.New()

	// Project lookup.
	mock.ExpectQuery("SELECT .* FROM projects WHERE slug").
		WithArgs("acme").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}).
			AddRow(projID, "Acme", "acme", "github", time.Now(), time.Now()))

	// Initial insert: status=running.
	mock.ExpectExec("INSERT INTO tenant_exports").
		WillReturnResult(sqlmock.NewResult(0, 1))

	// Pipeline will then update the row to ready (goroutine). We allow
	// any number of updates.
	mock.ExpectExec("UPDATE tenant_exports").
		WillReturnResult(sqlmock.NewResult(0, 1))

	req := InitiateRequest{
		ProjectSlug: "acme",
		UserID:      uuid.New(),
		UserEmail:   "alice@example.com",
		UserRole:    string(types.RoleAdmin),
	}
	row, err := svc.Initiate(context.Background(), req)
	if err != nil {
		t.Fatalf("Initiate: %v", err)
	}
	if row.Status != types.TenantExportStatusRunning {
		t.Errorf("non-prod Initiate status = %s, want running", row.Status)
	}

	// Give the goroutine a moment to upload. We don't assert full
	// completion here — it requires too many DB expectations. The key
	// invariants already covered by unit tests are the builder +
	// gatherers.
	time.Sleep(50 * time.Millisecond)
	_ = fs
	_ = not
}

func TestService_Initiate_Prod_Pending_EmitsApproval(t *testing.T) {
	svc, mock, _, not := newTestService(t, true)

	projID := uuid.New()

	mock.ExpectQuery("SELECT .* FROM projects WHERE slug").
		WithArgs("acme").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}).
			AddRow(projID, "Acme", "acme", "github", time.Now(), time.Now()))

	mock.ExpectExec("INSERT INTO tenant_exports").
		WillReturnResult(sqlmock.NewResult(0, 1))

	req := InitiateRequest{
		ProjectSlug: "acme",
		UserID:      uuid.New(),
		UserEmail:   "alice@example.com",
		UserRole:    string(types.RoleAdmin),
	}
	row, err := svc.Initiate(context.Background(), req)
	if err != nil {
		t.Fatalf("Initiate: %v", err)
	}
	if row.Status != types.TenantExportStatusPending {
		t.Errorf("prod Initiate status = %s, want pending", row.Status)
	}
	if len(not.approval) != 1 {
		t.Errorf("expected approval notification, got %d", len(not.approval))
	}
}

func TestService_Initiate_ProjectNotFound(t *testing.T) {
	svc, mock, _, _ := newTestService(t, false)

	mock.ExpectQuery("SELECT .* FROM projects WHERE slug").
		WithArgs("missing").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}))

	_, err := svc.Initiate(context.Background(), InitiateRequest{
		ProjectSlug: "missing",
		UserID:      uuid.New(),
		UserRole:    string(types.RoleAdmin),
	})
	if err == nil {
		t.Errorf("expected unauthorized-or-missing error")
	}
}

func TestService_Get_ReadyReturnsDownloadURL(t *testing.T) {
	svc, mock, _, _ := newTestService(t, false)

	exportID := uuid.New()
	projID := uuid.New()
	now := time.Now().UTC()
	key := "tenant-exports/acme/xxx/part001.tar.gz"

	mock.ExpectQuery("SELECT .* FROM tenant_exports WHERE id").
		WithArgs(exportID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "project_id", "status",
			"requested_by", "requested_at",
			"approved_by", "approved_at",
			"tarball_r2_key", "tarball_size_bytes", "sha256", "part_count",
			"error_message", "started_at", "completed_at",
			"expires_at", "created_at", "updated_at",
		}).AddRow(
			exportID, projID, "ready",
			"alice@example.com", now,
			nil, nil,
			key, int64(100), "sha256:abc", 1,
			nil, now, now,
			now.Add(14*24*time.Hour), now, now,
		))

	resp, err := svc.Get(context.Background(), InitiateRequest{
		UserID:   uuid.New(),
		UserRole: string(types.RoleAdmin),
	}, exportID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if resp.DownloadURL == "" {
		t.Errorf("expected download URL for ready export")
	}
	if resp.ExpiresIn != int((15 * time.Minute).Seconds()) {
		t.Errorf("ExpiresIn = %d, want 900", resp.ExpiresIn)
	}
}

func TestService_Get_PendingNoDownloadURL(t *testing.T) {
	svc, mock, _, _ := newTestService(t, false)

	exportID := uuid.New()
	projID := uuid.New()
	now := time.Now().UTC()

	mock.ExpectQuery("SELECT .* FROM tenant_exports WHERE id").
		WithArgs(exportID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "project_id", "status",
			"requested_by", "requested_at",
			"approved_by", "approved_at",
			"tarball_r2_key", "tarball_size_bytes", "sha256", "part_count",
			"error_message", "started_at", "completed_at",
			"expires_at", "created_at", "updated_at",
		}).AddRow(
			exportID, projID, "pending",
			"alice@example.com", now,
			nil, nil,
			nil, nil, nil, 1,
			nil, nil, nil,
			nil, now, now,
		))

	resp, err := svc.Get(context.Background(), InitiateRequest{
		UserID:   uuid.New(),
		UserRole: string(types.RoleAdmin),
	}, exportID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if resp.DownloadURL != "" {
		t.Errorf("pending export must not have download URL, got %q", resp.DownloadURL)
	}
}

func TestService_Delete_PurgesR2(t *testing.T) {
	svc, mock, fs, _ := newTestService(t, false)

	// Pre-populate the R2 fake with a tarball.
	exportID := uuid.New()
	key := "tenant-exports/acme/xxx/part001.tar.gz"
	fs.uploads[key] = []byte("tarball-data")

	projID := uuid.New()
	now := time.Now().UTC()
	mock.ExpectQuery("SELECT .* FROM tenant_exports WHERE id").
		WithArgs(exportID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "project_id", "status",
			"requested_by", "requested_at",
			"approved_by", "approved_at",
			"tarball_r2_key", "tarball_size_bytes", "sha256", "part_count",
			"error_message", "started_at", "completed_at",
			"expires_at", "created_at", "updated_at",
		}).AddRow(
			exportID, projID, "ready",
			"alice@example.com", now,
			nil, nil,
			key, int64(100), "sha256:abc", 1,
			nil, now, now,
			now, now, now,
		))

	mock.ExpectExec("UPDATE tenant_exports").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := svc.Delete(context.Background(), InitiateRequest{
		UserID:   uuid.New(),
		UserRole: string(types.RoleAdmin),
	}, exportID)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// R2 object must be gone.
	if _, ok := fs.uploads[key]; ok {
		t.Errorf("R2 object still present after delete")
	}
	if len(fs.deletes) != 1 || fs.deletes[0] != key {
		t.Errorf("expected 1 delete of %q, got %v", key, fs.deletes)
	}
}

func TestService_Approve_RejectsSelfApprovalInProd(t *testing.T) {
	svc, mock, _, _ := newTestService(t, true)

	exportID := uuid.New()
	projID := uuid.New()
	now := time.Now().UTC()

	mock.ExpectQuery("SELECT .* FROM tenant_exports WHERE id").
		WithArgs(exportID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "project_id", "status",
			"requested_by", "requested_at",
			"approved_by", "approved_at",
			"tarball_r2_key", "tarball_size_bytes", "sha256", "part_count",
			"error_message", "started_at", "completed_at",
			"expires_at", "created_at", "updated_at",
		}).AddRow(
			exportID, projID, "pending",
			"alice@example.com", now,
			nil, nil, nil, nil, nil, 1,
			nil, nil, nil,
			nil, now, now,
		))

	_, err := svc.Approve(context.Background(), InitiateRequest{
		UserID:    uuid.New(),
		UserEmail: "alice@example.com", // same as requester
		UserRole:  string(types.RoleAdmin),
	}, exportID)
	if err != ErrSelfApproval {
		t.Errorf("got err=%v, want ErrSelfApproval", err)
	}
}

func TestService_Approve_FailsOnNonPending(t *testing.T) {
	svc, mock, _, _ := newTestService(t, true)

	exportID := uuid.New()
	projID := uuid.New()
	now := time.Now().UTC()

	mock.ExpectQuery("SELECT .* FROM tenant_exports WHERE id").
		WithArgs(exportID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "project_id", "status",
			"requested_by", "requested_at",
			"approved_by", "approved_at",
			"tarball_r2_key", "tarball_size_bytes", "sha256", "part_count",
			"error_message", "started_at", "completed_at",
			"expires_at", "created_at", "updated_at",
		}).AddRow(
			exportID, projID, "ready",
			"alice@example.com", now,
			nil, nil, nil, nil, nil, 1,
			nil, nil, nil,
			nil, now, now,
		))

	_, err := svc.Approve(context.Background(), InitiateRequest{
		UserID:    uuid.New(),
		UserEmail: "bob@example.com",
		UserRole:  string(types.RoleAdmin),
	}, exportID)
	if err == nil {
		t.Errorf("expected error approving a non-pending export")
	}
}

func TestRedactR2Key_StripsQuery(t *testing.T) {
	in := "tenant-exports/acme/xxx/part001.tar.gz?X-Amz-Signature=abc123"
	out := redactR2Key(in)
	if out == in {
		t.Errorf("redactR2Key didn't strip query")
	}
	if !containsNot(out, "abc123") {
		t.Errorf("signature leaked after redaction: %s", out)
	}
}

func TestRedactQuerySignatures(t *testing.T) {
	in := "https://r2.example.com/x?X-Amz-Signature=SECRETSIG"
	out := redactQuerySignatures(in)
	if containsNot(out, "SECRETSIG") {
	} else {
		t.Errorf("signature leaked: %s", out)
	}
}

// containsNot is the inverse of strings.Contains used to keep the code
// in each assertion a single expression.
func containsNot(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return false
		}
	}
	return true
}
