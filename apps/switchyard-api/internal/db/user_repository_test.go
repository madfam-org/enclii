package db

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- helpers ---

func newUserMockDB(t *testing.T) (*UserRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewUserRepository(db)
	return repo, mock, func() { db.Close() }
}

var userColumns = []string{
	"id", "email", "password_hash", "name", "role",
	"oidc_subject", "oidc_issuer", "active",
	"created_at", "updated_at", "last_login_at",
}

func userRow(u *types.User) *sqlmock.Rows {
	return sqlmock.NewRows(userColumns).
		AddRow(
			u.ID, u.Email, u.PasswordHash, u.Name, u.Role,
			u.OIDCSubject, u.OIDCIssuer, u.Active,
			u.CreatedAt, u.UpdatedAt, u.LastLoginAt,
		)
}

// --- Create ---

func TestUserRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newUserMockDB(t)
		defer cleanup()

		oidcSub := "sub-123"
		oidcIss := "https://auth.madfam.io"
		user := &types.User{
			Email:        "dev@enclii.dev",
			PasswordHash: "hashed",
			Name:         "Developer",
			Role:         "developer",
			OIDCSubject:  &oidcSub,
			OIDCIssuer:   &oidcIss,
			Active:       true,
		}

		mock.ExpectExec(`INSERT INTO users`).
			WithArgs(
				sqlmock.AnyArg(), "dev@enclii.dev", "hashed", "Developer", "developer",
				&oidcSub, &oidcIss, true, sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(context.Background(), user)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, user.ID, "ID should be assigned")
		assert.False(t, user.CreatedAt.IsZero(), "CreatedAt should be set")
		assert.False(t, user.UpdatedAt.IsZero(), "UpdatedAt should be set")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("duplicate email error", func(t *testing.T) {
		repo, mock, cleanup := newUserMockDB(t)
		defer cleanup()

		user := &types.User{
			Email:  "dup@enclii.dev",
			Name:   "Dup",
			Role:   "developer",
			Active: true,
		}

		mock.ExpectExec(`INSERT INTO users`).
			WithArgs(
				sqlmock.AnyArg(), "dup@enclii.dev", "", "Dup", "developer",
				(*string)(nil), (*string)(nil), true, sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnError(fmt.Errorf("pq: duplicate key value violates unique constraint \"users_email_key\""))

		err := repo.Create(context.Background(), user)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate key")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newUserMockDB(t)
		defer cleanup()

		user := &types.User{Email: "fail@enclii.dev", Name: "Fail", Role: "viewer", Active: true}

		mock.ExpectExec(`INSERT INTO users`).
			WithArgs(
				sqlmock.AnyArg(), "fail@enclii.dev", "", "Fail", "viewer",
				(*string)(nil), (*string)(nil), true, sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnError(fmt.Errorf("connection refused"))

		err := repo.Create(context.Background(), user)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByID ---

func TestUserRepository_GetByID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newUserMockDB(t)
		defer cleanup()

		id := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		oidcSub := "sub-abc"
		oidcIss := "https://auth.madfam.io"
		expected := &types.User{
			ID: id, Email: "user@enclii.dev", PasswordHash: "hash",
			Name: "User", Role: "developer", OIDCSubject: &oidcSub, OIDCIssuer: &oidcIss,
			Active: true, CreatedAt: now, UpdatedAt: now, LastLoginAt: &now,
		}

		mock.ExpectQuery(`SELECT id, email, password_hash, name, role, oidc_subject, oidc_issuer, active, created_at, updated_at, last_login_at`).
			WithArgs(id).
			WillReturnRows(userRow(expected))

		result, err := repo.GetByID(context.Background(), id)
		assert.NoError(t, err)
		assert.Equal(t, expected.ID, result.ID)
		assert.Equal(t, expected.Email, result.Email)
		assert.Equal(t, expected.Name, result.Name)
		assert.Equal(t, expected.Role, result.Role)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newUserMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, email, password_hash, name, role, oidc_subject, oidc_issuer, active, created_at, updated_at, last_login_at`).
			WithArgs(id).
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByEmail ---

func TestUserRepository_GetByEmail(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newUserMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		expected := &types.User{
			ID: uuid.New(), Email: "admin@madfam.io", Name: "Admin",
			Role: "admin", Active: true, CreatedAt: now, UpdatedAt: now,
		}

		mock.ExpectQuery(`SELECT id, email, password_hash, name, role, oidc_subject, oidc_issuer, active, created_at, updated_at, last_login_at`).
			WithArgs("admin@madfam.io").
			WillReturnRows(userRow(expected))

		result, err := repo.GetByEmail(context.Background(), "admin@madfam.io")
		assert.NoError(t, err)
		assert.Equal(t, "admin@madfam.io", result.Email)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newUserMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, email, password_hash, name, role, oidc_subject, oidc_issuer, active, created_at, updated_at, last_login_at`).
			WithArgs("nobody@enclii.dev").
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByEmail(context.Background(), "nobody@enclii.dev")
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByOIDCIdentity ---

func TestUserRepository_GetByOIDCIdentity(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newUserMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		oidcSub := "sub-oidc-1"
		oidcIss := "https://auth.madfam.io"
		expected := &types.User{
			ID: uuid.New(), Email: "oidc@enclii.dev", Name: "OIDC User",
			Role: "developer", OIDCSubject: &oidcSub, OIDCIssuer: &oidcIss,
			Active: true, CreatedAt: now, UpdatedAt: now,
		}

		mock.ExpectQuery(`SELECT id, email, password_hash, name, role, oidc_subject, oidc_issuer, active, created_at, updated_at, last_login_at`).
			WithArgs("https://auth.madfam.io", "sub-oidc-1").
			WillReturnRows(userRow(expected))

		result, err := repo.GetByOIDCIdentity(context.Background(), "https://auth.madfam.io", "sub-oidc-1")
		assert.NoError(t, err)
		assert.Equal(t, expected.Email, result.Email)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newUserMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, email, password_hash, name, role, oidc_subject, oidc_issuer, active, created_at, updated_at, last_login_at`).
			WithArgs("https://unknown.io", "sub-none").
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByOIDCIdentity(context.Background(), "https://unknown.io", "sub-none")
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- Update ---

func TestUserRepository_Update(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newUserMockDB(t)
		defer cleanup()

		id := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		user := &types.User{
			ID: id, Email: "updated@enclii.dev", Name: "Updated",
			Role: "admin", Active: true, CreatedAt: now, UpdatedAt: now,
		}

		mock.ExpectExec(`UPDATE users`).
			WithArgs(
				"updated@enclii.dev", "", "Updated", "admin",
				(*string)(nil), (*string)(nil), true, sqlmock.AnyArg(), (*time.Time)(nil), id,
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Update(context.Background(), user)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newUserMockDB(t)
		defer cleanup()

		user := &types.User{ID: uuid.New(), Email: "fail@enclii.dev", Name: "Fail", Role: "viewer", Active: true}

		mock.ExpectExec(`UPDATE users`).
			WithArgs(
				"fail@enclii.dev", "", "Fail", "viewer",
				(*string)(nil), (*string)(nil), true, sqlmock.AnyArg(), (*time.Time)(nil), user.ID,
			).
			WillReturnError(fmt.Errorf("connection reset"))

		err := repo.Update(context.Background(), user)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- UpdateLastLogin ---

func TestUserRepository_UpdateLastLogin(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newUserMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE users SET last_login_at`).
			WithArgs(id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateLastLogin(context.Background(), id)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- List ---

func TestUserRepository_List(t *testing.T) {
	t.Run("multiple results", func(t *testing.T) {
		repo, mock, cleanup := newUserMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		rows := sqlmock.NewRows(userColumns).
			AddRow(uuid.New(), "alice@enclii.dev", "", "Alice", "admin", nil, nil, true, now, now, nil).
			AddRow(uuid.New(), "bob@enclii.dev", "", "Bob", "developer", nil, nil, true, now.Add(-time.Hour), now.Add(-time.Hour), nil)

		mock.ExpectQuery(`SELECT id, email, password_hash, name, role, oidc_subject, oidc_issuer, active, created_at, updated_at, last_login_at`).
			WillReturnRows(rows)

		results, err := repo.List(context.Background())
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.Equal(t, "alice@enclii.dev", results[0].Email)
		assert.Equal(t, "bob@enclii.dev", results[1].Email)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty", func(t *testing.T) {
		repo, mock, cleanup := newUserMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, email, password_hash, name, role, oidc_subject, oidc_issuer, active, created_at, updated_at, last_login_at`).
			WillReturnRows(sqlmock.NewRows(userColumns))

		results, err := repo.List(context.Background())
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query error", func(t *testing.T) {
		repo, mock, cleanup := newUserMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, email, password_hash, name, role, oidc_subject, oidc_issuer, active, created_at, updated_at, last_login_at`).
			WillReturnError(fmt.Errorf("db unavailable"))

		results, err := repo.List(context.Background())
		assert.Nil(t, results)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
