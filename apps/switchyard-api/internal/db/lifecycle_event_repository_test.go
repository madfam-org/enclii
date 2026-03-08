package db

import (
	"context"
	"database/sql"
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

func newLifecycleMockDB(t *testing.T) (*LifecycleEventRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewLifecycleEventRepository(db)
	return repo, mock, func() { db.Close() }
}

var lifecycleColumns = []string{
	"id", "deployment_id", "release_id", "ci_run_id", "project_id", "service_id",
	"repo_full_name", "commit_sha", "branch", "ref", "target_env",
	"event_type", "source", "message", "metadata", "created_at",
}

func addLifecycleRow(rows *sqlmock.Rows, e *types.DeploymentLifecycleEvent) *sqlmock.Rows {
	metadataJSON, _ := json.Marshal(e.Metadata)

	var deploymentID, releaseID, ciRunID, projectID, serviceID sql.NullString
	if e.DeploymentID != nil {
		deploymentID = sql.NullString{String: e.DeploymentID.String(), Valid: true}
	}
	if e.ReleaseID != nil {
		releaseID = sql.NullString{String: e.ReleaseID.String(), Valid: true}
	}
	if e.CIRunID != nil {
		ciRunID = sql.NullString{String: e.CIRunID.String(), Valid: true}
	}
	if e.ProjectID != nil {
		projectID = sql.NullString{String: e.ProjectID.String(), Valid: true}
	}
	if e.ServiceID != nil {
		serviceID = sql.NullString{String: e.ServiceID.String(), Valid: true}
	}

	var targetEnv, message sql.NullString
	if e.TargetEnv != nil {
		targetEnv = sql.NullString{String: *e.TargetEnv, Valid: true}
	}
	if e.Message != nil {
		message = sql.NullString{String: *e.Message, Valid: true}
	}

	return rows.AddRow(
		e.ID, deploymentID, releaseID, ciRunID, projectID, serviceID,
		e.RepoFullName, e.CommitSHA, e.Branch, e.Ref, targetEnv,
		e.EventType, e.Source, message, metadataJSON, e.CreatedAt,
	)
}

func sampleLifecycleEvent() *types.DeploymentLifecycleEvent {
	now := time.Now().Truncate(time.Microsecond)
	projID := uuid.New()
	svcID := uuid.New()
	targetEnv := "production"
	msg := "Build completed successfully"
	return &types.DeploymentLifecycleEvent{
		ID:           uuid.New(),
		ProjectID:    &projID,
		ServiceID:    &svcID,
		RepoFullName: "madfam-org/enclii",
		CommitSHA:    "abc123def456",
		Branch:       "main",
		Ref:          "refs/heads/main",
		TargetEnv:    &targetEnv,
		EventType:    types.LifecycleBuildSucceeded,
		Source:       types.SourceCICallback,
		Message:      &msg,
		Metadata:     map[string]interface{}{"image": "ghcr.io/test:latest"},
		CreatedAt:    now,
	}
}

// --- Create ---

func TestLifecycleEventRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newLifecycleMockDB(t)
		defer cleanup()

		projID := uuid.New()
		targetEnv := "staging"
		msg := "Push received"
		event := &types.DeploymentLifecycleEvent{
			ProjectID:    &projID,
			RepoFullName: "madfam-org/enclii",
			CommitSHA:    "abc123",
			Branch:       "main",
			Ref:          "refs/heads/main",
			TargetEnv:    &targetEnv,
			EventType:    types.LifecyclePushReceived,
			Source:       types.SourceGitHubWebhook,
			Message:      &msg,
			Metadata:     map[string]interface{}{"sender": "github"},
		}

		mock.ExpectExec(`INSERT INTO deployment_lifecycle_events`).
			WithArgs(
				sqlmock.AnyArg(), (*uuid.UUID)(nil), (*uuid.UUID)(nil), (*uuid.UUID)(nil),
				&projID, (*uuid.UUID)(nil),
				"madfam-org/enclii", "abc123", "main", "refs/heads/main", &targetEnv,
				types.LifecyclePushReceived, types.SourceGitHubWebhook, &msg,
				sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(context.Background(), event)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, event.ID, "ID should be assigned")
		assert.False(t, event.CreatedAt.IsZero(), "CreatedAt should be set")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("preserves existing ID", func(t *testing.T) {
		repo, mock, cleanup := newLifecycleMockDB(t)
		defer cleanup()

		existingID := uuid.New()
		existingTime := time.Now().Add(-time.Hour)
		event := &types.DeploymentLifecycleEvent{
			ID:           existingID,
			RepoFullName: "madfam-org/test",
			CommitSHA:    "def456",
			Branch:       "develop",
			Ref:          "refs/heads/develop",
			EventType:    types.LifecycleBuildStarted,
			Source:       types.SourcePlatform,
			CreatedAt:    existingTime,
		}

		mock.ExpectExec(`INSERT INTO deployment_lifecycle_events`).
			WithArgs(
				existingID, (*uuid.UUID)(nil), (*uuid.UUID)(nil), (*uuid.UUID)(nil),
				(*uuid.UUID)(nil), (*uuid.UUID)(nil),
				"madfam-org/test", "def456", "develop", "refs/heads/develop", (*string)(nil),
				types.LifecycleBuildStarted, types.SourcePlatform, (*string)(nil),
				sqlmock.AnyArg(), existingTime,
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(context.Background(), event)
		assert.NoError(t, err)
		assert.Equal(t, existingID, event.ID, "existing ID should be preserved")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newLifecycleMockDB(t)
		defer cleanup()

		event := &types.DeploymentLifecycleEvent{
			RepoFullName: "madfam-org/fail",
			CommitSHA:    "fail123",
			Branch:       "main",
			Ref:          "refs/heads/main",
			EventType:    types.LifecycleDeployFailed,
			Source:       types.SourceManual,
		}

		mock.ExpectExec(`INSERT INTO deployment_lifecycle_events`).
			WithArgs(
				sqlmock.AnyArg(), (*uuid.UUID)(nil), (*uuid.UUID)(nil), (*uuid.UUID)(nil),
				(*uuid.UUID)(nil), (*uuid.UUID)(nil),
				"madfam-org/fail", "fail123", "main", "refs/heads/main", (*string)(nil),
				types.LifecycleDeployFailed, types.SourceManual, (*string)(nil),
				sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnError(fmt.Errorf("connection refused"))

		err := repo.Create(context.Background(), event)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByCommit ---

func TestLifecycleEventRepository_GetByCommit(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newLifecycleMockDB(t)
		defer cleanup()

		expected := sampleLifecycleEvent()
		rows := sqlmock.NewRows(lifecycleColumns)
		addLifecycleRow(rows, expected)

		mock.ExpectQuery(`SELECT id, deployment_id, release_id, ci_run_id, project_id, service_id`).
			WithArgs("abc123def456").
			WillReturnRows(rows)

		results, err := repo.GetByCommit(context.Background(), "abc123def456")
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, expected.ID, results[0].ID)
		assert.Equal(t, expected.RepoFullName, results[0].RepoFullName)
		assert.Equal(t, expected.EventType, results[0].EventType)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty", func(t *testing.T) {
		repo, mock, cleanup := newLifecycleMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, deployment_id, release_id, ci_run_id, project_id, service_id`).
			WithArgs("nonexistent-sha").
			WillReturnRows(sqlmock.NewRows(lifecycleColumns))

		results, err := repo.GetByCommit(context.Background(), "nonexistent-sha")
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query error", func(t *testing.T) {
		repo, mock, cleanup := newLifecycleMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, deployment_id, release_id, ci_run_id, project_id, service_id`).
			WithArgs("error-sha").
			WillReturnError(fmt.Errorf("db unavailable"))

		results, err := repo.GetByCommit(context.Background(), "error-sha")
		assert.Nil(t, results)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetLatestByRepo ---

func TestLifecycleEventRepository_GetLatestByRepo(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newLifecycleMockDB(t)
		defer cleanup()

		expected := sampleLifecycleEvent()
		rows := sqlmock.NewRows(lifecycleColumns)
		addLifecycleRow(rows, expected)

		mock.ExpectQuery(`SELECT id, deployment_id, release_id, ci_run_id, project_id, service_id`).
			WithArgs("madfam-org/enclii").
			WillReturnRows(rows)

		result, err := repo.GetLatestByRepo(context.Background(), "madfam-org/enclii")
		assert.NoError(t, err)
		assert.Equal(t, expected.ID, result.ID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found returns ErrNoRows", func(t *testing.T) {
		repo, mock, cleanup := newLifecycleMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, deployment_id, release_id, ci_run_id, project_id, service_id`).
			WithArgs("madfam-org/nonexistent").
			WillReturnRows(sqlmock.NewRows(lifecycleColumns))

		result, err := repo.GetLatestByRepo(context.Background(), "madfam-org/nonexistent")
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetLatestByBranch ---

func TestLifecycleEventRepository_GetLatestByBranch(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newLifecycleMockDB(t)
		defer cleanup()

		expected := sampleLifecycleEvent()
		rows := sqlmock.NewRows(lifecycleColumns)
		addLifecycleRow(rows, expected)

		mock.ExpectQuery(`SELECT id, deployment_id, release_id, ci_run_id, project_id, service_id`).
			WithArgs("madfam-org/enclii", "main").
			WillReturnRows(rows)

		result, err := repo.GetLatestByBranch(context.Background(), "madfam-org/enclii", "main")
		assert.NoError(t, err)
		assert.Equal(t, expected.ID, result.ID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found returns ErrNoRows", func(t *testing.T) {
		repo, mock, cleanup := newLifecycleMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, deployment_id, release_id, ci_run_id, project_id, service_id`).
			WithArgs("madfam-org/enclii", "nonexistent-branch").
			WillReturnRows(sqlmock.NewRows(lifecycleColumns))

		result, err := repo.GetLatestByBranch(context.Background(), "madfam-org/enclii", "nonexistent-branch")
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- UpdateMetadata ---

func TestLifecycleEventRepository_UpdateMetadata(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newLifecycleMockDB(t)
		defer cleanup()

		id := uuid.New()
		metadata := map[string]interface{}{"image": "ghcr.io/test:v2", "digest": "sha256:abc123"}

		mock.ExpectExec(`UPDATE deployment_lifecycle_events SET metadata`).
			WithArgs(sqlmock.AnyArg(), id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateMetadata(context.Background(), id, metadata)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newLifecycleMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE deployment_lifecycle_events SET metadata`).
			WithArgs(sqlmock.AnyArg(), id).
			WillReturnError(fmt.Errorf("connection lost"))

		err := repo.UpdateMetadata(context.Background(), id, map[string]interface{}{})
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByBranch ---

func TestLifecycleEventRepository_GetByBranch(t *testing.T) {
	t.Run("returns events", func(t *testing.T) {
		repo, mock, cleanup := newLifecycleMockDB(t)
		defer cleanup()

		expected := sampleLifecycleEvent()
		rows := sqlmock.NewRows(lifecycleColumns)
		addLifecycleRow(rows, expected)

		mock.ExpectQuery(`SELECT id, deployment_id, release_id, ci_run_id, project_id, service_id`).
			WithArgs("madfam-org/enclii", "main", 50).
			WillReturnRows(rows)

		results, err := repo.GetByBranch(context.Background(), "madfam-org/enclii", "main", 50)
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("clamps invalid limit", func(t *testing.T) {
		repo, mock, cleanup := newLifecycleMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, deployment_id, release_id, ci_run_id, project_id, service_id`).
			WithArgs("madfam-org/enclii", "main", 50).
			WillReturnRows(sqlmock.NewRows(lifecycleColumns))

		results, err := repo.GetByBranch(context.Background(), "madfam-org/enclii", "main", -1)
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
