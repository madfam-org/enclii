package db

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- helpers ---

func newAPITokenMockDB(t *testing.T) (*APITokenRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewAPITokenRepository(db)
	return repo, mock, func() { db.Close() }
}

var apiTokenListColumns = []string{
	"id", "user_id", "name", "prefix", "scopes",
	"expires_at", "last_used_at", "last_used_ip",
	"revoked", "revoked_at", "created_at", "updated_at",
}

// --- Create ---

func TestAPITokenRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newAPITokenMockDB(t)
		defer cleanup()

		userID := uuid.New()

		mock.ExpectQuery(`INSERT INTO api_tokens`).
			WithArgs(
				sqlmock.AnyArg(), userID, "my-token", sqlmock.AnyArg(),
				sqlmock.AnyArg(), pq.Array([]string{"read", "write"}),
				sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))

		resp, err := repo.Create(context.Background(), userID, "my-token", []string{"read", "write"}, nil)
		assert.NoError(t, err)
		assert.NotNil(t, resp)
		assert.NotEmpty(t, resp.Token)
		assert.Equal(t, "my-token", resp.Name)
		assert.Contains(t, resp.Token, "enclii_")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newAPITokenMockDB(t)
		defer cleanup()

		userID := uuid.New()
		mock.ExpectQuery(`INSERT INTO api_tokens`).
			WithArgs(
				sqlmock.AnyArg(), userID, "fail", sqlmock.AnyArg(),
				sqlmock.AnyArg(), pq.Array([]string{}),
				sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnError(fmt.Errorf("connection refused"))

		resp, err := repo.Create(context.Background(), userID, "fail", []string{}, nil)
		assert.Error(t, err)
		assert.Nil(t, resp)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByID ---

func TestAPITokenRepository_GetByID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newAPITokenMockDB(t)
		defer cleanup()

		id := uuid.New()
		userID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)

		mock.ExpectQuery(`SELECT id, user_id, name, prefix, scopes, expires_at, last_used_at, last_used_ip`).
			WithArgs(id).
			WillReturnRows(sqlmock.NewRows(apiTokenListColumns).
				AddRow(id, userID, "test-token", "enclii_abc", pq.Array([]string{"read"}),
					nil, nil, sql.NullString{},
					false, nil, now, now))

		result, err := repo.GetByID(context.Background(), id)
		assert.NoError(t, err)
		assert.Equal(t, id, result.ID)
		assert.Equal(t, userID, result.UserID)
		assert.Equal(t, "test-token", result.Name)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newAPITokenMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, user_id, name, prefix, scopes`).
			WithArgs(id).
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- ListByUser ---

func TestAPITokenRepository_ListByUser(t *testing.T) {
	t.Run("returns tokens", func(t *testing.T) {
		repo, mock, cleanup := newAPITokenMockDB(t)
		defer cleanup()

		userID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)

		rows := sqlmock.NewRows(apiTokenListColumns).
			AddRow(uuid.New(), userID, "token-1", "enclii_aaa", pq.Array([]string{"read"}),
				nil, nil, sql.NullString{}, false, nil, now, now).
			AddRow(uuid.New(), userID, "token-2", "enclii_bbb", pq.Array([]string{"write"}),
				nil, nil, sql.NullString{}, true, &now, now, now)

		mock.ExpectQuery(`SELECT id, user_id, name, prefix, scopes`).
			WithArgs(userID).
			WillReturnRows(rows)

		results, err := repo.ListByUser(context.Background(), userID)
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.Equal(t, "token-1", results[0].Name)
		assert.Equal(t, "token-2", results[1].Name)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty", func(t *testing.T) {
		repo, mock, cleanup := newAPITokenMockDB(t)
		defer cleanup()

		userID := uuid.New()
		mock.ExpectQuery(`SELECT id, user_id, name, prefix, scopes`).
			WithArgs(userID).
			WillReturnRows(sqlmock.NewRows(apiTokenListColumns))

		results, err := repo.ListByUser(context.Background(), userID)
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- Revoke ---

func TestAPITokenRepository_Revoke(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newAPITokenMockDB(t)
		defer cleanup()

		id := uuid.New()
		userID := uuid.New()

		mock.ExpectExec(`UPDATE api_tokens`).
			WithArgs(sqlmock.AnyArg(), id, userID).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Revoke(context.Background(), id, userID)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found or already revoked", func(t *testing.T) {
		repo, mock, cleanup := newAPITokenMockDB(t)
		defer cleanup()

		id := uuid.New()
		userID := uuid.New()

		mock.ExpectExec(`UPDATE api_tokens`).
			WithArgs(sqlmock.AnyArg(), id, userID).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.Revoke(context.Background(), id, userID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found or already revoked")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- Delete ---

func TestAPITokenRepository_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newAPITokenMockDB(t)
		defer cleanup()

		id := uuid.New()
		userID := uuid.New()

		mock.ExpectExec(`DELETE FROM api_tokens`).
			WithArgs(id, userID).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Delete(context.Background(), id, userID)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newAPITokenMockDB(t)
		defer cleanup()

		id := uuid.New()
		userID := uuid.New()

		mock.ExpectExec(`DELETE FROM api_tokens`).
			WithArgs(id, userID).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.Delete(context.Background(), id, userID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "token not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- CountByUser ---

func TestAPITokenRepository_CountByUser(t *testing.T) {
	t.Run("returns count", func(t *testing.T) {
		repo, mock, cleanup := newAPITokenMockDB(t)
		defer cleanup()

		userID := uuid.New()
		mock.ExpectQuery(`SELECT COUNT`).
			WithArgs(userID).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

		count, err := repo.CountByUser(context.Background(), userID)
		assert.NoError(t, err)
		assert.Equal(t, 3, count)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
