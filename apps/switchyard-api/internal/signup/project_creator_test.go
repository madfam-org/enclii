package signup

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
)

// TestCreateDefaultProjectForSignup_GrantsAdminAccess is the regression test for
// the orphan-project bug: provisioning used to create a Project row but no
// ProjectAccess row, leaving the project invisible to its owner (oidc.go
// loadUserProjectIDs) and uncounted by the paywall (middleware/tier.go). This
// test proves provisioning now creates BOTH the Project and an admin
// ProjectAccess row for the signup user, atomically.
func TestCreateDefaultProjectForSignup_GrantsAdminAccess(t *testing.T) {
	repos, mock, _, cleanup := newMockRepos(t)
	defer cleanup()

	mock.ExpectBegin()
	// Slug is free -> project gets inserted.
	mock.ExpectQuery(`SELECT .* FROM projects WHERE slug`).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(`INSERT INTO projects`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	// No local user row yet -> one is upserted for the signup.
	mock.ExpectQuery(`SELECT .* FROM users WHERE email`).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(`INSERT INTO users`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	// The critical assertion: an admin grant is written. Column order is
	// (id, user_id, project_id, environment_id, role, granted_by, granted_at, expires_at),
	// so the role argument (index 4) must be "admin".
	mock.ExpectExec(`INSERT INTO project_access`).
		WithArgs(
			sqlmock.AnyArg(), // id
			sqlmock.AnyArg(), // user_id
			sqlmock.AnyArg(), // project_id
			sqlmock.AnyArg(), // environment_id (nil = all environments)
			"admin",          // role
			sqlmock.AnyArg(), // granted_by
			sqlmock.AnyArg(), // granted_at
			sqlmock.AnyArg(), // expires_at
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	// Best-effort audit log runs after the transaction commits.
	mock.ExpectExec(`INSERT INTO audit_logs`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	creator := NewDefaultProjectCreator(repos)
	proj, err := creator.CreateDefaultProjectForSignup(
		context.Background(),
		uuid.New(),
		"founder@acme.io",
		"Acme Inc",
		"janua-sub-123",
	)
	if err != nil {
		t.Fatalf("CreateDefaultProjectForSignup returned error: %v", err)
	}
	if proj == nil || proj.ID == uuid.Nil {
		t.Fatalf("expected a persisted project with an ID, got %+v", proj)
	}
	// ExpectationsWereMet fails if the project_access grant (or any other write)
	// did not run — this is what pins the fix.
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations (project + admin project_access must both be written): %v", err)
	}
}

// TestCreateDefaultProjectForSignup_ReusesExistingUserForGrant proves that when
// a local users row already exists for the signup email, provisioning reuses it
// (no duplicate user insert) and grants that user admin on the new project — so
// the id we grant matches the id the OIDC login path will resolve.
func TestCreateDefaultProjectForSignup_ReusesExistingUserForGrant(t *testing.T) {
	repos, mock, _, cleanup := newMockRepos(t)
	defer cleanup()

	userID := uuid.New()
	userRows := sqlmock.NewRows([]string{
		"id", "email", "password_hash", "name", "role",
		"oidc_subject", "oidc_issuer", "active",
		"created_at", "updated_at", "last_login_at",
	}).AddRow(
		userID, "founder@acme.io", "", "Founder", "developer",
		nil, nil, true,
		time.Now(), time.Now(), nil,
	)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM projects WHERE slug`).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(`INSERT INTO projects`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	// Existing user found by email -> no INSERT INTO users expected.
	mock.ExpectQuery(`SELECT .* FROM users WHERE email`).
		WillReturnRows(userRows)
	// Grant references the resolved existing user for both grantee and grantor.
	mock.ExpectExec(`INSERT INTO project_access`).
		WithArgs(
			sqlmock.AnyArg(), // id
			userID,           // user_id -> existing user
			sqlmock.AnyArg(), // project_id
			sqlmock.AnyArg(), // environment_id
			"admin",          // role
			userID,           // granted_by -> self-serve owner
			sqlmock.AnyArg(), // granted_at
			sqlmock.AnyArg(), // expires_at
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectExec(`INSERT INTO audit_logs`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	creator := NewDefaultProjectCreator(repos)
	proj, err := creator.CreateDefaultProjectForSignup(
		context.Background(),
		uuid.New(),
		"founder@acme.io",
		"Acme Inc",
		"janua-sub-123",
	)
	if err != nil {
		t.Fatalf("CreateDefaultProjectForSignup returned error: %v", err)
	}
	if proj == nil {
		t.Fatal("expected a persisted project")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

// TestCreateDefaultProjectForSignup_RollsBackWhenGrantFails proves the writes are
// atomic: if the ProjectAccess grant fails, the whole transaction rolls back and
// the caller gets an error (rather than a committed, orphaned project).
func TestCreateDefaultProjectForSignup_RollsBackWhenGrantFails(t *testing.T) {
	repos, mock, _, cleanup := newMockRepos(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM projects WHERE slug`).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(`INSERT INTO projects`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(`SELECT .* FROM users WHERE email`).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(`INSERT INTO users`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO project_access`).
		WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	creator := NewDefaultProjectCreator(repos)
	_, err := creator.CreateDefaultProjectForSignup(
		context.Background(),
		uuid.New(),
		"founder@acme.io",
		"Acme Inc",
		"janua-sub-123",
	)
	if err == nil {
		t.Fatal("expected an error when the project_access grant fails, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations (transaction must roll back): %v", err)
	}
}
