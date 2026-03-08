package db

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- helpers ---

func newRotationAuditLogMockDB(t *testing.T) (*RotationAuditLogRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewRotationAuditLogRepository(db)
	return repo, mock, func() { db.Close() }
}

// --- Create ---

func TestRotationAuditLogRepository_Create(t *testing.T) {
	t.Run("invalid type returns error", func(t *testing.T) {
		repo, _, cleanup := newRotationAuditLogMockDB(t)
		defer cleanup()

		// Pass a non-rotationLog type to trigger the type assertion error
		err := repo.Create(context.Background(), "not a rotation log")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid log type")
	})
}

// --- GetByServiceID ---

func TestRotationAuditLogRepository_GetByServiceID(t *testing.T) {
	rotationLogColumns := []string{
		"id", "event_id", "service_id", "service_name", "environment",
		"secret_name", "secret_path", "old_version", "new_version", "status",
		"started_at", "completed_at", "duration_ms", "rollout_strategy",
		"pods_restarted", "error", "changed_by", "triggered_by", "created_at",
	}

	t.Run("returns logs", func(t *testing.T) {
		repo, mock, cleanup := newRotationAuditLogMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)

		rows := sqlmock.NewRows(rotationLogColumns).
			AddRow(
				uuid.New(), uuid.New(), svcID, "my-service", "production",
				"DB_PASSWORD", "/secrets/db-pass", 1, 2, "completed",
				now, &now,
				sql.NullInt64{Int64: 1500, Valid: true},
				sql.NullString{String: "rolling", Valid: true},
				3,
				sql.NullString{},
				sql.NullString{String: "admin@test.com", Valid: true},
				"automated",
				now,
			)

		mock.ExpectQuery(`SELECT id, event_id, service_id, service_name, environment`).
			WithArgs(svcID, 50).
			WillReturnRows(rows)

		results, err := repo.GetByServiceID(context.Background(), svcID, 0)
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty", func(t *testing.T) {
		repo, mock, cleanup := newRotationAuditLogMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		mock.ExpectQuery(`SELECT id, event_id, service_id`).
			WithArgs(svcID, 10).
			WillReturnRows(sqlmock.NewRows(rotationLogColumns))

		results, err := repo.GetByServiceID(context.Background(), svcID, 10)
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query error", func(t *testing.T) {
		repo, mock, cleanup := newRotationAuditLogMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		mock.ExpectQuery(`SELECT id, event_id, service_id`).
			WithArgs(svcID, 50).
			WillReturnError(fmt.Errorf("connection timeout"))

		results, err := repo.GetByServiceID(context.Background(), svcID, 0)
		assert.Nil(t, results)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("default limit applied when zero", func(t *testing.T) {
		repo, mock, cleanup := newRotationAuditLogMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		// When limit is 0, it should default to 50
		mock.ExpectQuery(`SELECT id, event_id, service_id`).
			WithArgs(svcID, 50).
			WillReturnRows(sqlmock.NewRows(rotationLogColumns))

		results, err := repo.GetByServiceID(context.Background(), svcID, -1)
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
