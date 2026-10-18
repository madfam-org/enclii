package db

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeploymentRepository_GetLatestByServiceAndEnvironment(t *testing.T) {
	cols := []string{"id", "release_id", "environment_id", "replicas", "status", "health", "error_message",
		"service_id", "version_number", "created_at", "updated_at"}
	svc, env := uuid.New(), uuid.New()
	tests := []struct {
		name      string
		found     bool
		wantErrIs error
	}{
		{name: "latest deployment in that environment", found: true},
		{name: "no deployment in that environment", wantErrIs: sql.ErrNoRows},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = database.Close() }()
			repo := NewDeploymentRepository(database)

			q := mock.ExpectQuery(`WHERE r.service_id = \$1 AND d.environment_id = \$2\s+ORDER BY d.created_at DESC\s+LIMIT 1`).WithArgs(svc, env)
			if tt.found {
				q.WillReturnRows(sqlmock.NewRows(cols).AddRow(uuid.New(), uuid.New(), env, 2, "running", "healthy", nil, svc, 7, time.Now(), time.Now()))
			} else {
				q.WillReturnError(sql.ErrNoRows)
			}

			got, err := repo.GetLatestByServiceAndEnvironment(context.Background(), svc, env)
			if tt.wantErrIs != nil {
				assert.ErrorIs(t, err, tt.wantErrIs)
				assert.Nil(t, got)
			} else {
				require.NoError(t, err)
				assert.Equal(t, env, got.EnvironmentID)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
