package auth

import (
	"context"
	"database/sql"
	"errors"
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

func TestNormalizeRequestedScopes(t *testing.T) {
	cases := []struct {
		name      string
		requested []string
		ceiling   Role
		want      []string
		wantErr   error
	}{
		{"empty is allowed", nil, RoleViewer, nil, nil},
		{"blank strings are dropped", []string{" ", ""}, RoleViewer, nil, nil},
		{"viewer may mint viewer", []string{"viewer"}, RoleViewer, []string{"viewer"}, nil},
		{"developer may mint viewer and developer", []string{"Developer ", "viewer", "viewer"}, RoleDeveloper, []string{"developer", "viewer"}, nil},
		{"admin may mint admin", []string{"admin"}, RoleAdmin, []string{"admin"}, nil},
		{"developer may not mint admin", []string{"admin"}, RoleDeveloper, nil, ErrAPITokenScopeAboveRole},
		{"viewer may not mint developer", []string{"developer"}, RoleViewer, nil, ErrAPITokenScopeAboveRole},
		{"free-text label is rejected", []string{"deploy"}, RoleAdmin, nil, ErrUnknownAPITokenScope},
		{"superadmin is not an accepted scope", []string{"superadmin"}, RoleAdmin, nil, ErrUnknownAPITokenScope},
		{"platform_admin is not an accepted scope", []string{"platform_admin"}, RoleAdmin, nil, ErrUnknownAPITokenScope},
		{"unknown ceiling grants nothing", []string{"viewer"}, Role("system"), nil, ErrAPITokenScopeAboveRole},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeRequestedScopes(tc.requested, tc.ceiling)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestEffectiveAPITokenRole(t *testing.T) {
	cases := []struct {
		scopes  []string
		ceiling Role
		want    Role
	}{
		{nil, RoleAdmin, RoleDeveloper},
		{nil, RoleViewer, RoleViewer},
		{[]string{"admin"}, RoleAdmin, RoleAdmin},
		{[]string{"admin"}, RoleDeveloper, RoleDeveloper},
		{[]string{"admin"}, RoleViewer, RoleViewer},
		{[]string{"admin", "read"}, RoleDeveloper, RoleDeveloper},
		{[]string{"superadmin"}, RoleDeveloper, RoleDeveloper},
		{[]string{"viewer"}, RoleAdmin, RoleViewer},
		{[]string{"deploy", "logs"}, RoleAdmin, RoleDeveloper},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, EffectiveAPITokenRole(tc.scopes, tc.ceiling), "scopes=%v ceiling=%s", tc.scopes, tc.ceiling)
	}
}

func TestOwnerRoleCeiling(t *testing.T) {
	assert.Equal(t, RoleAdmin, OwnerRoleCeiling("admin", false))
	assert.Equal(t, RoleAdmin, OwnerRoleCeiling("developer", true))
	assert.Equal(t, RoleDeveloper, OwnerRoleCeiling("developer", false))
	assert.Equal(t, RoleViewer, OwnerRoleCeiling("viewer", false))
	assert.Equal(t, RoleViewer, OwnerRoleCeiling("", false))
	assert.Equal(t, RoleViewer, OwnerRoleCeiling("system", false))
}

// scopedTokenValidator returns a fixed token so the test controls scopes and
// the owner without exercising the token-hash query.
type scopedTokenValidator struct {
	owner  uuid.UUID
	scopes []string
	used   chan struct{}
}

func (v *scopedTokenValidator) ValidateTokenForAuth(context.Context, string) (*db.APITokenInfo, error) {
	return &db.APITokenInfo{ID: uuid.New(), UserID: v.owner, Name: "fixture", Scopes: v.scopes}, nil
}

func (v *scopedTokenValidator) UpdateLastUsed(context.Context, uuid.UUID, string) error {
	v.used <- struct{}{}
	return nil
}

func ownerRow(id uuid.UUID, role string, active bool) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "email", "password_hash", "name", "role", "oidc_subject", "oidc_issuer", "active", "created_at", "updated_at", "last_login_at"}).
		AddRow(id, "owner@example.test", "", "Owner", role, nil, nil, active, time.Now(), time.Now(), nil)
}

