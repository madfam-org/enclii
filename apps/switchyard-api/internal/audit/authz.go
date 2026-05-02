package audit

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/middleware"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ginAuthzChecker is the production implementation of AuthzChecker.
// It reads the context values set by the switchyard-api JWT/OIDC auth
// middleware. We avoid importing the auth package directly to keep this
// sub-package dependency-light and easier to test.
type ginAuthzChecker struct{}

// NewGinAuthz returns an AuthzChecker that reads role/user_id from the
// Gin context keys populated by switchyard-api's auth middleware.
func NewGinAuthz() AuthzChecker {
	return &ginAuthzChecker{}
}

// IsAdmin returns true iff the caller carries the admin role.
//
// The switchyard-api auth middleware writes the caller's role as a string
// into “c.Set("role", ...)“ and the corresponding types.Role constant
// is “types.RoleAdmin“ (= "admin"). We compare case-sensitively —
// roles are machine-written, not user input.
func (g *ginAuthzChecker) IsAdmin(c *gin.Context) bool {
	v, ok := c.Get("role")
	if !ok {
		return false
	}
	switch r := v.(type) {
	case string:
		return r == string(types.RoleAdmin)
	case types.Role:
		return r == types.RoleAdmin
	}
	return false
}

// GinActingTeamReader is the production ActingTeamReader. It defers to
// middleware.ActingTeamID, which reads the value the ActingAsMiddleware
// stashed in the gin context after validating the active acting-as session.
//
// Kept as a zero-value struct (rather than a function) so callers wire it
// uniformly with NewGinAuthz.
type GinActingTeamReader struct{}

// ActingTeamID resolves the team a master admin is currently acting on
// behalf of, or (uuid.Nil, false) if no session is active. Non-admin
// callers always observe (uuid.Nil, false) — the middleware only sets the
// context key after a defense-in-depth admin-role check.
func (GinActingTeamReader) ActingTeamID(c *gin.Context) (uuid.UUID, bool) {
	return middleware.ActingTeamID(c)
}

// ActorSub returns the caller's stable identifier for self-RBAC forcing.
//
// In local-auth mode this is the local user UUID (string). In OIDC mode
// this is the Janua sub. Either way, it's exactly what we need to narrow
// a non-admin caller to their own audit rows.
func (g *ginAuthzChecker) ActorSub(c *gin.Context) string {
	v, ok := c.Get("user_id")
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	// user_id can also be a uuid.UUID; convert via String() if so.
	type stringer interface{ String() string }
	if s, ok := v.(stringer); ok {
		return s.String()
	}
	return ""
}
