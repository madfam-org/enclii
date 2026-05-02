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

	// Regression test for the `pq: invalid input syntax for type inet: ""`
	// crash hit on app.enclii.dev /v1/audit. When the request has no
	// resolvable client IP, the writer must bind nil (SQL NULL) — not "".
	t.Run("empty ip bound as nil", func(t *testing.T) {
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
			IPAddress:    "", // <-- the repro case
			UserAgent:    "enclii-cli/1.0",
			Outcome:      "success",
			Context:      map[string]interface{}{},
			Metadata:     map[string]interface{}{},
		}

		// Position 12 (1-indexed) is ip_address per the INSERT column
		// list. We pin it to nil and leave the rest loose.
		mock.ExpectExec(`INSERT INTO audit_logs`).
			WithArgs(
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), nil, sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Log(context.Background(), log)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("whitespace ip bound as nil", func(t *testing.T) {
		repo, mock, cleanup := newAuditLogMockDB(t)
		defer cleanup()

		log := &types.AuditLog{
			ActorEmail: "user@example.com",
			ActorRole:  "admin",
			Action:     "deploy",
			IPAddress:  "   ",
			Outcome:    "success",
			Context:    map[string]interface{}{},
			Metadata:   map[string]interface{}{},
		}

		mock.ExpectExec(`INSERT INTO audit_logs`).
			WithArgs(
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), nil, sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Log(context.Background(), log)
		assert.NoError(t, err)
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

	t.Run("null ip scanned as empty string", func(t *testing.T) {
		repo, mock, cleanup := newAuditLogMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		ctxJSON, _ := json.Marshal(map[string]interface{}{})
		metaJSON, _ := json.Marshal(map[string]interface{}{})
		actorID := uuid.New()

		rows := sqlmock.NewRows(auditLogColumns).
			AddRow(uuid.New(), now, &actorID, "user@test.com", "admin",
				"deploy", "service", "svc-123", "my-service",
				nil, nil, nil, "cli/1.0", // <-- ip_address is NULL
				"success", ctxJSON, metaJSON)

		mock.ExpectQuery(`SELECT id, timestamp, actor_id, actor_email`).
			WithArgs(50, 0).
			WillReturnRows(rows)

		results, err := repo.Query(context.Background(), map[string]interface{}{}, 50, 0)
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, "", results[0].IPAddress)
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

// --- QueryByTeam (XC-2 Round 5 enforcement) ---

func TestAuditLogRepository_QueryByTeam(t *testing.T) {
	t.Run("team match returns rows owned by team", func(t *testing.T) {
		repo, mock, cleanup := newAuditLogMockDB(t)
		defer cleanup()

		teamID := uuid.New()
		actorID := uuid.New()
		projID := uuid.New()
		now := time.Now()
		emptyJSON, _ := json.Marshal(map[string]any{})

		mock.ExpectQuery(`(?s)WHERE \(\s*project_id IN \(SELECT id FROM projects WHERE team_id = \$1\)\s+OR acting_on_behalf_of_team_id = \$1\s*\)`).
			WithArgs(teamID, 50, 0).
			WillReturnRows(sqlmock.NewRows(auditLogColumns).AddRow(
				uuid.New(), now, &actorID, "tenant@x.com", "developer",
				"deploy", "service", uuid.New().String(), "api",
				&projID, nil, nil, "enclii",
				"success", emptyJSON, emptyJSON,
			))

		out, err := repo.QueryByTeam(context.Background(), teamID, map[string]interface{}{}, 50, 0)
		require.NoError(t, err)
		require.Len(t, out, 1)
		assert.Equal(t, "deploy", out[0].Action)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("team mismatch returns empty", func(t *testing.T) {
		repo, mock, cleanup := newAuditLogMockDB(t)
		defer cleanup()

		teamID := uuid.New()
		mock.ExpectQuery(`(?s)WHERE \(\s*project_id IN \(SELECT id FROM projects WHERE team_id = \$1\)`).
			WithArgs(teamID, 50, 0).
			WillReturnRows(sqlmock.NewRows(auditLogColumns))

		out, err := repo.QueryByTeam(context.Background(), teamID, map[string]interface{}{}, 50, 0)
		require.NoError(t, err)
		assert.Empty(t, out)
	})

	t.Run("no rows", func(t *testing.T) {
		repo, mock, cleanup := newAuditLogMockDB(t)
		defer cleanup()

		teamID := uuid.New()
		mock.ExpectQuery(`(?s)WHERE \(\s*project_id IN`).
			WithArgs(teamID, 50, 0).
			WillReturnRows(sqlmock.NewRows(auditLogColumns))

		out, err := repo.QueryByTeam(context.Background(), teamID, map[string]interface{}{}, 50, 0)
		require.NoError(t, err)
		assert.Empty(t, out)
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newAuditLogMockDB(t)
		defer cleanup()

		teamID := uuid.New()
		mock.ExpectQuery(`(?s)WHERE \(\s*project_id IN`).
			WithArgs(teamID, 50, 0).
			WillReturnError(fmt.Errorf("connection refused"))

		_, err := repo.QueryByTeam(context.Background(), teamID, map[string]interface{}{}, 50, 0)
		require.Error(t, err)
	})
}
