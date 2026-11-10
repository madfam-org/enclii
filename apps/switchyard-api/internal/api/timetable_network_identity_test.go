package api

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// postOneOffJob runs CreateOneOffJob against a mocked project lookup and, when
// expectInsert is set, a mocked insert whose last argument must equal
// wantIdentity.
func postOneOffJob(t *testing.T, networkIdentityJSON string, expectInsert bool, wantIdentity string) *httptest.ResponseRecorder {
	t.Helper()
	h, mock, cleanup := setupTimetableTestHandler(t)
	defer cleanup()

	projectID, serviceID, now := uuid.New(), uuid.New(), time.Now()

	mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects WHERE slug`).
		WithArgs("test-project").
		WillReturnRows(sqlmock.NewRows(projectSelectColumns).
			AddRow(projectID, "Test Project", "test-project", "github", now, now))

	if expectInsert {
		mock.ExpectExec(`INSERT INTO one_off_jobs`).
			WithArgs(sqlmock.AnyArg(), projectID, serviceID, "grant-dryrun", "node script.js -",
				"", 300, sqlmock.AnyArg(), "pending", sqlmock.AnyArg(), wantIdentity).
			WillReturnResult(sqlmock.NewResult(0, 1))
	}

	router := gin.New()
	withTestAdminContext(router)
	router.POST("/v1/projects/:slug/one-off-jobs", h.CreateOneOffJob)

	body := fmt.Sprintf(`{"service_id": %q, "name": "grant-dryrun", "command": "node script.js -"%s}`,
		serviceID.String(), networkIdentityJSON)
	req := httptest.NewRequest("POST", "/v1/projects/test-project/one-off-jobs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.NoError(t, mock.ExpectationsWereMet())
	return w
}

func TestCreateOneOffJob_NetworkIdentity(t *testing.T) {
	t.Run("omitted keeps the default of no identity", func(t *testing.T) {
		w := postOneOffJob(t, "", true, "")
		assert.Equal(t, http.StatusCreated, w.Code)
		assert.NotContains(t, w.Body.String(), "network_identity")
	})

	t.Run("service is stored and echoed back", func(t *testing.T) {
		w := postOneOffJob(t, `, "network_identity": "service"`, true, "service")
		assert.Equal(t, http.StatusCreated, w.Code)
		assert.Contains(t, w.Body.String(), `"network_identity":"service"`)
	})

	t.Run("an unknown value is refused before anything is stored", func(t *testing.T) {
		w := postOneOffJob(t, `, "network_identity": "namespace"`, false, "")
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid network_identity")
	})
}
