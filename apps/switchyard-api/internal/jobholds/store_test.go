package jobholds

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestJobHoldTransactionLock(t *testing.T) {
	for _, scenario := range []string{"success", "callback-error", "lock-error", "commit-error"} {
		t.Run(scenario, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			store := &Store{DB: database}
			expected := errors.New("fixture failure")
			called := false
			mock.ExpectBegin()
			lock := mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WithArgs("enclii.job-hold:fixture-dev/nightly")
			if scenario == "lock-error" {
				lock.WillReturnError(expected)
			} else {
				lock.WillReturnResult(sqlmock.NewResult(0, 1))
			}
			if scenario == "lock-error" || scenario == "callback-error" {
				mock.ExpectRollback()
			} else if scenario == "commit-error" {
				mock.ExpectCommit().WillReturnError(expected)
			} else {
				mock.ExpectCommit()
			}
			err = store.WithLock(context.Background(), "fixture-dev", "nightly", func(ctx context.Context, _ *Session) error {
				called = true
				_, deadline := ctx.Deadline()
				require.True(t, deadline, "lock and mutation must share a bounded context")
				if scenario == "callback-error" {
					return expected
				}
				return nil
			})
			if scenario == "success" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, expected)
			}
			require.Equal(t, scenario != "lock-error", called)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
