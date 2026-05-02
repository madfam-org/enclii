package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
)

// newTenantsTestHandler builds a Handler with sqlmock-backed repositories
// scoped to the small surface the tenant endpoints need (Teams + sessions).
// Other Handler fields are left nil — these tests must not exercise them.
func newTenantsTestHandler(t *testing.T) (*Handler, sqlmock.Sqlmock, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	repos := &db.Repositories{
		Teams:               db.NewTeamRepository(mockDB),
		AdminActingSessions: db.NewAdminActingSessionRepository(mockDB),
	}
	h := &Handler{repos: repos}
	return h, mock, func() { _ = mockDB.Close() }
}

// withAdminContext sets the user_id key the handlers expect (the admin route
// group middleware would normally do this; these tests bypass the middleware).
func withAdminContext(adminID uuid.UUID, c *gin.Context) {
	c.Set("user_id", adminID.String())
}

func TestListTenants_NilRepos(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{} // no repos
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/admin/tenants", nil)

	h.ListTenants(c)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestListTenants_HappyPath(t *testing.T) {
	h, mock, cleanup := newTenantsTestHandler(t)
	defer cleanup()

	teamID1 := uuid.New()
	teamID2 := uuid.New()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, name, slug, description, avatar_url, billing_email, owner_id, settings, created_at, updated_at`)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "slug", "description", "avatar_url", "billing_email", "owner_id", "settings", "created_at", "updated_at",
		}).
			AddRow(teamID1, "Dhanam", "dhanam", nil, nil, nil, nil, []byte(`{}`), time.Now(), time.Now()).
			AddRow(teamID2, "Karafiel", "karafiel", nil, nil, nil, nil, []byte(`{}`), time.Now(), time.Now()))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT team_id, COUNT(*)`)).
		WillReturnRows(sqlmock.NewRows([]string{"team_id", "count"}).
			AddRow(teamID1, 3).
			AddRow(teamID2, 7))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/admin/tenants", nil)
	h.ListTenants(c)

	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Tenants []TenantListResponse `json:"tenants"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Tenants, 2)
	// Counts must come from the second query, not zero.
	for _, tt := range body.Tenants {
		switch tt.Slug {
		case "dhanam":
			assert.Equal(t, 3, tt.ProjectCount)
		case "karafiel":
			assert.Equal(t, 7, tt.ProjectCount)
		}
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEnterTenant_RequiresSlug(t *testing.T) {
	h, _, cleanup := newTenantsTestHandler(t)
	defer cleanup()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/admin/tenants//enter", nil)
	c.Params = gin.Params{{Key: "slug", Value: ""}}
	withAdminContext(uuid.New(), c)

	h.EnterTenant(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestEnterTenant_HappyPath_SetsCookieAndReturnsSession(t *testing.T) {
	h, mock, cleanup := newTenantsTestHandler(t)
	defer cleanup()

	adminID := uuid.New()
	teamID := uuid.New()

	// GetBySlug → returns the team.
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, name, slug, description, avatar_url, billing_email, owner_id, settings, created_at, updated_at
			FROM teams WHERE slug = $1`)).
		WithArgs("dhanam").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "slug", "description", "avatar_url", "billing_email", "owner_id", "settings", "created_at", "updated_at",
		}).
			AddRow(teamID, "Dhanam", "dhanam", nil, nil, nil, nil, []byte(`{}`), time.Now(), time.Now()))

	// Start: closes prior open sessions, then INSERT.
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE admin_acting_sessions
			   SET ended_at = now()
			 WHERE admin_user_id = $1 AND ended_at IS NULL`)).
		WithArgs(adminID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO admin_acting_sessions`)).
		WillReturnResult(sqlmock.NewResult(1, 1))

	body := strings.NewReader(`{"reason":"customer support call","duration_seconds":900}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/admin/tenants/dhanam/enter", body)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "slug", Value: "dhanam"}}
	withAdminContext(adminID, c)

	h.EnterTenant(c)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	var resp ActiveActingSessionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Active)
	require.NotNil(t, resp.Tenant)
	assert.Equal(t, "dhanam", resp.Tenant.Slug)

	// Cookie must be set with the correct name and value.
	cookies := w.Result().Cookies()
	var found *http.Cookie
	for _, ck := range cookies {
		if ck.Name == actingAsCookieName {
			found = ck
			break
		}
	}
	require.NotNil(t, found, "expected ax_acting_as cookie to be set")
	assert.Equal(t, "dhanam", found.Value)
	assert.True(t, found.HttpOnly, "cookie must be HttpOnly")
	assert.Equal(t, "/v1", found.Path, "cookie must be scoped to /v1")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEnterTenant_DurationCappedAtMax(t *testing.T) {
	// Verify that an absurdly long requested duration is clamped to
	// maxActingSessionTTL before being persisted. We assert via the cookie
	// MaxAge, which is derived from the same clamped value.
	h, mock, cleanup := newTenantsTestHandler(t)
	defer cleanup()

	adminID := uuid.New()
	teamID := uuid.New()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, name, slug`)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "slug", "description", "avatar_url", "billing_email", "owner_id", "settings", "created_at", "updated_at",
		}).AddRow(teamID, "Dhanam", "dhanam", nil, nil, nil, nil, []byte(`{}`), time.Now(), time.Now()))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE admin_acting_sessions
			   SET ended_at = now()`)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO admin_acting_sessions`)).
		WillReturnResult(sqlmock.NewResult(1, 1))

	body := bytes.NewReader([]byte(`{"duration_seconds":999999}`))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/admin/tenants/dhanam/enter", body)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "slug", Value: "dhanam"}}
	withAdminContext(adminID, c)

	h.EnterTenant(c)
	require.Equal(t, http.StatusOK, w.Code)

	for _, ck := range w.Result().Cookies() {
		if ck.Name == actingAsCookieName {
			assert.LessOrEqual(t, ck.MaxAge, int(maxActingSessionTTL.Seconds()),
				"cookie MaxAge must be clamped to the server-side max")
			return
		}
	}
	t.Fatal("cookie not set")
}

