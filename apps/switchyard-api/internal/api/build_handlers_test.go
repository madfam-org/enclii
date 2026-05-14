package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/clients"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func TestEnqueueToRoundhouse_FailsReleaseOnRoundhouseError(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = database.Close() }()

	roundhouse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/internal/enqueue", r.URL.Path)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer roundhouse.Close()

	projectID := uuid.New()
	serviceID := uuid.New()
	releaseID := uuid.New()
	now := time.Now()

	mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects WHERE id = \$1`).
		WithArgs(projectID).
		WillReturnRows(sqlmock.NewRows(projectSelectColumns).
			AddRow(projectID, "Enclii", "enclii", "", now, now))
	mock.ExpectExec(`UPDATE releases SET status = \$1, error_message = \$2, updated_at = NOW\(\) WHERE id = \$3`).
		WithArgs(types.ReleaseStatusFailed, sqlmock.AnyArg(), releaseID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	h := &Handler{
		repos: &db.Repositories{
			Projects: db.NewProjectRepository(database),
			Releases: db.NewReleaseRepository(database),
		},
		config: &config.Config{
			SelfURL: "http://switchyard-api",
		},
		logger:           testLogger(t),
		roundhouseClient: clients.NewRoundhouseClient(roundhouse.URL, ""),
	}

	h.enqueueToRoundhouse(context.Background(),
		&types.Service{
			ID:        serviceID,
			ProjectID: projectID,
			Name:      "switchyard-api",
			GitRepo:   "https://github.com/madfam-org/enclii.git",
		},
		&types.Release{ID: releaseID},
		"abc123456789",
		"main",
	)

	assert.NoError(t, mock.ExpectationsWereMet())
}
