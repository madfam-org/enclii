package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// TestGetActivity_ScopedForNonPlatformCallers: outside acting-as, only the
// platform rank reads the unscoped feed. Everyone else is limited to rows
// they produced, rows for projects they hold a grant on, and (tenant admins)
// rows for their own tenants.
func TestGetActivity_ScopedForNonPlatformCallers(t *testing.T) {
	const visibleWhere = `(?s)WHERE \(\s*actor_id = \$1\s+OR project_id IN \(\s*SELECT project_id FROM project_access\s+WHERE user_id = \$1`
	const tenantClause = `OR project_id IN \(SELECT id FROM projects WHERE team_id = ANY\(\$2::uuid\[\]\)\)\s+OR acting_on_behalf_of_team_id = ANY\(\$2::uuid\[\]\)`

	t.Run("developer sees own and granted rows", func(t *testing.T) {
		h, mock, cleanup := newXc2TestHandler(t)
		defer cleanup()
		uid := uuid.New()
		mock.ExpectQuery(visibleWhere+`.*\)\s+ORDER BY timestamp DESC`).
			WithArgs(uid, 50, 0).
			WillReturnRows(sqlmock.NewRows(activityScanColumns))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/activity", nil)
		c.Set("user_id", uid.String())
		c.Set("user_role", "developer")
		h.GetActivity(c)

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("filters are applied inside the scope", func(t *testing.T) {
		h, mock, cleanup := newXc2TestHandler(t)
		defer cleanup()
		uid, other := uuid.New(), uuid.New()
		mock.ExpectQuery(visibleWhere+`.*\)\s+AND actor_id = \$2 AND action = \$3`).
			WithArgs(uid, other, "deploy", 50, 0).
			WillReturnRows(sqlmock.NewRows(activityScanColumns))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/activity?actor_id="+other.String()+"&action=deploy", nil)
		c.Set("user_id", uid.String())
		c.Set("user_role", "viewer")
		h.GetActivity(c)

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("tenant admin also sees its tenants", func(t *testing.T) {
		h, mock, cleanup := newXc2TestHandler(t)
		defer cleanup()
		uid, team := uuid.New(), uuid.New()
		// Platform rank lookup (admin role but not platform admin), then the
		// tenant membership lookup, then the scoped query.
		mock.ExpectQuery(`SELECT is_platform_admin FROM users`).WithArgs(uid).
			WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(false))
		mock.ExpectQuery(`SELECT team_id FROM team_members`).WithArgs(uid).
			WillReturnRows(sqlmock.NewRows([]string{"team_id"}).AddRow(team))
		mock.ExpectQuery(visibleWhere+`.*`+tenantClause).
			WithArgs(uid, pq.StringArray{team.String()}, 50, 0).
			WillReturnRows(sqlmock.NewRows(activityScanColumns))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/activity", nil)
		c.Set("user_id", uid.String())
		c.Set("user_role", "admin")
		h.GetActivity(c)

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("platform admin resolved from the database keeps the full feed", func(t *testing.T) {
		h, mock, cleanup := newXc2TestHandler(t)
		defer cleanup()
		uid := uuid.New()
		mock.ExpectQuery(`SELECT is_platform_admin FROM users`).WithArgs(uid).
			WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(true))
		mock.ExpectQuery(`(?s)WHERE 1=1\s+ORDER BY timestamp DESC`).
			WithArgs(50, 0).
			WillReturnRows(sqlmock.NewRows(activityScanColumns))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/activity", nil)
		c.Set("user_id", uid.String())
		c.Set("user_role", "admin")
		h.GetActivity(c)

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("a role string alone never unlocks the full feed", func(t *testing.T) {
		h, mock, cleanup := newXc2TestHandler(t)
		defer cleanup()
		uid := uuid.New()
		// The strings normalize to tenant_admin, so tenant membership is
		// read; this caller belongs to no tenant.
		mock.ExpectQuery(`SELECT team_id FROM team_members`).WithArgs(uid).
			WillReturnRows(sqlmock.NewRows([]string{"team_id"}))
		mock.ExpectQuery(visibleWhere).
			WithArgs(uid, 50, 0).
			WillReturnRows(sqlmock.NewRows(activityScanColumns))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/activity", nil)
		c.Set("user_id", uid.String())
		c.Set("user_roles", []string{"platform_admin", "superadmin"}) // normalize to tenant_admin
		c.Set("is_platform_admin", false)
		h.GetActivity(c)

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("unauthenticated caller gets 401 and no query", func(t *testing.T) {
		h, mock, cleanup := newXc2TestHandler(t)
		defer cleanup()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/activity", nil)
		h.GetActivity(c)
		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}
