package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnforceUserProjectAccess_PlatformRankBypass is the descendant of a test
// named ...AdminBypass, which asserted that the `admin` role STRING — in
// either claim shape — waved a caller through to any project without a single
// lookup. ADR-003 rules that shape a defect: a rank comparison says nothing
// about which tenant the caller is senior in.
//
// What survives is the bypass for the PLATFORM rank, which the auth layer
// resolves from the principal record and no tenant can assert.
func TestEnforceUserProjectAccess_PlatformRankBypass(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("user_id", uuid.New().String())
	c.Set("user_roles", []string{"admin"})
	c.Set("is_platform_admin", true)

	ok := h.enforceUserProjectAccess(c, uuid.New())
	assert.True(t, ok)
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestEnforceUserProjectAccess_TenantAdminRoleIsNotAPlatformBypass pins the
// half of the old test that ADR-003 inverts: the role string alone, in either
// claim shape, must NOT reach a project the caller has no relationship to.
//
// The Handler here has no repositories, so the guard cannot consult
// project_access and answers 500 "authorization unavailable" rather than 404 —
// the assertion that matters is that it does not return true. A wired guard
// answering 404 across every resource kind is covered in
// tenant_scope_guard_test.go.
func TestEnforceUserProjectAccess_TenantAdminRoleIsNotAPlatformBypass(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{}

	for _, tc := range []struct {
		name string
		set  func(*gin.Context)
	}{
		{
			name: "plural roles claim",
			set: func(c *gin.Context) {
				c.Set("user_roles", []string{"admin"})
			},
		},
		{
			name: "singular role claim",
			set: func(c *gin.Context) {
				c.Set("user_role", "admin")
			},
		},
		{
			name: "legacy superadmin string",
			set: func(c *gin.Context) {
				c.Set("user_roles", []string{"superadmin"})
			},
		},
		{
			name: "self-asserted platform_admin string",
			set: func(c *gin.Context) {
				// Reachable today: an API token's `scopes` list is copied
				// verbatim into user_roles by internal/middleware/auth.go.
				c.Set("user_roles", []string{"platform_admin"})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Set("user_id", uuid.New().String())
			tc.set(c)

			ok := h.enforceUserProjectAccess(c, uuid.New())
			assert.False(t, ok, "a role string must never grant cross-tenant reach")
			assert.NotEqual(t, http.StatusOK, w.Code)
		})
	}
}

func TestEnforceUserProjectAccess_Unauthenticated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	ok := h.enforceUserProjectAccess(c, uuid.New())
	assert.False(t, ok)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "UNAUTHORIZED", body.Error.Code)
}