func TestExitTenant_ClosesSessionAndClearsCookie(t *testing.T) {
	h, mock, cleanup := newTenantsTestHandler(t)
	defer cleanup()

	adminID := uuid.New()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE admin_acting_sessions
		   SET ended_at = now()
		 WHERE admin_user_id = $1 AND ended_at IS NULL`)).
		WithArgs(adminID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/admin/tenants/exit", nil)
	withAdminContext(adminID, c)

	h.ExitTenant(c)
	require.Equal(t, http.StatusOK, w.Code)

	var resp ActiveActingSessionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp.Active)

	for _, ck := range w.Result().Cookies() {
		if ck.Name == actingAsCookieName {
			assert.Equal(t, -1, ck.MaxAge, "exit must mark cookie for deletion")
			return
		}
	}
	t.Fatal("expected exit handler to clear ax_acting_as cookie")
}

func TestActiveTenant_NoSessionReturns200False(t *testing.T) {
	h, mock, cleanup := newTenantsTestHandler(t)
	defer cleanup()

	adminID := uuid.New()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, admin_user_id, tenant_team_id`)).
		WithArgs(adminID).
		WillReturnError(sql.ErrNoRows)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/admin/tenants/active", nil)
	withAdminContext(adminID, c)

	h.ActiveTenant(c)
	require.Equal(t, http.StatusOK, w.Code, "no session is not an error condition")
	var resp ActiveActingSessionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp.Active)
	assert.Nil(t, resp.Tenant)
}

func TestActiveTenant_ActiveSessionReturnsTenant(t *testing.T) {
	h, mock, cleanup := newTenantsTestHandler(t)
	defer cleanup()

	adminID := uuid.New()
	sessionID := uuid.New()
	teamID := uuid.New()
	expires := time.Now().Add(time.Hour)
	startedAt := time.Now().Add(-30 * time.Minute)

	cols := []string{"id", "admin_user_id", "tenant_team_id", "started_at", "expires_at", "ended_at", "reason", "client_ip", "user_agent"}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, admin_user_id, tenant_team_id`)).
		WithArgs(adminID).
		WillReturnRows(sqlmock.NewRows(cols).
			AddRow(sessionID, adminID, teamID, startedAt, expires, nil, nil, nil, nil))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, name, slug, description, avatar_url, billing_email, owner_id, settings, created_at, updated_at
			FROM teams WHERE id = $1`)).
		WithArgs(teamID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "slug", "description", "avatar_url", "billing_email", "owner_id", "settings", "created_at", "updated_at",
		}).AddRow(teamID, "Dhanam", "dhanam", nil, nil, nil, nil, []byte(`{}`), time.Now(), time.Now()))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/admin/tenants/active", nil)
	withAdminContext(adminID, c)

	h.ActiveTenant(c)
	require.Equal(t, http.StatusOK, w.Code)
	var resp ActiveActingSessionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Active)
	require.NotNil(t, resp.Tenant)
	assert.Equal(t, "dhanam", resp.Tenant.Slug)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestActiveTenant_DanglingSessionAutoCloses(t *testing.T) {
	// If the session row exists but the tenant team has been deleted, we
	// must close the session, clear the cookie, and report active=false
	// rather than 500 the SPA.
	h, mock, cleanup := newTenantsTestHandler(t)
	defer cleanup()

	adminID := uuid.New()
	sessionID := uuid.New()
	teamID := uuid.New()
	cols := []string{"id", "admin_user_id", "tenant_team_id", "started_at", "expires_at", "ended_at", "reason", "client_ip", "user_agent"}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, admin_user_id, tenant_team_id`)).
		WithArgs(adminID).
		WillReturnRows(sqlmock.NewRows(cols).
			AddRow(sessionID, adminID, teamID, time.Now().Add(-time.Hour), time.Now().Add(time.Hour), nil, nil, nil, nil))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, name, slug, description, avatar_url, billing_email, owner_id, settings, created_at, updated_at
			FROM teams WHERE id = $1`)).
		WithArgs(teamID).
		WillReturnError(sql.ErrNoRows)

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE admin_acting_sessions
		   SET ended_at = now()`)).
		WithArgs(adminID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/admin/tenants/active", nil)
	withAdminContext(adminID, c)

	h.ActiveTenant(c)
	require.Equal(t, http.StatusOK, w.Code)
	var resp ActiveActingSessionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp.Active)
}

// quietContext returns a context.Background derivative — placeholder for
// future tests that want to assert log output stays clean.
func quietContext() context.Context { return context.Background() }
