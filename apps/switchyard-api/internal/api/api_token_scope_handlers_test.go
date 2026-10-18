package api

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
)

// adminEmailAuth is an AuthManager carrying an operator admin-email set, as
// the OIDC and JWT managers do (ENCLII_ADMIN_EMAILS).
type adminEmailAuth struct{ set map[string]bool }

func (a adminEmailAuth) AuthMiddleware() gin.HandlerFunc { return func(c *gin.Context) { c.Next() } }
func (a adminEmailAuth) RequireRole(...string) gin.HandlerFunc {
	return func(c *gin.Context) { c.Next() }
}
func (a adminEmailAuth) IsAdminEmail(email string) bool { return a.set[email] }

// scopesArg matches the scopes column value the repository writes.
type scopesArg struct{ want []string }

func (a scopesArg) Match(v driver.Value) bool {
	expected, err := pq.Array(a.want).Value()
	if err != nil {
		return false
	}
	return reflect.DeepEqual(expected, v)
}

// TestCreateAPIToken_ScopesBoundedByCaller covers fix (a): scopes are checked
// against an allow-list and may not rank above the creating principal's role
// (the lower of the session role and the stored account role).
func TestCreateAPIToken_ScopesBoundedByCaller(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name          string
		sessionRole   string
		storedRole    string
		platformAdmin *bool // nil: the platform rank must not be queried
		scopes        []string
		wantStatus    int
		wantStored    []string
		noOwnerLookup bool
		adminEmails   map[string]bool // nil: no auth manager wired
	}{
		// Production OIDC: the operator is admin by the session-time
		// admin-email override while users.role stays developer.
		{name: "operator in admin-email set may mint admin", sessionRole: "admin", storedRole: "developer", adminEmails: map[string]bool{"owner@example.test": true}, scopes: []string{"admin"}, wantStatus: http.StatusCreated, wantStored: []string{"admin"}},
		{name: "same account not in the set may not mint admin", sessionRole: "admin", storedRole: "developer", adminEmails: map[string]bool{"operator@example.test": true}, platformAdmin: ptrBool(false), scopes: []string{"admin"}, wantStatus: http.StatusForbidden},
		{name: "empty admin-email set may not mint admin", sessionRole: "admin", storedRole: "developer", adminEmails: map[string]bool{}, platformAdmin: ptrBool(false), scopes: []string{"admin"}, wantStatus: http.StatusForbidden},
		{name: "self-signed-up developer stays capped at developer", sessionRole: "developer", storedRole: "developer", adminEmails: map[string]bool{"operator@example.test": true}, platformAdmin: ptrBool(false), scopes: []string{"admin"}, wantStatus: http.StatusForbidden},
		{name: "developer may not mint admin", sessionRole: "developer", storedRole: "developer", platformAdmin: ptrBool(false), scopes: []string{"admin"}, wantStatus: http.StatusForbidden},
		{name: "viewer may not mint developer", sessionRole: "viewer", storedRole: "viewer", platformAdmin: ptrBool(false), scopes: []string{"developer"}, wantStatus: http.StatusForbidden},
		{name: "admin may mint admin", sessionRole: "admin", storedRole: "admin", scopes: []string{"admin"}, wantStatus: http.StatusCreated, wantStored: []string{"admin"}},
		{name: "platform admin with developer role may mint admin", sessionRole: "admin", storedRole: "developer", platformAdmin: ptrBool(true), scopes: []string{"admin"}, wantStatus: http.StatusCreated, wantStored: []string{"admin"}},
		{name: "stale admin session with demoted account may not mint admin", sessionRole: "admin", storedRole: "developer", platformAdmin: ptrBool(false), scopes: []string{"admin"}, wantStatus: http.StatusForbidden},
		{name: "developer-scoped token of an admin may not mint admin", sessionRole: "developer", storedRole: "admin", scopes: []string{"admin"}, wantStatus: http.StatusForbidden},
		{name: "developer may mint viewer, normalized", sessionRole: "developer", storedRole: "developer", scopes: []string{" Viewer", "viewer"}, wantStatus: http.StatusCreated, wantStored: []string{"viewer"}},
		{name: "unknown scope is a 400", sessionRole: "admin", storedRole: "admin", scopes: []string{"deploy"}, wantStatus: http.StatusBadRequest, noOwnerLookup: true},
		{name: "superadmin scope is a 400", sessionRole: "admin", storedRole: "admin", scopes: []string{"superadmin"}, wantStatus: http.StatusBadRequest, noOwnerLookup: true},
		{name: "no scopes needs no lookup", sessionRole: "viewer", storedRole: "viewer", scopes: nil, wantStatus: http.StatusCreated, wantStored: nil, noOwnerLookup: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = database.Close() })
			h := &Handler{
				repos: &db.Repositories{
					Users:       db.NewUserRepository(database),
					TenantScope: db.NewTenantScopeRepository(database),
					APITokens:   db.NewAPITokenRepository(database),
				},
				logger: testLogger(t),
			}
			if tc.adminEmails != nil {
				h.auth = adminEmailAuth{set: tc.adminEmails}
			}
			uid := uuid.New()

			if !tc.noOwnerLookup {
				mock.ExpectQuery(`FROM users WHERE id = \$1`).WithArgs(uid).WillReturnRows(
					sqlmock.NewRows([]string{"id", "email", "password_hash", "name", "role", "oidc_subject", "oidc_issuer", "active", "created_at", "updated_at", "last_login_at"}).
						AddRow(uid, "owner@example.test", "", "Owner", tc.storedRole, nil, nil, true, time.Now(), time.Now(), nil))
				if tc.platformAdmin != nil {
					mock.ExpectQuery(`SELECT is_platform_admin FROM users`).WithArgs(uid).
						WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(*tc.platformAdmin))
				}
			}
			if tc.wantStatus == http.StatusCreated {
				mock.ExpectQuery(`SELECT COUNT\(\*\) FROM api_tokens`).WithArgs(uid).
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
				mock.ExpectQuery(`INSERT INTO api_tokens`).
					WithArgs(sqlmock.AnyArg(), uid, "fixture", sqlmock.AnyArg(), sqlmock.AnyArg(), scopesArg{tc.wantStored}, sqlmock.AnyArg(), sqlmock.AnyArg()).
					WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
			}

			body, _ := json.Marshal(map[string]interface{}{"name": "fixture", "scopes": tc.scopes})
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/user/tokens", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set("user_id", uid.String())
			c.Set("user_role", tc.sessionRole)

			h.CreateAPIToken(c)
			require.Equal(t, tc.wantStatus, w.Code, w.Body.String())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func ptrBool(b bool) *bool { return &b }
