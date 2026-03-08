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

func newOnboardingMockDB(t *testing.T) (*OnboardingRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewOnboardingRepository(db)
	return repo, mock, func() { db.Close() }
}

var onboardingColumns = []string{
	"id", "project_id", "repo_full_name", "webhook_id", "webhook_secret",
	"argocd_app_name", "onboard_status", "config_snapshot", "error_message",
	"created_at", "updated_at",
}

// --- Create ---

func TestOnboardingRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newOnboardingMockDB(t)
		defer cleanup()

		reg := &types.OnboardingRegistration{
			ProjectID:      uuid.New(),
			RepoFullName:   "madfam-org/my-service",
			OnboardStatus:  "pending",
			ConfigSnapshot: map[string]interface{}{"key": "value"},
		}

		mock.ExpectExec(`INSERT INTO onboarding_registrations`).
			WithArgs(
				sqlmock.AnyArg(), reg.ProjectID, "madfam-org/my-service",
				nil, nil, nil, "pending", sqlmock.AnyArg(), nil,
				sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(context.Background(), reg)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, reg.ID)
		assert.False(t, reg.CreatedAt.IsZero())
		assert.False(t, reg.UpdatedAt.IsZero())
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("with webhook fields", func(t *testing.T) {
		repo, mock, cleanup := newOnboardingMockDB(t)
		defer cleanup()

		webhookID := int64(12345)
		webhookSecret := "secret-value"
		argoApp := "my-argo-app"
		reg := &types.OnboardingRegistration{
			ProjectID:     uuid.New(),
			RepoFullName:  "madfam-org/svc",
			WebhookID:     &webhookID,
			WebhookSecret: &webhookSecret,
			ArgocdAppName: &argoApp,
			OnboardStatus: "complete",
		}

		mock.ExpectExec(`INSERT INTO onboarding_registrations`).
			WithArgs(
				sqlmock.AnyArg(), reg.ProjectID, "madfam-org/svc",
				&webhookID, &webhookSecret, &argoApp, "complete", sqlmock.AnyArg(), nil,
				sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(context.Background(), reg)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newOnboardingMockDB(t)
		defer cleanup()

		reg := &types.OnboardingRegistration{
			ProjectID:     uuid.New(),
			RepoFullName:  "madfam-org/fail",
			OnboardStatus: "pending",
		}

		mock.ExpectExec(`INSERT INTO onboarding_registrations`).
			WithArgs(
				sqlmock.AnyArg(), reg.ProjectID, "madfam-org/fail",
				nil, nil, nil, "pending", sqlmock.AnyArg(), nil,
				sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnError(fmt.Errorf("connection refused"))

		err := repo.Create(context.Background(), reg)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByRepo ---

func TestOnboardingRepository_GetByRepo(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newOnboardingMockDB(t)
		defer cleanup()

		id := uuid.New()
		projID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		webhookID := int64(42)
		webhookSecret := "wh-secret"
		argoApp := "argo-app"

		mock.ExpectQuery(`SELECT id, project_id, repo_full_name, webhook_id, webhook_secret`).
			WithArgs("madfam-org/my-service").
			WillReturnRows(sqlmock.NewRows(onboardingColumns).
				AddRow(id, projID, "madfam-org/my-service", webhookID, webhookSecret,
					argoApp, "complete", []byte(`{"key":"value"}`), nil,
					now, now))

		result, err := repo.GetByRepo(context.Background(), "madfam-org/my-service")
		assert.NoError(t, err)
		assert.Equal(t, id, result.ID)
		assert.Equal(t, projID, result.ProjectID)
		assert.Equal(t, "madfam-org/my-service", result.RepoFullName)
		assert.Equal(t, "complete", result.OnboardStatus)
		assert.NotNil(t, result.WebhookID)
		assert.Equal(t, int64(42), *result.WebhookID)
		assert.NotNil(t, result.WebhookSecret)
		assert.Equal(t, "wh-secret", *result.WebhookSecret)
		assert.NotNil(t, result.ArgocdAppName)
		assert.Equal(t, "argo-app", *result.ArgocdAppName)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newOnboardingMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, project_id, repo_full_name, webhook_id, webhook_secret`).
			WithArgs("madfam-org/nonexistent").
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByRepo(context.Background(), "madfam-org/nonexistent")
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newOnboardingMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, project_id, repo_full_name, webhook_id, webhook_secret`).
			WithArgs("madfam-org/fail").
			WillReturnError(fmt.Errorf("connection reset"))

		result, err := repo.GetByRepo(context.Background(), "madfam-org/fail")
		assert.Nil(t, result)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- UpdateStatus ---

func TestOnboardingRepository_UpdateStatus(t *testing.T) {
	t.Run("success without error message", func(t *testing.T) {
		repo, mock, cleanup := newOnboardingMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE onboarding_registrations`).
			WithArgs("complete", nil, id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateStatus(context.Background(), id, "complete", nil)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("success with error message", func(t *testing.T) {
		repo, mock, cleanup := newOnboardingMockDB(t)
		defer cleanup()

		id := uuid.New()
		errMsg := "webhook creation failed"
		mock.ExpectExec(`UPDATE onboarding_registrations`).
			WithArgs("failed", &errMsg, id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateStatus(context.Background(), id, "failed", &errMsg)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newOnboardingMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE onboarding_registrations`).
			WithArgs("complete", nil, id).
			WillReturnError(fmt.Errorf("db unavailable"))

		err := repo.UpdateStatus(context.Background(), id, "complete", nil)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- List ---

func TestOnboardingRepository_List(t *testing.T) {
	t.Run("multiple results", func(t *testing.T) {
		repo, mock, cleanup := newOnboardingMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)

		rows := sqlmock.NewRows(onboardingColumns).
			AddRow(uuid.New(), uuid.New(), "madfam-org/svc-a", nil, nil,
				nil, "complete", []byte("{}"), nil, now, now).
			AddRow(uuid.New(), uuid.New(), "madfam-org/svc-b", int64(99), "secret",
				"app-b", "pending", []byte("{}"), "some error", now, now)

		mock.ExpectQuery(`SELECT id, project_id, repo_full_name, webhook_id, webhook_secret`).
			WillReturnRows(rows)

		results, err := repo.List(context.Background())
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.Equal(t, "madfam-org/svc-a", results[0].RepoFullName)
		assert.Nil(t, results[0].WebhookID)
		assert.Equal(t, "madfam-org/svc-b", results[1].RepoFullName)
		assert.NotNil(t, results[1].WebhookID)
		assert.Equal(t, int64(99), *results[1].WebhookID)
		assert.NotNil(t, results[1].ErrorMessage)
		assert.Equal(t, "some error", *results[1].ErrorMessage)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty results", func(t *testing.T) {
		repo, mock, cleanup := newOnboardingMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, project_id, repo_full_name, webhook_id, webhook_secret`).
			WillReturnRows(sqlmock.NewRows(onboardingColumns))

		results, err := repo.List(context.Background())
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query error", func(t *testing.T) {
		repo, mock, cleanup := newOnboardingMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, project_id, repo_full_name, webhook_id, webhook_secret`).
			WillReturnError(fmt.Errorf("db unavailable"))

		results, err := repo.List(context.Background())
		assert.Nil(t, results)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
