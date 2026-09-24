package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
)

func serviceDeploymentsFixture(t *testing.T) (*gin.Engine, sqlmock.Sqlmock) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	h := &Handler{
		repos: &db.Repositories{
			Services:    db.NewServiceRepository(database),
			Releases:    db.NewReleaseRepository(database),
			Deployments: db.NewDeploymentRepository(database),
		},
		logger: testLogger(t),
	}
	engine := gin.New()
	engine.Use(withPlatformAdminContext(uuid.New()))
	engine.GET("/v1/services/:id/deployments", h.ListServiceDeployments)
	return engine, mock
}

func expectReleases(mock sqlmock.Sqlmock, serviceID uuid.UUID, releaseIDs ...uuid.UUID) {
	rows := sqlmock.NewRows(releaseRowColumns)
	for _, id := range releaseIDs {
		rows.AddRow(id, serviceID, "v", "img", "sha", "ready", nil, nil, nil, nil, nil, nil, time.Now(), time.Now())
	}
	mock.ExpectQuery(`FROM releases WHERE service_id = \$1`).WillReturnRows(rows)
}

func deploymentsOfRelease(releaseID, serviceID uuid.UUID) *sqlmock.Rows {
	return sqlmock.NewRows(deploymentRowColumns).AddRow(
		uuid.New(), releaseID, uuid.New(), 1, "running", "healthy", nil, serviceID, 1, time.Now(), time.Now())
}

func getServiceDeployments(t *testing.T, engine *gin.Engine, serviceID uuid.UUID) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/services/"+serviceID.String()+"/deployments", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body
}

func TestListServiceDeployments_CompleteListIsNotTruncated(t *testing.T) {
	engine, mock := serviceDeploymentsFixture(t)
	serviceID, projectID := uuid.New(), uuid.New()
	r1, r2 := uuid.New(), uuid.New()
	expectServiceRow(mock, serviceID, projectID) // access check
	expectReleases(mock, serviceID, r1, r2)
	mock.ExpectQuery(`FROM deployments WHERE release_id = \$1`).WithArgs(r1.String()).WillReturnRows(deploymentsOfRelease(r1, serviceID))
	mock.ExpectQuery(`FROM deployments WHERE release_id = \$1`).WithArgs(r2.String()).WillReturnRows(deploymentsOfRelease(r2, serviceID))

	body := getServiceDeployments(t, engine, serviceID)
	require.NoError(t, mock.ExpectationsWereMet())

	keys := make([]string, 0, len(body))
	for k := range body {
		keys = append(keys, k)
	}
	assert.ElementsMatch(t, []string{"service_id", "deployments", "count", "truncated"}, keys,
		"existing fields unchanged; truncated is the only addition")
	assert.Equal(t, float64(2), body["count"])
	assert.Equal(t, false, body["truncated"])
}

func TestListServiceDeployments_FailedReleaseReadMarksTruncated(t *testing.T) {
	engine, mock := serviceDeploymentsFixture(t)
	serviceID, projectID := uuid.New(), uuid.New()
	r1, r2, r3 := uuid.New(), uuid.New(), uuid.New()
	expectServiceRow(mock, serviceID, projectID)
	expectReleases(mock, serviceID, r1, r2, r3)
	mock.ExpectQuery(`FROM deployments WHERE release_id = \$1`).WithArgs(r1.String()).WillReturnRows(deploymentsOfRelease(r1, serviceID))
	mock.ExpectQuery(`FROM deployments WHERE release_id = \$1`).WithArgs(r2.String()).WillReturnError(errors.New("connection reset"))
	mock.ExpectQuery(`FROM deployments WHERE release_id = \$1`).WithArgs(r3.String()).WillReturnRows(deploymentsOfRelease(r3, serviceID))

	body := getServiceDeployments(t, engine, serviceID)
	require.NoError(t, mock.ExpectationsWereMet())

	assert.Equal(t, float64(2), body["count"], "the rows that could be read are still returned")
	assert.Equal(t, true, body["truncated"])
	assert.Equal(t, []any{r2.String()}, body["skipped_release_ids"])
}

func TestListServiceDeployments_RowIterationErrorMarksTruncated(t *testing.T) {
	engine, mock := serviceDeploymentsFixture(t)
	serviceID, projectID := uuid.New(), uuid.New()
	r1 := uuid.New()
	expectServiceRow(mock, serviceID, projectID)
	expectReleases(mock, serviceID, r1)
	// The first row scans, then the cursor fails: ListByRelease used to return
	// the rows read so far as if complete.
	mock.ExpectQuery(`FROM deployments WHERE release_id = \$1`).WithArgs(r1.String()).
		WillReturnRows(deploymentsOfRelease(r1, serviceID).
			AddRow(uuid.New(), r1, uuid.New(), 1, "running", "healthy", nil, serviceID, 2, time.Now(), time.Now()).
			RowError(1, errors.New("cursor lost")))

	body := getServiceDeployments(t, engine, serviceID)
	require.NoError(t, mock.ExpectationsWereMet())
	assert.Equal(t, true, body["truncated"])
	assert.Equal(t, []any{r1.String()}, body["skipped_release_ids"])
}
