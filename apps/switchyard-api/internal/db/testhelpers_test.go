package db

import (
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
)

// newTestDB creates a new sqlmock database connection for repository tests.
// It configures the mock to match queries by SQL content and returns both
// the *sql.DB (for passing to repository constructors) and the sqlmock.Sqlmock
// (for setting expectations).
//
// The connection is automatically closed when the test completes.
func newTestDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}

	t.Cleanup(func() {
		db.Close()
	})

	return db, mock
}

// newTestDBWithQueryMatcher creates a sqlmock database with a custom query matcher.
// Use sqlmock.QueryMatcherRegexp for regex-based matching or sqlmock.QueryMatcherEqual
// for exact string matching.
func newTestDBWithQueryMatcher(t *testing.T, matcher sqlmock.QueryMatcher) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	if err != nil {
		t.Fatalf("failed to create sqlmock with custom matcher: %v", err)
	}

	t.Cleanup(func() {
		db.Close()
	})

	return db, mock
}

// ---- Mock Row Helpers ----
// These helpers create sqlmock.Rows pre-populated with column definitions
// matching the database schema for each entity type. Use them with
// mock.ExpectQuery(...).WillReturnRows(rows) to simulate query results.

// mockProjectRows returns sqlmock.Rows configured for Project queries.
func mockProjectRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "name", "slug", "description", "owner_id",
		"created_at", "updated_at",
	})
}

// addProjectRow adds a single project row with the given values.
func addProjectRow(rows *sqlmock.Rows, id uuid.UUID, name, slug, description string, ownerID uuid.UUID) *sqlmock.Rows {
	now := time.Now()
	return rows.AddRow(id, name, slug, description, ownerID, now, now)
}

// mockServiceRows returns sqlmock.Rows configured for Service queries.
func mockServiceRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "project_id", "name", "slug", "runtime", "image_uri",
		"port", "replicas", "health", "status",
		"created_at", "updated_at",
	})
}

// addServiceRow adds a single service row with the given values.
func addServiceRow(rows *sqlmock.Rows, id, projectID uuid.UUID, name, slug string) *sqlmock.Rows {
	now := time.Now()
	return rows.AddRow(
		id, projectID, name, slug, "go", "",
		8080, 1, "unknown", "active",
		now, now,
	)
}

// mockEnvironmentRows returns sqlmock.Rows configured for Environment queries.
func mockEnvironmentRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "project_id", "name", "slug", "is_production",
		"created_at", "updated_at",
	})
}

// addEnvironmentRow adds a single environment row with the given values.
func addEnvironmentRow(rows *sqlmock.Rows, id, projectID uuid.UUID, name, slug string, isProduction bool) *sqlmock.Rows {
	now := time.Now()
	return rows.AddRow(id, projectID, name, slug, isProduction, now, now)
}

// mockReleaseRows returns sqlmock.Rows configured for Release queries.
func mockReleaseRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "service_id", "version", "image_uri", "git_sha",
		"status", "sbom", "sbom_format", "image_signature",
		"created_at", "updated_at",
	})
}

// addReleaseRow adds a single release row with the given values.
func addReleaseRow(rows *sqlmock.Rows, id, serviceID uuid.UUID, version, imageURI, gitSHA string) *sqlmock.Rows {
	now := time.Now()
	return rows.AddRow(
		id, serviceID, version, imageURI, gitSHA,
		"pending", "", "", "",
		now, now,
	)
}

// mockDeploymentRows returns sqlmock.Rows configured for Deployment queries.
func mockDeploymentRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "release_id", "environment_id", "service_id",
		"status", "health", "strategy",
		"created_at", "updated_at",
	})
}

// addDeploymentRow adds a single deployment row with the given values.
func addDeploymentRow(rows *sqlmock.Rows, id, releaseID, envID, serviceID uuid.UUID) *sqlmock.Rows {
	now := time.Now()
	return rows.AddRow(
		id, releaseID, envID, serviceID,
		"pending", "unknown", "rolling",
		now, now,
	)
}

// mockUserRows returns sqlmock.Rows configured for User queries.
func mockUserRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "email", "name", "role", "provider", "provider_id",
		"created_at", "updated_at",
	})
}

// addUserRow adds a single user row with the given values.
func addUserRow(rows *sqlmock.Rows, id uuid.UUID, email, name string) *sqlmock.Rows {
	now := time.Now()
	return rows.AddRow(id, email, name, "developer", "oidc", "", now, now)
}

// mockClusterRows returns sqlmock.Rows configured for Cluster queries.
func mockClusterRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "name", "slug", "type", "endpoint",
		"kubeconfig_secret_ref", "region", "status", "metadata",
		"created_at", "updated_at",
	})
}

// addClusterRow adds a single cluster row with the given values.
func addClusterRow(rows *sqlmock.Rows, id uuid.UUID, name, slug string) *sqlmock.Rows {
	now := time.Now()
	return rows.AddRow(
		id, name, slug, "k3s", "https://10.0.0.1:6443",
		"", "us-east-1", "ready", nil,
		now, now,
	)
}

// mockBareMetalHostRows returns sqlmock.Rows configured for BareMetalHost queries.
func mockBareMetalHostRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "name", "cluster_id", "bmc_address", "bmc_credentials_ref",
		"mac_address", "boot_mode", "state", "power_state",
		"hardware_profile", "firmware_version", "root_device_hints",
		"raid_config", "cost_per_hour_cents", "last_inspection_at",
		"created_at", "updated_at",
	})
}

// mockDriftEventRows returns sqlmock.Rows configured for DriftEvent queries.
func mockDriftEventRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "source", "resource_type", "resource_name",
		"cluster_id", "drift_details", "severity",
		"resolved", "resolved_at", "detected_at", "created_at",
	})
}

// mockCostAllocationRows returns sqlmock.Rows configured for CostAllocation queries.
func mockCostAllocationRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "bare_metal_host_id", "tenant_id",
		"allocation_percent", "period_start", "period_end",
		"cost_cents", "created_at",
	})
}

// mockAuditLogRows returns sqlmock.Rows configured for AuditLog queries.
func mockAuditLogRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "timestamp", "actor_id", "actor_email", "actor_role",
		"action", "resource_type", "resource_id", "resource_name",
		"project_id", "environment_id", "ip_address", "user_agent",
		"outcome", "context", "metadata",
	})
}

// mockApprovalRecordRows returns sqlmock.Rows configured for ApprovalRecord queries.
func mockApprovalRecordRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "deployment_id", "pr_url", "pr_number",
		"approver_email", "approver_name", "approved_at",
		"ci_status", "change_ticket_url", "compliance_receipt",
		"created_at",
	})
}

// assertExpectations verifies that all sqlmock expectations were met.
// Call this at the end of each test to ensure all expected queries were executed.
func assertExpectations(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}
