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

func newDeploymentMockDB(t *testing.T) (*DeploymentRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewDeploymentRepository(db)
	return repo, mock, func() { db.Close() }
}

// deploymentColumns matches the standard column set for deployment queries.
var deploymentColumns = []string{
	"id", "release_id", "environment_id", "replicas", "status", "health",
	"error_message", "created_at", "updated_at",
}

func deploymentRow(d *types.Deployment) *sqlmock.Rows {
	return sqlmock.NewRows(deploymentColumns).
		AddRow(d.ID, d.ReleaseID, d.EnvironmentID, d.Replicas, d.Status, d.Health,
			d.ErrorMessage, d.CreatedAt, d.UpdatedAt)
}

func newTestDeployment() *types.Deployment {
	return &types.Deployment{
		ReleaseID:     uuid.New(),
		EnvironmentID: uuid.New(),
		Replicas:      2,
		Status:        types.DeploymentStatusPending,
		Health:        types.HealthStatusUnknown,
	}
}

// --- Create ---

func TestDeploymentRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		d := newTestDeployment()

		mock.ExpectExec(`INSERT INTO deployments`).
			WithArgs(
				sqlmock.AnyArg(), d.ReleaseID, d.EnvironmentID, 2,
				types.DeploymentStatusPending, types.HealthStatusUnknown,
				sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(d)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, d.ID)
		assert.False(t, d.CreatedAt.IsZero())
		assert.False(t, d.UpdatedAt.IsZero())
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		d := newTestDeployment()
		mock.ExpectExec(`INSERT INTO deployments`).
			WithArgs(
				sqlmock.AnyArg(), d.ReleaseID, d.EnvironmentID, 2,
				types.DeploymentStatusPending, types.HealthStatusUnknown,
				sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnError(fmt.Errorf("foreign key constraint"))

		err := repo.Create(d)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- UpdateStatus ---

func TestDeploymentRepository_UpdateStatus(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE deployments SET status = \$1, health = \$2, updated_at = NOW\(\) WHERE id = \$3`).
			WithArgs(types.DeploymentStatusRunning, types.HealthStatusHealthy, id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateStatus(id, types.DeploymentStatusRunning, types.HealthStatusHealthy)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("transition pending to deploying", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE deployments SET status = \$1, health = \$2, updated_at = NOW\(\) WHERE id = \$3`).
			WithArgs(types.DeploymentStatusDeploying, types.HealthStatusUnknown, id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateStatus(id, types.DeploymentStatusDeploying, types.HealthStatusUnknown)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("transition to failed", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE deployments SET status = \$1, health = \$2, updated_at = NOW\(\) WHERE id = \$3`).
			WithArgs(types.DeploymentStatusFailed, types.HealthStatusUnhealthy, id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateStatus(id, types.DeploymentStatusFailed, types.HealthStatusUnhealthy)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- UpdateStatusWithError ---

func TestDeploymentRepository_UpdateStatusWithError(t *testing.T) {
	t.Run("with error message", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		id := uuid.New()
		errMsg := "pod CrashLoopBackOff"
		mock.ExpectExec(`UPDATE deployments SET status = \$1, health = \$2, error_message = \$3, updated_at = NOW\(\) WHERE id = \$4`).
			WithArgs(types.DeploymentStatusFailed, types.HealthStatusUnhealthy, &errMsg, id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateStatusWithError(id, types.DeploymentStatusFailed, types.HealthStatusUnhealthy, &errMsg)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("with nil error message", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE deployments SET status = \$1, health = \$2, error_message = \$3, updated_at = NOW\(\) WHERE id = \$4`).
			WithArgs(types.DeploymentStatusRunning, types.HealthStatusHealthy, (*string)(nil), id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateStatusWithError(id, types.DeploymentStatusRunning, types.HealthStatusHealthy, nil)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByID ---

func TestDeploymentRepository_GetByID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		id := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		relID := uuid.New()
		envID := uuid.New()

		mock.ExpectQuery(`SELECT id, release_id, environment_id, replicas, status, health, error_message, created_at, updated_at\s+FROM deployments WHERE id = \$1`).
			WithArgs(id.String()).
			WillReturnRows(sqlmock.NewRows(deploymentColumns).
				AddRow(id, relID, envID, 3, types.DeploymentStatusRunning, types.HealthStatusHealthy,
					nil, now, now))

		result, err := repo.GetByID(context.Background(), id.String())
		assert.NoError(t, err)
		assert.Equal(t, id, result.ID)
		assert.Equal(t, relID, result.ReleaseID)
		assert.Equal(t, envID, result.EnvironmentID)
		assert.Equal(t, 3, result.Replicas)
		assert.Equal(t, types.DeploymentStatusRunning, result.Status)
		assert.Nil(t, result.ErrorMessage)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		id := uuid.New().String()
		mock.ExpectQuery(`SELECT id, release_id, environment_id, replicas, status, health, error_message, created_at, updated_at\s+FROM deployments WHERE id = \$1`).
			WithArgs(id).
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- ListByRelease ---

func TestDeploymentRepository_ListByRelease(t *testing.T) {
	t.Run("with results", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		releaseID := uuid.New().String()
		now := time.Now().Truncate(time.Microsecond)

		rows := sqlmock.NewRows(deploymentColumns).
			AddRow(uuid.New(), uuid.MustParse(releaseID), uuid.New(), 2, types.DeploymentStatusRunning,
				types.HealthStatusHealthy, nil, now, now).
			AddRow(uuid.New(), uuid.MustParse(releaseID), uuid.New(), 1, types.DeploymentStatusFailed,
				types.HealthStatusUnhealthy, ptrStr("timeout"), now.Add(-time.Hour), now.Add(-time.Hour))

		mock.ExpectQuery(`SELECT id, release_id, environment_id, replicas, status, health, error_message, created_at, updated_at\s+FROM deployments WHERE release_id = \$1 ORDER BY created_at DESC`).
			WithArgs(releaseID).
			WillReturnRows(rows)

		results, err := repo.ListByRelease(context.Background(), releaseID)
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.Equal(t, types.DeploymentStatusRunning, results[0].Status)
		assert.Equal(t, types.DeploymentStatusFailed, results[1].Status)
		assert.NotNil(t, results[1].ErrorMessage)
		assert.Equal(t, "timeout", *results[1].ErrorMessage)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		releaseID := uuid.New().String()
		mock.ExpectQuery(`SELECT id, release_id, environment_id, replicas, status, health, error_message, created_at, updated_at\s+FROM deployments WHERE release_id = \$1`).
			WithArgs(releaseID).
			WillReturnRows(sqlmock.NewRows(deploymentColumns))

		results, err := repo.ListByRelease(context.Background(), releaseID)
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetLatestByService ---

func TestDeploymentRepository_GetLatestByService(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		svcID := uuid.New().String()
		now := time.Now().Truncate(time.Microsecond)
		depID := uuid.New()

		mock.ExpectQuery(`SELECT d\.id, d\.release_id, d\.environment_id, d\.replicas, d\.status, d\.health, d\.error_message, d\.created_at, d\.updated_at`).
			WithArgs(svcID).
			WillReturnRows(sqlmock.NewRows(deploymentColumns).
				AddRow(depID, uuid.New(), uuid.New(), 2, types.DeploymentStatusRunning,
					types.HealthStatusHealthy, nil, now, now))

		result, err := repo.GetLatestByService(context.Background(), svcID)
		assert.NoError(t, err)
		assert.Equal(t, depID, result.ID)
		assert.Equal(t, types.DeploymentStatusRunning, result.Status)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		svcID := uuid.New().String()
		mock.ExpectQuery(`SELECT d\.id, d\.release_id, d\.environment_id`).
			WithArgs(svcID).
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetLatestByService(context.Background(), svcID)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByStatus ---

func TestDeploymentRepository_GetByStatus(t *testing.T) {
	t.Run("filters by status", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		rows := sqlmock.NewRows(deploymentColumns).
			AddRow(uuid.New(), uuid.New(), uuid.New(), 1, types.DeploymentStatusDeploying,
				types.HealthStatusUnknown, nil, now, now)

		mock.ExpectQuery(`SELECT id, release_id, environment_id, replicas, status, health, error_message, created_at, updated_at\s+FROM deployments WHERE status = \$1 ORDER BY created_at ASC`).
			WithArgs(types.DeploymentStatusDeploying).
			WillReturnRows(rows)

		results, err := repo.GetByStatus(context.Background(), types.DeploymentStatusDeploying)
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, types.DeploymentStatusDeploying, results[0].Status)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty when no matching status", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, release_id, environment_id, replicas, status, health, error_message, created_at, updated_at\s+FROM deployments WHERE status = \$1`).
			WithArgs(types.DeploymentStatusCancelled).
			WillReturnRows(sqlmock.NewRows(deploymentColumns))

		results, err := repo.GetByStatus(context.Background(), types.DeploymentStatusCancelled)
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByServiceSince ---

func TestDeploymentRepository_GetByServiceSince(t *testing.T) {
	t.Run("returns deployments after cutoff", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		svcID := uuid.New().String()
		since := time.Now().Add(-24 * time.Hour)
		now := time.Now().Truncate(time.Microsecond)

		rows := sqlmock.NewRows(deploymentColumns).
			AddRow(uuid.New(), uuid.New(), uuid.New(), 2, types.DeploymentStatusRunning,
				types.HealthStatusHealthy, nil, now, now)

		mock.ExpectQuery(`SELECT d\.id, d\.release_id, d\.environment_id, d\.replicas, d\.status, d\.health, d\.error_message, d\.created_at, d\.updated_at`).
			WithArgs(svcID, since).
			WillReturnRows(rows)

		results, err := repo.GetByServiceSince(context.Background(), svcID, since)
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- ListAll ---

func TestDeploymentRepository_ListAll(t *testing.T) {
	t.Run("without since filter", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		rows := sqlmock.NewRows(deploymentColumns).
			AddRow(uuid.New(), uuid.New(), uuid.New(), 1, types.DeploymentStatusRunning,
				types.HealthStatusHealthy, nil, now, now)

		mock.ExpectQuery(`SELECT id, release_id, environment_id, replicas, status, health, error_message, created_at, updated_at\s+FROM deployments ORDER BY created_at DESC LIMIT \$1`).
			WithArgs(50).
			WillReturnRows(rows)

		results, err := repo.ListAll(context.Background(), nil, 50)
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("with since filter", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		since := time.Now().Add(-time.Hour)
		now := time.Now().Truncate(time.Microsecond)

		rows := sqlmock.NewRows(deploymentColumns).
			AddRow(uuid.New(), uuid.New(), uuid.New(), 2, types.DeploymentStatusRunning,
				types.HealthStatusHealthy, nil, now, now)

		mock.ExpectQuery(`SELECT id, release_id, environment_id, replicas, status, health, error_message, created_at, updated_at\s+FROM deployments WHERE created_at >= \$1 ORDER BY created_at DESC LIMIT \$2`).
			WithArgs(since, 50).
			WillReturnRows(rows)

		results, err := repo.ListAll(context.Background(), &since, 50)
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("limit clamped to 50 when invalid", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, release_id, environment_id, replicas, status, health, error_message, created_at, updated_at\s+FROM deployments ORDER BY created_at DESC LIMIT \$1`).
			WithArgs(50).
			WillReturnRows(sqlmock.NewRows(deploymentColumns))

		results, err := repo.ListAll(context.Background(), nil, 0)
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("limit clamped to 50 when over 100", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, release_id, environment_id, replicas, status, health, error_message, created_at, updated_at\s+FROM deployments ORDER BY created_at DESC LIMIT \$1`).
			WithArgs(50).
			WillReturnRows(sqlmock.NewRows(deploymentColumns))

		results, err := repo.ListAll(context.Background(), nil, 200)
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- FindDeployingByServiceAndSHA ---

func TestDeploymentRepository_FindDeployingByServiceAndSHA(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		sha := "abc123def456"
		now := time.Now().Truncate(time.Microsecond)
		depID := uuid.New()

		mock.ExpectQuery(`SELECT d\.id, d\.release_id, d\.environment_id, d\.replicas, d\.status, d\.health`).
			WithArgs(svcID, sha).
			WillReturnRows(sqlmock.NewRows(deploymentColumns).
				AddRow(depID, uuid.New(), uuid.New(), 1, types.DeploymentStatusDeploying,
					types.HealthStatusUnknown, nil, now, now))

		result, err := repo.FindDeployingByServiceAndSHA(context.Background(), svcID, sha)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, depID, result.ID)
		assert.Equal(t, types.DeploymentStatusDeploying, result.Status)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found returns nil nil", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		mock.ExpectQuery(`SELECT d\.id, d\.release_id, d\.environment_id`).
			WithArgs(svcID, "nonexistent").
			WillReturnError(sql.ErrNoRows)

		result, err := repo.FindDeployingByServiceAndSHA(context.Background(), svcID, "nonexistent")
		assert.Nil(t, result)
		assert.Nil(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error propagated", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		mock.ExpectQuery(`SELECT d\.id, d\.release_id, d\.environment_id`).
			WithArgs(svcID, "sha").
			WillReturnError(fmt.Errorf("connection timeout"))

		result, err := repo.FindDeployingByServiceAndSHA(context.Background(), svcID, "sha")
		assert.Nil(t, result)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "connection timeout")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- FindRecentDeployingByService ---

func TestDeploymentRepository_FindRecentDeployingByService(t *testing.T) {
	t.Run("found within window", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		depID := uuid.New()

		mock.ExpectQuery(`SELECT d\.id, d\.release_id, d\.environment_id`).
			WithArgs(svcID, sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows(deploymentColumns).
				AddRow(depID, uuid.New(), uuid.New(), 1, types.DeploymentStatusDeploying,
					types.HealthStatusUnknown, nil, now, now))

		result, err := repo.FindRecentDeployingByService(context.Background(), svcID, 30*time.Minute)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, depID, result.ID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found returns nil nil", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		mock.ExpectQuery(`SELECT d\.id, d\.release_id, d\.environment_id`).
			WithArgs(svcID, sqlmock.AnyArg()).
			WillReturnError(sql.ErrNoRows)

		result, err := repo.FindRecentDeployingByService(context.Background(), svcID, 30*time.Minute)
		assert.Nil(t, result)
		assert.Nil(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- CleanupStaleDeploying ---

func TestDeploymentRepository_CleanupStaleDeploying(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		mock.ExpectExec(`UPDATE deployments SET status = 'failed'`).
			WithArgs(svcID, sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(0, 3))

		err := repo.CleanupStaleDeploying(context.Background(), svcID, 30*time.Minute)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- CleanupAllStaleDeploying ---

func TestDeploymentRepository_CleanupAllStaleDeploying(t *testing.T) {
	t.Run("returns affected count", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		mock.ExpectExec(`UPDATE deployments SET status = 'failed'`).
			WithArgs(sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(0, 5))

		count, err := repo.CleanupAllStaleDeploying(context.Background(), 30*time.Minute)
		assert.NoError(t, err)
		assert.Equal(t, int64(5), count)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("zero affected", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		mock.ExpectExec(`UPDATE deployments SET status = 'failed'`).
			WithArgs(sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(0, 0))

		count, err := repo.CleanupAllStaleDeploying(context.Background(), 30*time.Minute)
		assert.NoError(t, err)
		assert.Equal(t, int64(0), count)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		mock.ExpectExec(`UPDATE deployments SET status = 'failed'`).
			WithArgs(sqlmock.AnyArg()).
			WillReturnError(fmt.Errorf("lock timeout"))

		count, err := repo.CleanupAllStaleDeploying(context.Background(), 30*time.Minute)
		assert.Equal(t, int64(0), count)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- ListByGroup ---

func TestDeploymentRepository_ListByGroup(t *testing.T) {
	t.Run("returns empty slice (feature not migrated)", func(t *testing.T) {
		repo, _, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		results, err := repo.ListByGroup(context.Background(), uuid.New())
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NotNil(t, results, "should return non-nil empty slice")
	})
}

// --- helper ---

func ptrStr(s string) *string {
	return &s
}
