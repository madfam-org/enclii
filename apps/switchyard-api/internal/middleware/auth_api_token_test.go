package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

type fixedTokenValidator struct {
	token *types.APIToken
}

func (v *fixedTokenValidator) ValidateToken(context.Context, string) (*types.APIToken, error) {
	return v.token, nil
}

func (v *fixedTokenValidator) UpdateLastUsed(context.Context, uuid.UUID, string) error { return nil }

type fixedOwnerResolver struct {
	ceiling auth.Role
	err     error
}

func (r *fixedOwnerResolver) APITokenOwnerCeiling(context.Context, uuid.UUID, auth.Role) (auth.Role, error) {
	return r.ceiling, r.err
}

// TestAuthMiddleware_APITokenRoleBounded covers the generic middleware's
// API-token path: scopes are never copied into user_roles verbatim, and the
// resulting role never exceeds the owner's ceiling.
func TestAuthMiddleware_APITokenRoleBounded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, publicKey := generateTestRSAKeys(t)

	cases := []struct {
		name         string
		scopes       []string
		resolver     APITokenOwnerResolver
		requireAdmin bool
		wantStatus   int
		wantRoles    []string
	}{
		{name: "admin scope without resolver is capped at developer", scopes: []string{"admin"}, wantStatus: http.StatusOK, wantRoles: []string{"developer"}},
		{name: "free-text scope is not a role", scopes: []string{"superadmin", "deploy"}, wantStatus: http.StatusOK, wantRoles: []string{"developer"}},
		{name: "viewer scope narrows", scopes: []string{"viewer"}, wantStatus: http.StatusOK, wantRoles: []string{"viewer"}},
		{name: "admin scope with admin owner", scopes: []string{"admin"}, resolver: &fixedOwnerResolver{ceiling: auth.RoleAdmin}, wantStatus: http.StatusOK, wantRoles: []string{"admin"}},
		{name: "admin scope with viewer owner", scopes: []string{"admin"}, resolver: &fixedOwnerResolver{ceiling: auth.RoleViewer}, wantStatus: http.StatusOK, wantRoles: []string{"viewer"}},
		{name: "owner lookup failure refuses", scopes: []string{"admin"}, resolver: &fixedOwnerResolver{err: errors.New("unavailable")}, wantStatus: http.StatusUnauthorized},
		{name: "role requirement applies to tokens", scopes: []string{"admin"}, requireAdmin: true, wantStatus: http.StatusForbidden},
		{name: "role requirement met by bounded admin", scopes: []string{"admin"}, resolver: &fixedOwnerResolver{ceiling: auth.RoleAdmin}, requireAdmin: true, wantStatus: http.StatusOK, wantRoles: []string{"admin"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			am := NewAuthMiddleware(publicKey)
			am.SetTokenValidator(&fixedTokenValidator{token: &types.APIToken{ID: uuid.New(), UserID: uuid.New(), Name: "fixture", Scopes: tc.scopes}})
			if tc.resolver != nil {
				am.SetOwnerResolver(tc.resolver)
			}
			if tc.requireAdmin {
				am.AddRoleRequirement("/protected", []string{"admin"})
			}
			router := gin.New()
			var gotRoles []string
			router.GET("/protected", am.Middleware(), func(c *gin.Context) {
				gotRoles = c.GetStringSlice("user_roles")
				c.Status(http.StatusOK)
			})
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.Header.Set("Authorization", "Bearer enclii_fixture")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			require.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())
			if tc.wantStatus == http.StatusOK {
				assert.Equal(t, tc.wantRoles, gotRoles)
			}
		})
	}
}
