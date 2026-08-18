package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// TestAddonActorFromContext covers the helper that extracts (userID, userSub,
// userEmail) from gin.Context for attribution in the addon event ledger. This
// is the only auth-glue logic on the addon handler path that doesn't require
// a fully-wired Handler, so it's worth covering directly.
func TestAddonActorFromContext(t *testing.T) {
	t.Run("fully populated context", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		userID := uuid.New()
		c.Set("user_id", userID.String())
		c.Set("userSub", "auth0|123abc")
		c.Set("userEmail", "dev@madfam.io")

		actor := addonActorFromContext(c)
		assert.NotNil(t, actor.UserID)
		assert.Equal(t, userID, *actor.UserID)
		assert.Equal(t, "auth0|123abc", actor.UserSub)
		assert.Equal(t, "dev@madfam.io", actor.UserEmail)
	})

	t.Run("empty context yields zero actor", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		actor := addonActorFromContext(c)
		assert.Nil(t, actor.UserID)
		assert.Equal(t, "", actor.UserSub)
		assert.Equal(t, "", actor.UserEmail)
	})

	t.Run("malformed userID is ignored, not fatal", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		c.Set("user_id", "not-a-uuid")
		c.Set("userSub", "auth0|xyz")

		actor := addonActorFromContext(c)
		assert.Nil(t, actor.UserID)
		// Sub still parses.
		assert.Equal(t, "auth0|xyz", actor.UserSub)
	})

	t.Run("empty userID string", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		c.Set("user_id", "")
		actor := addonActorFromContext(c)
		assert.Nil(t, actor.UserID)
	})

	t.Run("non-string context values are tolerated", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		// Someone stored a non-string by mistake.
		c.Set("userID", 42)
		c.Set("userSub", nil)

		// Should not panic, should not populate the fields.
		actor := addonActorFromContext(c)
		assert.Nil(t, actor.UserID)
		assert.Equal(t, "", actor.UserSub)
	})
}

// TestListManagedDBPlansNoRepos verifies the handler surfaces 503 cleanly if
// the plan repository isn't wired — which is the case in minimal test
// harnesses that don't stand up the full Repositories struct.
func TestListManagedDBPlansNoRepos(t *testing.T) {
	h := &Handler{} // no repos, no services
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/v1/addons/plans", nil)

	h.ListManagedDBPlans(c)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// TestGetAddonEventsBadUUID confirms 400 on a non-UUID path param.
func TestGetAddonEventsBadUUID(t *testing.T) {
	h := &Handler{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: "not-a-uuid"}}
	c.Request, _ = http.NewRequest("GET", "/v1/addons/not-a-uuid/events", nil)

	h.GetAddonEvents(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestGetAddonEventsNoRepos surfaces 503 if the event ledger repo is
// unavailable, even with a valid UUID.
func TestGetAddonEventsNoRepos(t *testing.T) {
	h := &Handler{} // no repos wired
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
	c.Request, _ = http.NewRequest("GET", "/v1/addons/x/events", nil)

	h.GetAddonEvents(c)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// TestIsTruthyQueryParam pins how the destructive `?force=` flag is parsed on
// the delete path (2026-08-17 audit #10). Only explicit truthy values enable
// the force path; anything else (including the empty default) is a safe,
// retention-holding delete.
func TestIsTruthyQueryParam(t *testing.T) {
	truthy := []string{"1", "true", "TRUE", "True", "yes", "YES", "on", " on "}
	for _, v := range truthy {
		assert.Truef(t, isTruthyQueryParam(v), "%q should be truthy", v)
	}
	falsy := []string{"", "0", "false", "no", "off", "maybe", "2", "force"}
	for _, v := range falsy {
		assert.Falsef(t, isTruthyQueryParam(v), "%q should be falsy", v)
	}
}

// TestForceDeleteRequiresPlatformAdmin guards the authorization concern from
// audit #10: a non-platform-admin caller must not be able to trigger the
// immediate, unrecoverable teardown via ?force=true. The handler must reject
// with 403 before reaching the service. A missing/false force flag is the
// normal (retention-holding) path and is not blocked here.
func TestForceDeleteRequiresPlatformAdmin(t *testing.T) {
	// A non-admin principal with force=true. addonService is nil: if the guard
	// did NOT fire first, the handler would panic dereferencing it — so a clean
	// 403 also proves the guard runs before any service call.
	h := &Handler{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("user_role", "developer")
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
	c.Request, _ = http.NewRequest("DELETE", "/v1/addons/x?force=true", nil)

	assert.NotPanics(t, func() { h.DeleteAddon(c) })
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// TestForceDeleteAdminPassesGuard confirms a platform admin clears the force
// guard (the request then proceeds into normal handler flow). We only assert
// the guard itself does not 403 the admin — downstream behavior needs a wired
// service and is covered at the service layer.
func TestForceDeleteAdminPassesGuard(t *testing.T) {
	assert.True(t, callerIsPlatformAdmin(adminCtx(t)), "admin role must be recognized as platform admin")
}

// adminCtx builds a gin context carrying the admin role.
func adminCtx(t *testing.T) *gin.Context {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("user_role", "admin")
	return c
}
