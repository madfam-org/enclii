package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
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

func newWebhookMockDB(t *testing.T) (*WebhookRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewWebhookRepository(db)
	return repo, mock, func() { db.Close() }
}

var webhookColumns = []string{
	"id", "project_id", "name", "type", "webhook_url",
	"telegram_bot_token", "telegram_chat_id", "custom_headers", "signing_secret",
	"events", "enabled", "last_delivery_at", "last_delivery_status", "last_delivery_error",
	"consecutive_failures", "auto_disabled_at",
	"created_by", "created_by_email", "created_at", "updated_at",
}

func sampleWebhookRow(id, projectID uuid.UUID, name string) []driver.Value {
	now := time.Now().Truncate(time.Microsecond)
	eventsJSON, _ := json.Marshal([]types.WebhookEventType{types.WebhookEventDeploymentStarted})
	headersJSON, _ := json.Marshal(map[string]string{})
	return []driver.Value{
		id, projectID, name, types.WebhookTypeSlack, "https://hooks.slack.com/test",
		sql.NullString{}, sql.NullString{}, headersJSON, sql.NullString{},
		eventsJSON, true, sql.NullTime{}, sql.NullString{}, sql.NullString{},
		0, sql.NullTime{},
		sql.NullString{}, sql.NullString{}, now, now,
	}
}

// --- Create ---

func TestWebhookRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newWebhookMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		webhook := &types.WebhookDestination{
			ProjectID:  projectID,
			Name:       "deploy-notifier",
			Type:       types.WebhookTypeSlack,
			WebhookURL: "https://hooks.slack.com/services/test",
			Events:     []types.WebhookEventType{types.WebhookEventDeploymentStarted},
			Enabled:    true,
		}

		mock.ExpectExec(`INSERT INTO webhook_destinations`).
			WithArgs(
				sqlmock.AnyArg(), projectID, "deploy-notifier", types.WebhookTypeSlack, "https://hooks.slack.com/services/test",
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), true, 0,
				(*uuid.UUID)(nil), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(context.Background(), webhook)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, webhook.ID, "ID should be assigned")
		assert.False(t, webhook.CreatedAt.IsZero(), "CreatedAt should be set")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newWebhookMockDB(t)
		defer cleanup()

		webhook := &types.WebhookDestination{
			ProjectID:  uuid.New(),
			Name:       "fail-hook",
			Type:       types.WebhookTypeSlack,
			WebhookURL: "https://hooks.slack.com/fail",
			Events:     []types.WebhookEventType{types.WebhookEventBuildFailed},
			Enabled:    true,
		}

		mock.ExpectExec(`INSERT INTO webhook_destinations`).
			WithArgs(
				sqlmock.AnyArg(), webhook.ProjectID, "fail-hook", types.WebhookTypeSlack, "https://hooks.slack.com/fail",
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), true, 0,
				(*uuid.UUID)(nil), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnError(fmt.Errorf("connection refused"))

		err := repo.Create(context.Background(), webhook)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByID ---

func TestWebhookRepository_GetByID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newWebhookMockDB(t)
		defer cleanup()

		id := uuid.New()
		projectID := uuid.New()
		row := sampleWebhookRow(id, projectID, "my-hook")

		rows := sqlmock.NewRows(webhookColumns).AddRow(row...)

		mock.ExpectQuery(`SELECT id, project_id, name, type, webhook_url`).
			WithArgs(id).
			WillReturnRows(rows)

		result, err := repo.GetByID(context.Background(), id)
		assert.NoError(t, err)
		assert.Equal(t, id, result.ID)
		assert.Equal(t, "my-hook", result.Name)
		assert.Equal(t, types.WebhookTypeSlack, result.Type)
		assert.True(t, result.Enabled)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newWebhookMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, name, type, webhook_url`).
			WithArgs(id).
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newWebhookMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, name, type, webhook_url`).
			WithArgs(id).
			WillReturnError(fmt.Errorf("connection reset"))

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- ListByProject ---