// TestAPITokenRoleBoundedByOwner covers fix (b): in both auth managers the
// role a token runs with never exceeds what its owner holds now.
func TestAPITokenRoleBoundedByOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name          string
		scopes        []string
		ownerRole     string
		ownerActive   bool
		ownerErr      error
		platformAdmin *bool // nil: the platform rank must not be queried
		platformErr   error
		wantStatus    int
		wantRole      string
	}{
		{name: "admin scope, admin owner", scopes: []string{"admin"}, ownerRole: "admin", ownerActive: true, wantStatus: http.StatusNoContent, wantRole: "admin"},
		{name: "admin scope, developer owner", scopes: []string{"admin"}, ownerRole: "developer", ownerActive: true, platformAdmin: boolPtr(false), wantStatus: http.StatusNoContent, wantRole: "developer"},
		{name: "admin scope, viewer owner", scopes: []string{"admin"}, ownerRole: "viewer", ownerActive: true, platformAdmin: boolPtr(false), wantStatus: http.StatusNoContent, wantRole: "viewer"},
		{name: "admin scope, developer owner with platform rank", scopes: []string{"admin"}, ownerRole: "developer", ownerActive: true, platformAdmin: boolPtr(true), wantStatus: http.StatusNoContent, wantRole: "admin"},
		{name: "admin scope, platform lookup fails", scopes: []string{"admin"}, ownerRole: "developer", ownerActive: true, platformErr: errors.New("db down"), wantStatus: http.StatusNoContent, wantRole: "developer"},
		{name: "no scopes, admin owner", scopes: nil, ownerRole: "admin", ownerActive: true, wantStatus: http.StatusNoContent, wantRole: "developer"},
		{name: "no scopes, viewer owner", scopes: nil, ownerRole: "viewer", ownerActive: true, wantStatus: http.StatusNoContent, wantRole: "viewer"},
		{name: "viewer scope, admin owner", scopes: []string{"viewer"}, ownerRole: "admin", ownerActive: true, wantStatus: http.StatusNoContent, wantRole: "viewer"},
		{name: "legacy label, developer owner", scopes: []string{"deploy"}, ownerRole: "developer", ownerActive: true, wantStatus: http.StatusNoContent, wantRole: "developer"},
		{name: "owner inactive", scopes: []string{"admin"}, ownerRole: "admin", ownerActive: false, wantStatus: http.StatusUnauthorized},
		{name: "owner missing", scopes: []string{"admin"}, ownerErr: sql.ErrNoRows, wantStatus: http.StatusUnauthorized},
	}
	for _, mode := range []string{"jwt", "oidc"} {
		for _, tc := range cases {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				manager, mock, conn := accountTestManagerDB(t)
				manager.repos.TenantScope = db.NewTenantScopeRepository(conn)
				owner := uuid.New()
				validator := &scopedTokenValidator{owner: owner, scopes: tc.scopes, used: make(chan struct{}, 1)}
				manager.SetAPITokenValidator(validator)

				q := mock.ExpectQuery(`FROM users WHERE id = \$1`).WithArgs(owner)
				if tc.ownerErr != nil {
					q.WillReturnError(tc.ownerErr)
				} else {
					q.WillReturnRows(ownerRow(owner, tc.ownerRole, tc.ownerActive))
				}
				if tc.platformAdmin != nil {
					mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id = \$1`).WithArgs(owner).
						WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(*tc.platformAdmin))
				} else if tc.platformErr != nil {
					mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id = \$1`).WithArgs(owner).
						WillReturnError(tc.platformErr)
				}

				handler := manager.AuthMiddleware()
				if mode == "oidc" {
					handler = (&OIDCManager{jwtManager: manager, repos: manager.repos}).AuthMiddleware()
				}
				router := gin.New()
				var gotRole string
				router.GET("/protected", handler, func(c *gin.Context) {
					gotRole = c.GetString("user_role")
					c.Status(http.StatusNoContent)
				})
				req := httptest.NewRequest(http.MethodGet, "/protected", nil)
				req.Header.Set("Authorization", "Bearer enclii_fixture")
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				require.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())
				if tc.wantStatus == http.StatusNoContent {
					assert.Equal(t, tc.wantRole, gotRole)
					select {
					case <-validator.used:
					case <-time.After(time.Second):
						t.Fatal("successful authentication did not record usage")
					}
				}
			})
		}
	}
}

func boolPtr(b bool) *bool { return &b }
