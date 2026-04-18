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
// P2.6 added service_id + version_number between error_message and the
// timestamps. Tests that stub SELECT queries must return rows in this order.
var deploymentColumns = []string{
	"id", "release_id", "environment_id", "replicas", "status", "health",
	"error_message", "service_id", "version_number", "created_at", "updated_at",
}

func deploymentRow(d *types.Deployment) *sqlmock.Rows {
	return sqlmock.NewRows(deploymentColumns).
		AddRow(d.ID, d.ReleaseID, d.EnvironmentID, d.Replicas, d.Status, d.Health,
			d.ErrorMessage, d.ServiceID, d.VersionNumber, d.CreatedAt, d.UpdatedAt)
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

		mock.ExpectQuery(`SELECT id, release_id, environment_id, replicas, status, health, error_message,\s+service_id, version_number, created_at, updated_at\s+FROM deployments WHERE id = \$1`).
			WithArgs(id.String()).
			WillReturnRows(sqlmock.NewRows(deploymentColumns).
				AddRow(id, relID, envID, 3, types.DeploymentStatusRunning, types.HealthStatusHealthy,
					nil, nil, nil, now, now))

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
		mock.ExpectQuery(`SELECT id, release_id, environment_id, replicas, status, health, error_message,\s+service_id, version_number, created_at, updated_at\s+FROM deployments WHERE id = \$1`).
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
				types.HealthStatusHealthy, nil, nil, nil, now, now).
			AddRow(uuid.New(), uuid.MustParse(releaseID), uuid.New(), 1, types.DeploymentStatusFailed,
				types.HealthStatusUnhealthy, ptrStr("timeout"), nil, nil, now.Add(-time.Hour), now.Add(-time.Hour))

		mock.ExpectQuery(`SELECT id, release_id, environment_id, replicas, status, health, error_message,\s+service_id, version_number, created_at, updated_at\s+FROM deployments WHERE release_id = \$1 ORDER BY created_at DESC`).
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
		mock.ExpectQuery(`SELECT id, release_id, environment_id, replicas, status, health, error_message,\s+service_id, version_number, created_at, updated_at\s+FROM deployments WHERE release_id = \$1`).
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

		mock.ExpectQuery(`SELECT d\.id, d\.release_id, d\.environment_id, d\.replicas, d\.status, d\.health, d\.error_message,\s+d\.service_id, d\.version_number, d\.created_at, d\.updated_at`).
			WithArgs(svcID).
			WillReturnRows(sqlmock.NewRows(deploymentColumns).
				AddRow(depID, uuid.New(), uuid.New(), 2, types.DeploymentStatusRunning,
					types.HealthStatusHealthy, nil, nil, nil, now, now))

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
				types.HealthStatusUnknown, nil, nil, nil, now, now)

		mock.ExpectQuery(`SELECT id, release_id, environment_id, replicas, status, health, error_message,\s+service_id, version_number, created_at, updated_at\s+FROM deployments WHERE status = \$1 ORDER BY created_at ASC`).
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

		mock.ExpectQuery(`SELECT id, release_id, environment_id, replicas, status, health, error_message,\s+service_id, version_number, created_at, updated_at\s+FROM deployments WHERE status = \$1`).
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
				types.HealthStatusHealthy, nil, nil, nil, now, now)

		mock.ExpectQuery(`SELECT d\.id, d\.release_id, d\.environment_id, d\.replicas, d\.status, d\.health, d\.error_message,\s+d\.service_id, d\.version_number, d\.created_at, d\.updated_at`).
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
				types.HealthStatusHealthy, nil, nil, nil, now, now)

		mock.ExpectQuery(`SELECT id, release_id, environment_id, replicas, status, health, error_message,\s+service_id, version_number, created_at, updated_at\s+FROM deployments ORDER BY created_at DESC LIMIT \$1`).
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
				types.HealthStatusHealthy, nil, nil, nil, now, now)

		mock.ExpectQuery(`SELECT id, release_id, environment_id, replicas, status, health, error_message,\s+service_id, version_number, created_at, updated_at\s+FROM deployments WHERE created_at >= \$1 ORDER BY created_at DESC LIMIT \$2`).
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

		mock.ExpectQuery(`SELECT id, release_id, environment_id, replicas, status, health, error_message,\s+service_id, version_number, created_at, updated_at\s+FROM deployments ORDER BY created_at DESC LIMIT \$1`).
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

		mock.ExpectQuery(`SELECT id, release_id, environment_id, replicas, status, health, error_message,\s+service_id, version_number, created_at, updated_at\s+FROM deployments ORDER BY created_at DESC LIMIT \$1`).
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
					types.HealthStatusUnknown, nil, nil, nil, now, now))

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
					types.HealthStatusUnknown, nil, nil, nil, now, now))

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

