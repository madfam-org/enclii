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
		c.Set("userID", userID.String())
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

		c.Set("userID", "not-a-uuid")
		c.Set("userSub", "auth0|xyz")

		actor := addonActorFromContext(c)
		assert.Nil(t, actor.UserID)
		// Sub still parses.
		assert.Equal(t, "auth0|xyz", actor.UserSub)
	})

	t.Run("empty userID string", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		c.Set("userID", "")
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
