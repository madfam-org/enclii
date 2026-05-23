package api

import (
	"encoding/json"
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

func setupAuthzHandler(t *testing.T) (*Handler, sqlmock.Sqlmock, func()) {
	t.Helper()
	database, mock, err := sqlmock.New()
	require.NoError(t, err)

	h := &Handler{
		repos: &db.Repositories{
			Projects:      db.NewProjectRepository(database),
			Services:      db.NewServiceRepository(database),
			CronJobs:      db.NewCronJobRepository(database),
			ProjectAccess: db.NewProjectAccessRepository(database),
		},
		logger: testLogger(t),
	}
	return h, mock, func() { _ = database.Close() }
}

func withUserContext(userID uuid.UUID, roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("user_id", userID.String())
		c.Set("user_roles", roles)
		c.Next()
	}
}

func assertErrorCode(t *testing.T, w *httptest.ResponseRecorder, wantCode string) {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, wantCode, body.Error.Code)
}

func TestAuthZMatrix_EnforceUserProjectAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)

	userA := uuid.New()
	projectOwned := uuid.New()
	projectOther := uuid.New()

	cases := []struct {
		name       string
		roles      []string
		userID     uuid.UUID
		projectID  uuid.UUID
		accessRows int
		wantOK     bool
		wantCode   string
	}{
		{
			name:      "admin bypass",
			roles:     []string{"admin"},
			userID:    userA,
			projectID: projectOther,
			wantOK:    true,
		},
		{
			name:       "member with access",
			roles:      []string{"developer"},
			userID:     userA,
			projectID:  projectOwned,
			accessRows: 1,
			wantOK:     true,
		},
		{
			name:       "member without access",
			roles:      []string{"developer"},
			userID:     userA,
			projectID:  projectOther,
			accessRows: 0,
			wantOK:     false,
			wantCode:   "NOT_FOUND",
		},
		{
			name:      "unauthenticated",
			roles:     nil,
			userID:    uuid.Nil,
			projectID: projectOwned,
			wantOK:    false,
			wantCode:  "UNAUTHORIZED",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, mock, cleanup := setupAuthzHandler(t)
			defer cleanup()

			if tc.userID != uuid.Nil && tc.roles != nil && tc.roles[0] != "admin" {
				mock.ExpectQuery(`SELECT COUNT\(\*\) FROM project_access`).
					WithArgs(tc.userID, tc.projectID).
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(tc.accessRows))
			}

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.userID != uuid.Nil {
				c.Set("user_id", tc.userID.String())
			}
			if tc.roles != nil {
				c.Set("user_roles", tc.roles)
			}

			ok := h.enforceUserProjectAccess(c, tc.projectID)
			assert.Equal(t, tc.wantOK, ok)
			if !tc.wantOK {
				assertErrorCode(t, w, tc.wantCode)
			}
			if tc.userID != uuid.Nil && tc.roles != nil && tc.roles[0] != "admin" {
				assert.NoError(t, mock.ExpectationsWereMet())
			}
		})
	}
}

func TestGetCronJob_CrossTenantDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, mock, cleanup := setupAuthzHandler(t)
	defer cleanup()

	userA := uuid.New()
	projectB := uuid.New()
	cronID := uuid.New()
	now := time.Now()

	mock.ExpectQuery(`FROM cron_jobs WHERE id`).
		WillReturnRows(sqlmock.NewRows(cronJobSelectColumns).AddRow(
			cronID, projectB, uuid.New(), "nightly", "0 0 * * *", "echo", "",
			300, 0, false, "Allow",
			now, now, nil, nil,
		))

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM project_access`).
		WithArgs(userA, projectB).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.Use(withUserContext(userA, "developer"))
	engine.GET("/v1/cron-jobs/:id", h.GetCronJob)

	req, _ := http.NewRequest(http.MethodGet, "/v1/cron-jobs/"+cronID.String(), nil)
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assertErrorCode(t, w, "NOT_FOUND")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateService_CrossTenantDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, mock, cleanup := setupAuthzHandler(t)
	defer cleanup()

	userA := uuid.New()
	projectB := uuid.New()
	serviceID := uuid.New()

	mock.ExpectQuery(`FROM services WHERE id = \$1`).
		WillReturnRows(sqlmock.NewRows(serviceListByGitRepoColumns).AddRow(
			serviceID, projectB, "api", "https://github.com/org/repo", "",
			[]byte(`{"type":"dockerfile"}`),
			true, "main", "production",
			time.Now(), time.Now(), []byte(`[]`), "web", "default",
		))

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM project_access`).
		WithArgs(userA, projectB).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.Use(withUserContext(userA, "developer"))
	engine.PATCH("/v1/services/:id", h.UpdateService)

	req, _ := http.NewRequest(http.MethodPatch, "/v1/services/"+serviceID.String(), nil)
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assertErrorCode(t, w, "NOT_FOUND")
	assert.NoError(t, mock.ExpectationsWereMet())
}