// --- P2.6: Heroku-style semantic version numbers ---

// TestDeploymentRepository_CreateWithVersion covers the P2.6 allocation path:
// Create uses the INSERT ... SELECT MAX()+1 pattern when ServiceID is set
// and populates VersionNumber on the returned deployment.
func TestDeploymentRepository_CreateWithVersion(t *testing.T) {
	t.Run("first deployment gets v1", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		d := newTestDeployment()
		d.ServiceID = &svcID

		// The allocate-and-insert returns version_number via RETURNING.
		mock.ExpectQuery(`INSERT INTO deployments.*RETURNING version_number`).
			WithArgs(
				sqlmock.AnyArg(), d.ReleaseID, d.EnvironmentID, svcID,
				2, types.DeploymentStatusPending, types.HealthStatusUnknown,
				sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnRows(sqlmock.NewRows([]string{"version_number"}).AddRow(1))

		err := repo.Create(d)
		assert.NoError(t, err)
		require.NotNil(t, d.VersionNumber)
		assert.Equal(t, 1, *d.VersionNumber)
		assert.Equal(t, "v1", d.VersionLabel())
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("second deployment gets v2", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		d := newTestDeployment()
		d.ServiceID = &svcID

		mock.ExpectQuery(`INSERT INTO deployments.*RETURNING version_number`).
			WithArgs(
				sqlmock.AnyArg(), d.ReleaseID, d.EnvironmentID, svcID,
				2, types.DeploymentStatusPending, types.HealthStatusUnknown,
				sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnRows(sqlmock.NewRows([]string{"version_number"}).AddRow(2))

		err := repo.Create(d)
		assert.NoError(t, err)
		require.NotNil(t, d.VersionNumber)
		assert.Equal(t, 2, *d.VersionNumber)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("unique-violation surfaces as ErrVersionAllocationConflict", func(t *testing.T) {
		// Covers the concurrent-allocation race: two deploys read the same
		// MAX(version_number), both try to INSERT with the same v-number,
		// and the UNIQUE index fires on the second one. Per the P2.6
		// contract we bubble up a typed error so the caller can retry the
		// enclosing transaction — we do NOT silently renumber.
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		d := newTestDeployment()
		d.ServiceID = &svcID

		mock.ExpectQuery(`INSERT INTO deployments.*RETURNING version_number`).
			WithArgs(
				sqlmock.AnyArg(), d.ReleaseID, d.EnvironmentID, svcID,
				2, types.DeploymentStatusPending, types.HealthStatusUnknown,
				sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnError(fmt.Errorf(`pq: duplicate key value violates unique constraint "idx_deployments_service_version"`))

		err := repo.Create(d)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrVersionAllocationConflict)
		assert.Nil(t, d.VersionNumber, "version should not be populated on conflict")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("non-allocation error is not wrapped as conflict", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		d := newTestDeployment()
		d.ServiceID = &svcID

		mock.ExpectQuery(`INSERT INTO deployments.*RETURNING version_number`).
			WithArgs(
				sqlmock.AnyArg(), d.ReleaseID, d.EnvironmentID, svcID,
				2, types.DeploymentStatusPending, types.HealthStatusUnknown,
				sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnError(fmt.Errorf("connection refused"))

		err := repo.Create(d)
		assert.Error(t, err)
		assert.NotErrorIs(t, err, ErrVersionAllocationConflict)
		assert.Contains(t, err.Error(), "connection refused")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("legacy call without ServiceID uses old INSERT path", func(t *testing.T) {
		// Pre-P2.6 call sites may not set ServiceID. The storage layer
		// gracefully falls through to the original column set so we don't
		// break rolling deploys during the migration window.
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		d := newTestDeployment()
		// d.ServiceID is nil.

		mock.ExpectExec(`INSERT INTO deployments \(id, release_id, environment_id, replicas, status, health, created_at, updated_at\)`).
			WithArgs(
				sqlmock.AnyArg(), d.ReleaseID, d.EnvironmentID, 2,
				types.DeploymentStatusPending, types.HealthStatusUnknown,
				sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(d)
		assert.NoError(t, err)
		assert.Nil(t, d.VersionNumber, "legacy path should not allocate a version")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// TestDeploymentRepository_CreateWithVersion_RaceRetry documents the
// allocation-race contract from the P2.6 spec:
//
//  1. Two deploys to the same service run concurrently.
//  2. Both compute MAX(version_number)+1 and try to INSERT the same value.
//  3. The UNIQUE (service_id, version_number) index fires on the second.
//  4. The repo returns ErrVersionAllocationConflict — the caller retries,
//     MAX() now reflects the winning row, and the second deploy gets the
//     NEXT v-number (winner=v43, loser retries and becomes v44).
//
// The retry is coordinated at the app layer (handler loop or caller's
// WithTransaction), never inside Create itself. This test simulates that
// handshake with sqlmock: first call conflicts, second call succeeds with
// an incremented version.
func TestDeploymentRepository_CreateWithVersion_RaceRetry(t *testing.T) {
	repo, mock, cleanup := newDeploymentMockDB(t)
	defer cleanup()

	svcID := uuid.New()
	d := newTestDeployment()
	d.ServiceID = &svcID

	// Attempt 1: unique violation (the other deploy got v43 first).
	mock.ExpectQuery(`INSERT INTO deployments.*RETURNING version_number`).
		WillReturnError(fmt.Errorf(`pq: duplicate key value violates unique constraint "idx_deployments_service_version"`))

	// Attempt 2 (after caller retries): MAX() now sees the winner's row,
	// so we allocate v44.
	mock.ExpectQuery(`INSERT INTO deployments.*RETURNING version_number`).
		WillReturnRows(sqlmock.NewRows([]string{"version_number"}).AddRow(44))

	// First call: conflict surfaces cleanly.
	err1 := repo.Create(d)
	assert.ErrorIs(t, err1, ErrVersionAllocationConflict)
	assert.Nil(t, d.VersionNumber, "no version assigned on conflict")

	// Caller retries (resets ID/timestamps, keeps ServiceID). This is the
	// exact handshake the handler implements.
	d.ID = uuid.Nil
	err2 := repo.Create(d)
	require.NoError(t, err2)
	require.NotNil(t, d.VersionNumber)
	assert.Equal(t, 44, *d.VersionNumber, "retry gets the next v-number, not a reused one")

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestDeploymentRepository_GetByServiceAndVersion covers the lookup endpoint
// used by `enclii rollback v42` and `GET /v1/services/:id/versions/:v`.
func TestDeploymentRepository_GetByServiceAndVersion(t *testing.T) {
	t.Run("found returns deployment with version populated", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		depID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		version := 42

		mock.ExpectQuery(`SELECT id, release_id, environment_id, replicas, status, health, error_message,\s+service_id, version_number, created_at, updated_at\s+FROM deployments\s+WHERE service_id = \$1 AND version_number = \$2`).
			WithArgs(svcID, 42).
			WillReturnRows(sqlmock.NewRows(deploymentColumns).
				AddRow(depID, uuid.New(), uuid.New(), 2, types.DeploymentStatusRunning,
					types.HealthStatusHealthy, nil, svcID, version, now, now))

		result, err := repo.GetByServiceAndVersion(context.Background(), svcID, 42)
		assert.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, depID, result.ID)
		require.NotNil(t, result.VersionNumber)
		assert.Equal(t, 42, *result.VersionNumber)
	})

	t.Run("not found returns sql.ErrNoRows", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		mock.ExpectQuery(`SELECT id, release_id, environment_id, replicas, status, health, error_message,\s+service_id, version_number, created_at, updated_at\s+FROM deployments\s+WHERE service_id = \$1 AND version_number = \$2`).
			WithArgs(svcID, 999).
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByServiceAndVersion(context.Background(), svcID, 999)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
	})

	t.Run("resolves historical version (not most recent)", func(t *testing.T) {
		// Confirms the lookup works for any v-number in the history, not
		// just the latest — matches the spec's requirement that
		// `enclii rollback v42` works when v42 is not the current deploy.
		repo, mock, cleanup := newDeploymentMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		oldDepID := uuid.New()
		oldTime := time.Now().Add(-30 * 24 * time.Hour).Truncate(time.Microsecond)
		version := 7

		mock.ExpectQuery(`SELECT id, release_id, environment_id, replicas, status, health, error_message,\s+service_id, version_number, created_at, updated_at\s+FROM deployments\s+WHERE service_id = \$1 AND version_number = \$2`).
			WithArgs(svcID, 7).
			WillReturnRows(sqlmock.NewRows(deploymentColumns).
				AddRow(oldDepID, uuid.New(), uuid.New(), 1, types.DeploymentStatusCancelled,
					types.HealthStatusUnknown, nil, svcID, version, oldTime, oldTime))

		result, err := repo.GetByServiceAndVersion(context.Background(), svcID, 7)
		assert.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, oldDepID, result.ID)
		assert.Equal(t, 7, *result.VersionNumber)
	})
}

// --- helper ---

func ptrStr(s string) *string {
	return &s
}