func TestWebhookRepository_ListByProject(t *testing.T) {
	t.Run("multiple results", func(t *testing.T) {
		repo, mock, cleanup := newWebhookMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		row1 := sampleWebhookRow(uuid.New(), projectID, "hook-alpha")
		row2 := sampleWebhookRow(uuid.New(), projectID, "hook-beta")

		rows := sqlmock.NewRows(webhookColumns).AddRow(row1...).AddRow(row2...)

		mock.ExpectQuery(`SELECT id, project_id, name, type, webhook_url`).
			WithArgs(projectID).
			WillReturnRows(rows)

		results, err := repo.ListByProject(context.Background(), projectID)
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.Equal(t, "hook-alpha", results[0].Name)
		assert.Equal(t, "hook-beta", results[1].Name)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty", func(t *testing.T) {
		repo, mock, cleanup := newWebhookMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, name, type, webhook_url`).
			WithArgs(projectID).
			WillReturnRows(sqlmock.NewRows(webhookColumns))

		results, err := repo.ListByProject(context.Background(), projectID)
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query error", func(t *testing.T) {
		repo, mock, cleanup := newWebhookMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, name, type, webhook_url`).
			WithArgs(projectID).
			WillReturnError(fmt.Errorf("db unavailable"))

		results, err := repo.ListByProject(context.Background(), projectID)
		assert.Nil(t, results)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- Update ---

func TestWebhookRepository_Update(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newWebhookMockDB(t)
		defer cleanup()

		id := uuid.New()
		webhook := &types.WebhookDestination{
			ID:         id,
			Name:       "updated-hook",
			WebhookURL: "https://hooks.slack.com/updated",
			Events:     []types.WebhookEventType{types.WebhookEventDeploymentSucceeded},
			Enabled:    true,
		}

		mock.ExpectExec(`UPDATE webhook_destinations`).
			WithArgs(
				id, "updated-hook", "https://hooks.slack.com/updated",
				sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), true,
				sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Update(context.Background(), webhook)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newWebhookMockDB(t)
		defer cleanup()

		webhook := &types.WebhookDestination{
			ID:         uuid.New(),
			Name:       "fail-hook",
			WebhookURL: "https://hooks.slack.com/fail",
			Events:     []types.WebhookEventType{types.WebhookEventBuildFailed},
			Enabled:    true,
		}

		mock.ExpectExec(`UPDATE webhook_destinations`).
			WithArgs(
				webhook.ID, "fail-hook", "https://hooks.slack.com/fail",
				sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), true,
				sqlmock.AnyArg(),
			).
			WillReturnError(fmt.Errorf("connection reset"))

		err := repo.Update(context.Background(), webhook)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- Delete ---

func TestWebhookRepository_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newWebhookMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM webhook_destinations WHERE id = \$1`).
			WithArgs(id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Delete(context.Background(), id)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newWebhookMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM webhook_destinations WHERE id = \$1`).
			WithArgs(id).
			WillReturnError(fmt.Errorf("foreign key violation"))

		err := repo.Delete(context.Background(), id)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "foreign key")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- UpdateDeliveryStatus ---

func TestWebhookRepository_UpdateDeliveryStatus(t *testing.T) {
	t.Run("success with failure increment", func(t *testing.T) {
		repo, mock, cleanup := newWebhookMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE webhook_destinations`).
			WithArgs(id, sqlmock.AnyArg(), "failed", sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateDeliveryStatus(context.Background(), id, "failed", "timeout", true)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("success without failure increment", func(t *testing.T) {
		repo, mock, cleanup := newWebhookMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE webhook_destinations`).
			WithArgs(id, sqlmock.AnyArg(), "success", sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateDeliveryStatus(context.Background(), id, "success", "", false)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- ResetFailures ---

func TestWebhookRepository_ResetFailures(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newWebhookMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE webhook_destinations`).
			WithArgs(id, sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.ResetFailures(context.Background(), id)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newWebhookMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE webhook_destinations`).
			WithArgs(id, sqlmock.AnyArg()).
			WillReturnError(fmt.Errorf("connection lost"))

		err := repo.ResetFailures(context.Background(), id)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- CreateDelivery ---

func TestWebhookRepository_CreateDelivery(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newWebhookMockDB(t)
		defer cleanup()

		webhookID := uuid.New()
		delivery := &types.WebhookDelivery{
			WebhookID:     webhookID,
			EventType:     types.WebhookEventDeploymentStarted,
			Payload:       map[string]any{"service": "api"},
			Status:        types.WebhookDeliveryStatusPending,
			AttemptNumber: 1,
		}

		mock.ExpectExec(`INSERT INTO webhook_deliveries`).
			WithArgs(
				sqlmock.AnyArg(), webhookID, types.WebhookEventDeploymentStarted,
				(*uuid.UUID)(nil), sqlmock.AnyArg(),
				types.WebhookDeliveryStatusPending, (*int)(nil), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), (*time.Time)(nil), (*int)(nil), 1,
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.CreateDelivery(context.Background(), delivery)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, delivery.ID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetDelivery ---

func TestWebhookRepository_GetDelivery(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newWebhookMockDB(t)
		defer cleanup()

		id := uuid.New()
		webhookID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		payloadJSON, _ := json.Marshal(map[string]any{"key": "value"})

		deliveryColumns := []string{
			"id", "webhook_id", "event_type", "event_id", "payload",
			"status", "status_code", "response_body", "error_message",
			"attempted_at", "completed_at", "duration_ms", "attempt_number",
		}

		rows := sqlmock.NewRows(deliveryColumns).
			AddRow(id, webhookID, types.WebhookEventDeploymentStarted, nil, payloadJSON,
				types.WebhookDeliveryStatusSuccess, int64(200), "OK", "",
				now, now, int64(150), 1)

		mock.ExpectQuery(`SELECT id, webhook_id, event_type, event_id, payload, status, status_code`).
			WithArgs(id).
			WillReturnRows(rows)

		result, err := repo.GetDelivery(context.Background(), id)
		assert.NoError(t, err)
		assert.Equal(t, id, result.ID)
		assert.Equal(t, webhookID, result.WebhookID)
		assert.Equal(t, types.WebhookDeliveryStatusSuccess, result.Status)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newWebhookMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, webhook_id, event_type, event_id, payload, status, status_code`).
			WithArgs(id).
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetDelivery(context.Background(), id)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
