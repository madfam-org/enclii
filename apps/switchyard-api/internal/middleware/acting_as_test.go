package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeResolver is a test double for ActingAsContext. It records the lookups
// made against it and returns whatever was preset.
type fakeResolver struct {
	teamID    uuid.UUID
	teamSlug  string
	ok        bool
	calls     int
	lastAdmin uuid.UUID
}

func (f *fakeResolver) GetActiveActingSession(_ context.Context, adminUserID uuid.UUID) (uuid.UUID, string, bool) {
	f.calls++
	f.lastAdmin = adminUserID
	return f.teamID, f.teamSlug, f.ok
}

// runWithMiddleware wires the middleware in front of a probe handler that
// captures the resulting acting-as context state. Auth state is preset on the
// gin context as if AuthMiddleware had already run.
func runWithMiddleware(t *testing.T, mw gin.HandlerFunc, req *http.Request, presetCtx func(c *gin.Context)) (*httptest.ResponseRecorder, gin.H) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()

	// Stage auth context the way AuthMiddleware would.
	r.Use(func(c *gin.Context) {
		if presetCtx != nil {
			presetCtx(c)
		}
		c.Next()
	})
	r.Use(mw)

	out := gin.H{}
	r.GET("/probe", func(c *gin.Context) {
		id, ok := ActingTeamID(c)
		out["acting_team_id"] = ""
		out["is_acting_as"] = IsActingAs(c)
		out["slug"] = ActingTeamSlug(c)
		if ok {
			out["acting_team_id"] = id.String()
		}
		c.JSON(http.StatusOK, out)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w, out
}

func TestActingAsMiddleware_NilResolver_NoOp(t *testing.T) {
	mw := ActingAsMiddleware(nil)
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)

	_, out := runWithMiddleware(t, mw, req, nil)
	assert.False(t, out["is_acting_as"].(bool))
	assert.Equal(t, "", out["acting_team_id"])
}

func TestActingAsMiddleware_NoCookie_NoOp(t *testing.T) {
	r := &fakeResolver{}
	mw := ActingAsMiddleware(r)
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)

	_, out := runWithMiddleware(t, mw, req, func(c *gin.Context) {
		c.Set("user_id", uuid.New().String())
		c.Set("user_roles", []string{"admin"})
	})
	assert.False(t, out["is_acting_as"].(bool))
	assert.Equal(t, 0, r.calls, "resolver must not be called without a cookie")
}

func TestActingAsMiddleware_NonAdmin_IgnoresCookie(t *testing.T) {
	r := &fakeResolver{ok: true, teamID: uuid.New(), teamSlug: "dhanam"}
	mw := ActingAsMiddleware(r)
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.AddCookie(&http.Cookie{Name: actingAsCookieName, Value: "dhanam"})

	_, out := runWithMiddleware(t, mw, req, func(c *gin.Context) {
		c.Set("user_id", uuid.New().String())
		c.Set("user_roles", []string{"developer"}) // no admin
	})
	assert.False(t, out["is_acting_as"].(bool), "non-admin must never trigger team-scoping")
	assert.Equal(t, 0, r.calls, "non-admin path must not consult the resolver")
}

func TestActingAsMiddleware_AdminWithSession_StashesTeamID(t *testing.T) {
	teamID := uuid.New()
	r := &fakeResolver{ok: true, teamID: teamID, teamSlug: "dhanam"}
	mw := ActingAsMiddleware(r)
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.AddCookie(&http.Cookie{Name: actingAsCookieName, Value: "dhanam"})

	adminID := uuid.New()
	_, out := runWithMiddleware(t, mw, req, func(c *gin.Context) {
		c.Set("user_id", adminID.String())
		c.Set("user_roles", []string{"admin", "developer"})
	})

	assert.True(t, out["is_acting_as"].(bool))
	assert.Equal(t, teamID.String(), out["acting_team_id"])
	assert.Equal(t, "dhanam", out["slug"])
	require.Equal(t, 1, r.calls)
	assert.Equal(t, adminID, r.lastAdmin)
}

func TestActingAsMiddleware_CookiePresentButNoSession_NoFilter(t *testing.T) {
	r := &fakeResolver{ok: false}
	mw := ActingAsMiddleware(r)
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.AddCookie(&http.Cookie{Name: actingAsCookieName, Value: "stale"})

	_, out := runWithMiddleware(t, mw, req, func(c *gin.Context) {
		c.Set("user_id", uuid.New().String())
		c.Set("user_roles", []string{"admin"})
	})
	assert.False(t, out["is_acting_as"].(bool), "stale cookie must not produce a half-state filter")
}

func TestActingAsMiddleware_BadUserID_NoFilter(t *testing.T) {
	r := &fakeResolver{ok: true, teamID: uuid.New(), teamSlug: "x"}
	mw := ActingAsMiddleware(r)
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.AddCookie(&http.Cookie{Name: actingAsCookieName, Value: "x"})

	_, out := runWithMiddleware(t, mw, req, func(c *gin.Context) {
		c.Set("user_id", "not-a-uuid")
		c.Set("user_roles", []string{"admin"})
	})
	assert.False(t, out["is_acting_as"].(bool))
	assert.Equal(t, 0, r.calls)
}

func TestHasAdminRole(t *testing.T) {
	assert.True(t, hasAdminRole([]string{"admin"}))
	assert.True(t, hasAdminRole([]string{"developer", "admin"}))
	assert.False(t, hasAdminRole([]string{"developer"}))
	assert.False(t, hasAdminRole(nil))
	assert.False(t, hasAdminRole([]string{}))
}

func TestNewRepoActingAsResolver_NilInputsReturnNil(t *testing.T) {
	assert.Nil(t, NewRepoActingAsResolver(nil, nil),
		"both nil → nil so wiring code can detect 'feature off'")
}
