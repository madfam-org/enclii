package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newEventRepoMockDB(t *testing.T) (*ManagedDBAddonEventRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	return NewManagedDBAddonEventRepository(db), mock, func() { db.Close() }
}

func TestManagedDBAddonEventRepository_Insert(t *testing.T) {
	t.Run("success with actor", func(t *testing.T) {
		repo, mock, cleanup := newEventRepoMockDB(t)
		defer cleanup()

		addonID := uuid.New()
		projID := uuid.New()

		mock.ExpectExec(`INSERT INTO managed_db_addon_events`).
			WithArgs(
				sqlmock.AnyArg(), addonID, projID,
				EventAddonCreateRequested,
				"auth0|123", "dev@madfam.io",
				sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		id, err := repo.Insert(context.Background(), InsertEventParams{
			AddonID:        addonID,
			ProjectID:      projID,
			EventType:      EventAddonCreateRequested,
			ActorUserSub:   "auth0|123",
			ActorUserEmail: "dev@madfam.io",
			Details:        map[string]interface{}{"plan": "standard-0"},
		})
		require.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, id)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("system actor (empty sub/email)", func(t *testing.T) {
		repo, mock, cleanup := newEventRepoMockDB(t)
		defer cleanup()

		mock.ExpectExec(`INSERT INTO managed_db_addon_events`).
			WithArgs(
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				EventAddonReady,
				"", "",
				sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		_, err := repo.Insert(context.Background(), InsertEventParams{
			AddonID:   uuid.New(),
			ProjectID: uuid.New(),
			EventType: EventAddonReady,
			Details:   nil,
		})
		require.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("missing addon_id rejected", func(t *testing.T) {
		repo, _, cleanup := newEventRepoMockDB(t)
		defer cleanup()

		_, err := repo.Insert(context.Background(), InsertEventParams{
			ProjectID: uuid.New(),
			EventType: EventAddonReady,
		})
		assert.Error(t, err)
	})

	t.Run("missing project_id rejected", func(t *testing.T) {
		repo, _, cleanup := newEventRepoMockDB(t)
		defer cleanup()

		_, err := repo.Insert(context.Background(), InsertEventParams{
			AddonID:   uuid.New(),
			EventType: EventAddonReady,
		})
		assert.Error(t, err)
	})

	t.Run("missing event_type rejected", func(t *testing.T) {
		repo, _, cleanup := newEventRepoMockDB(t)
		defer cleanup()

		_, err := repo.Insert(context.Background(), InsertEventParams{
			AddonID:   uuid.New(),
			ProjectID: uuid.New(),
		})
		assert.Error(t, err)
	})
}

func TestManagedDBAddonEventRepository_ListByAddon(t *testing.T) {
	repo, mock, cleanup := newEventRepoMockDB(t)
	defer cleanup()

	addonID := uuid.New()
	projID := uuid.New()
	now := time.Now().Truncate(time.Microsecond)

	cols := []string{"id", "addon_id", "project_id", "event_type", "actor_user_sub", "actor_user_email", "details", "created_at"}

	mock.ExpectQuery(`FROM managed_db_addon_events\s+WHERE addon_id = \$1`).
		WithArgs(addonID, 100).
		WillReturnRows(sqlmock.NewRows(cols).
			AddRow(uuid.New(), addonID, projID, string(EventAddonReady),
				sql.NullString{}, sql.NullString{}, []byte(`{"host":"10.0.0.5"}`), now).
			AddRow(uuid.New(), addonID, projID, string(EventAddonProvisioningStarted),
				sql.NullString{String: "auth0|abc", Valid: true},
				sql.NullString{String: "op@madfam.io", Valid: true},
				[]byte(`{"plan":"standard-0"}`), now.Add(-time.Minute)))

	events, err := repo.ListByAddon(context.Background(), addonID, 0) // 0 → default 100
	require.NoError(t, err)
	assert.Len(t, events, 2)
	assert.Equal(t, EventAddonReady, events[0].EventType)
	// Actor fields parsed back into struct strings.
	assert.Equal(t, "", events[0].ActorUserSub)
	assert.Equal(t, "auth0|abc", events[1].ActorUserSub)
	// Details round-trip as json.RawMessage.
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(events[0].Details, &payload))
	assert.Equal(t, "10.0.0.5", payload["host"])
}

func TestManagedDBAddonEventRepository_ListByProject(t *testing.T) {
	repo, mock, cleanup := newEventRepoMockDB(t)
	defer cleanup()

	projID := uuid.New()
	now := time.Now()
	cols := []string{"id", "addon_id", "project_id", "event_type", "actor_user_sub", "actor_user_email", "details", "created_at"}

	mock.ExpectQuery(`WHERE project_id = \$1`).
		WithArgs(projID, 50).
		WillReturnRows(sqlmock.NewRows(cols).
			AddRow(uuid.New(), uuid.New(), projID, string(EventAddonDestroyed),
				sql.NullString{}, sql.NullString{}, []byte(`{}`), now))

	events, err := repo.ListByProject(context.Background(), projID, 50)
	require.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, EventAddonDestroyed, events[0].EventType)
}
