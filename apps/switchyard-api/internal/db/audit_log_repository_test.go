package db

import (
	"context"
	"encoding/json"
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

func newAuditLogMockDB(t *testing.T) (*AuditLogRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewAuditLogRepository(db)
	return repo, mock, func() { db.Close() }
}

var auditLogColumns = []string{
	"id", "timestamp", "actor_id", "actor_email", "actor_role",
	"action", "resource_type", "resource_id", "resource_name",
	"project_id", "environment_id", "ip_address", "user_agent",
	"outcome", "context", "metadata",
}

// --- Log ---

func TestAuditLogRepository_Log(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newAuditLogMockDB(t)
		defer cleanup()

		actorID := uuid.New()
		log := &types.AuditLog{
			ActorID:      &actorID,
			ActorEmail:   "user@example.com",
			ActorRole:    "admin",
			Action:       "deploy",
			ResourceType: "service",
			ResourceID:   uuid.New().String(),
			ResourceName: "my-service",
			IPAddress:    "192.168.1.1",
			UserAgent:    "enclii-cli/1.0",
			Outcome:      "success",
			Context:      map[string]interface{}{"commit_sha": "abc123"},
			Metadata:     map[string]interface{}{"region": "us-east-1"},
		}

		mock.ExpectExec(`INSERT INTO audit_logs`).
			WithArgs(
				sqlmock.AnyArg(), sqlmock.AnyArg(), &actorID, "user@example.com", types.Role("admin"),
				"deploy", "service", sqlmock.AnyArg(), "my-service",
				sqlmock.AnyArg(), sqlmock.AnyArg(), "192.168.1.1", "enclii-cli/1.0",
				"success", sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Log(context.Background(), log)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, log.ID)
		assert.False(t, log.Timestamp.IsZero())
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newAuditLogMockDB(t)
		defer cleanup()

		log := &types.AuditLog{
			ActorEmail: "user@test.com",
			Action:     "deploy",
			Outcome:    "failure",
			Context:    map[string]interface{}{},
			Metadata:   map[string]interface{}{},
		}

		mock.ExpectExec(`INSERT INTO audit_logs`).
			WithArgs(
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnError(fmt.Errorf("connection refused"))

		err := repo.Log(context.Background(), log)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- Query ---

func TestAuditLogRepository_Query(t *testing.T) {
	t.Run("returns logs", func(t *testing.T) {
		repo, mock, cleanup := newAuditLogMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		ctxJSON, _ := json.Marshal(map[string]interface{}{"key": "value"})
		metaJSON, _ := json.Marshal(map[string]interface{}{})
		actorID := uuid.New()

		rows := sqlmock.NewRows(auditLogColumns).
			AddRow(uuid.New(), now, &actorID, "user@test.com", "admin",
				"deploy", "service", "svc-123", "my-service",
				nil, nil, "10.0.0.1", "cli/1.0",
				"success", ctxJSON, metaJSON)

		mock.ExpectQuery(`SELECT id, timestamp, actor_id, actor_email`).
			WithArgs(50, 0).
			WillReturnRows(rows)

		results, err := repo.Query(context.Background(), map[string]interface{}{}, 50, 0)
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, "deploy", results[0].Action)
		assert.Equal(t, "user@test.com", results[0].ActorEmail)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty", func(t *testing.T) {
		repo, mock, cleanup := newAuditLogMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, timestamp, actor_id, actor_email`).
			WithArgs(50, 0).
			WillReturnRows(sqlmock.NewRows(auditLogColumns))

		results, err := repo.Query(context.Background(), map[string]interface{}{}, 50, 0)
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query error", func(t *testing.T) {
		repo, mock, cleanup := newAuditLogMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, timestamp, actor_id, actor_email`).
			WithArgs(50, 0).
			WillReturnError(fmt.Errorf("db unavailable"))

		results, err := repo.Query(context.Background(), map[string]interface{}{}, 50, 0)
		assert.Nil(t, results)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
