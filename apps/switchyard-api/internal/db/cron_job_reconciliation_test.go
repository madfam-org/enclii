package db

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCronJobRepositoryListForReconciliation(t *testing.T) {
	query := regexp.QuoteMeta(`SELECT id, project_id, service_id, name, schedule, command, image,
 timeout, retries, suspended, concurrency, created_at, updated_at, last_run_at, next_run_at
 FROM cron_jobs ORDER BY created_at ASC`)
	t.Run("includes active and suspended", func(t *testing.T) {
		repo, mock, cleanup := newCronJobMockDB(t)
		defer cleanup()
		now := time.Now()
		rows := sqlmock.NewRows(cronJobColumns)
		for _, paused := range []bool{false, true} {
			rows.AddRow(uuid.New(), uuid.New(), uuid.New(), "fixture", "0 2 * * *", "echo fixture", nil, 60, 0, paused, "forbid", now, now, nil, nil)
		}
		mock.ExpectQuery(query).WillReturnRows(rows)
		jobs, err := repo.ListForReconciliation(context.Background())
		require.NoError(t, err)
		require.Len(t, jobs, 2)
		require.False(t, jobs[0].Suspended)
		require.True(t, jobs[1].Suspended)
		require.NoError(t, mock.ExpectationsWereMet())
	})
	t.Run("query failure", func(t *testing.T) {
		repo, mock, cleanup := newCronJobMockDB(t)
		defer cleanup()
		expected := errors.New("unavailable")
		mock.ExpectQuery(query).WillReturnError(expected)
		jobs, err := repo.ListForReconciliation(context.Background())
		require.ErrorIs(t, err, expected)
		require.Nil(t, jobs)
		require.NoError(t, mock.ExpectationsWereMet())
	})
	t.Run("incomplete read fails closed", func(t *testing.T) {
		repo, mock, cleanup := newCronJobMockDB(t)
		defer cleanup()
		now := time.Now()
		expected := errors.New("read interrupted")
		rows := sqlmock.NewRows(cronJobColumns).AddRow(uuid.New(), uuid.New(), uuid.New(), "fixture", "0 2 * * *", "echo fixture", nil, 60, 0, true, "forbid", now, now, nil, nil).RowError(0, expected)
		mock.ExpectQuery(query).WillReturnRows(rows)
		_, err := repo.ListForReconciliation(context.Background())
		require.ErrorIs(t, err, expected)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}
