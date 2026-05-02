package db

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newActingSessionMock(t *testing.T) (*AdminActingSessionRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	return NewAdminActingSessionRepository(db), mock, func() { _ = db.Close() }
}

func TestActingSession_Start_ClosesPriorOpenSession(t *testing.T) {
	repo, mock, cleanup := newActingSessionMock(t)
	defer cleanup()

	adminID := uuid.New()
	teamID := uuid.New()
	expires := time.Now().Add(time.Hour)

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE admin_acting_sessions
		   SET ended_at = now()
		 WHERE admin_user_id = $1 AND ended_at IS NULL`)).
		WithArgs(adminID).
		WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO admin_acting_sessions`)).
		WithArgs(
			sqlmock.AnyArg(), adminID, teamID,
			sqlmock.AnyArg(), expires,
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	row, err := repo.Start(context.Background(), adminID, teamID, expires, "white-glove debug", "203.0.113.5", "ua")
	require.NoError(t, err)
	assert.Equal(t, adminID, row.AdminUserID)
	assert.Equal(t, teamID, row.TenantTeamID)
	assert.Equal(t, "white-glove debug", *row.Reason)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestActingSession_GetActive_ExpiredAutoCloses(t *testing.T) {
	repo, mock, cleanup := newActingSessionMock(t)
	defer cleanup()

	adminID := uuid.New()
	id := uuid.New()
	teamID := uuid.New()
	expired := time.Now().Add(-time.Minute)

	cols := []string{"id", "admin_user_id", "tenant_team_id", "started_at", "expires_at", "ended_at", "reason", "client_ip", "user_agent"}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, admin_user_id, tenant_team_id`)).
		WithArgs(adminID).
		WillReturnRows(sqlmock.NewRows(cols).
			AddRow(id, adminID, teamID, time.Now().Add(-time.Hour), expired, nil, nil, nil, nil))

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE admin_acting_sessions SET ended_at = now() WHERE id = $1`)).
		WithArgs(id).
		WillReturnResult(sqlmock.NewResult(0, 1))

	_, err := repo.GetActive(context.Background(), adminID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoActiveActingSession))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestActingSession_GetActive_NoneFound(t *testing.T) {
	repo, mock, cleanup := newActingSessionMock(t)
	defer cleanup()

	adminID := uuid.New()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, admin_user_id, tenant_team_id`)).
		WithArgs(adminID).
		WillReturnError(sql.ErrNoRows)

	_, err := repo.GetActive(context.Background(), adminID)
	assert.True(t, errors.Is(err, ErrNoActiveActingSession))
}

func TestActingSession_EndAll(t *testing.T) {
	repo, mock, cleanup := newActingSessionMock(t)
	defer cleanup()

	adminID := uuid.New()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE admin_acting_sessions
		   SET ended_at = now()
		 WHERE admin_user_id = $1 AND ended_at IS NULL`)).
		WithArgs(adminID).
		WillReturnResult(sqlmock.NewResult(0, 2))

	n, err := repo.EndAll(context.Background(), adminID)
	require.NoError(t, err)
	assert.EqualValues(t, 2, n)
	require.NoError(t, mock.ExpectationsWereMet())
}
