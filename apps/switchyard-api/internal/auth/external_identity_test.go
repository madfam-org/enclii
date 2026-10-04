package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/stretchr/testify/require"
)

const fixtureIssuer = "https://identity.example.org"
const identityQuery = `FROM users WHERE oidc_issuer = \$1 AND oidc_subject = \$2`

var externalUserColumns = []string{"id", "email", "password_hash", "name", "role", "oidc_subject", "oidc_issuer", "active", "created_at", "updated_at", "last_login_at"}

func linkedExternalRow(id uuid.UUID, subject string, active bool) *sqlmock.Rows {
	return sqlmock.NewRows(externalUserColumns).AddRow(id, "operator@example.org", "", "Fixture", "developer", subject, fixtureIssuer, active, time.Now(), time.Now(), nil)
}

func externalIdentityFixture(t *testing.T, mode string) (*JWTManager, AuthManager, sqlmock.Sqlmock) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	repos := &db.Repositories{Users: db.NewUserRepository(database), ProjectAccess: db.NewProjectAccessRepository(database), TenantScope: db.NewTenantScopeRepository(database)}
	manager, err := NewJWTManager(15*time.Minute, time.Hour, repos, nil)
	require.NoError(t, err)
	manager.adminEmails = map[string]bool{"operator@example.org": true}
	manager.externalJWKSURL = fixtureIssuer + "/jwks"
	manager.externalIssuer = fixtureIssuer
	manager.externalAudience = "enclii-api"
	manager.externalJWKSCache = &jwksCache{keys: map[string]*rsa.PublicKey{}, expiresAt: time.Now().Add(time.Hour)}
	if mode == "oidc" {
		return manager, &OIDCManager{jwtManager: manager, repos: repos, adminEmails: manager.adminEmails}, mock
	}
	return manager, manager, mock
}

func signedExternalIdentity(t *testing.T, manager *JWTManager, subject string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	manager.externalJWKSCache.keys["fixture"] = &key.PublicKey
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": fixtureIssuer, "sub": subject, "aud": "enclii-api", "exp": time.Now().Add(time.Hour).Unix(),
		"email": "operator@example.org", "role": "superadmin", "is_platform_admin": true,
	})
	token.Header["kid"] = "fixture"
	raw, err := token.SignedString(key)
	require.NoError(t, err)
	return raw
}

func identityRequest(manager AuthManager, token string, endpoint gin.HandlerFunc) *httptest.ResponseRecorder {
	engine := gin.New()
	engine.GET("/probe", manager.AuthMiddleware(), endpoint)
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	out := httptest.NewRecorder()
	engine.ServeHTTP(out, req)
	return out
}

func expectExternalProjects(mock sqlmock.Sqlmock, id any) {
	mock.ExpectQuery(`FROM project_access`).WithArgs(id).WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "project_id", "environment_id", "role", "granted_by", "granted_at", "expires_at"}))
}

func TestExternalIdentityUsesLinkedLocalIDBeforeDatabaseRank(t *testing.T) {
	for _, mode := range []string{"jwt", "oidc"} {
		for _, rank := range []bool{false, true} {
			t.Run(mode+"/rank="+map[bool]string{false: "false", true: "true"}[rank], func(t *testing.T) {
				manager, middleware, mock := externalIdentityFixture(t, mode)
				subject, localID := uuid.New().String(), uuid.New()
				require.NotEqual(t, subject, localID.String())
				token := signedExternalIdentity(t, manager, subject)
				mock.ExpectQuery(identityQuery).WithArgs(fixtureIssuer, subject).WillReturnRows(linkedExternalRow(localID, subject, true))
				if mode == "oidc" {
					expectExternalProjects(mock, localID)
				}
				mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id`).WithArgs(localID).WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(rank))
				response := identityRequest(middleware, token, func(c *gin.Context) {
					require.Equal(t, localID.String(), c.GetString("user_id"))
					require.Equal(t, "admin", c.GetString("user_role"))
					_, granted := c.Get("is_platform_admin")
					require.False(t, granted, "neither token role/rank claims nor the email role mapping may grant platform rank")
					id, err := uuid.Parse(c.GetString("user_id"))
					require.NoError(t, err)
					actual, err := manager.repos.TenantScope.IsPlatformAdmin(c.Request.Context(), id)
					require.NoError(t, err)
					if !actual {
						c.Status(http.StatusForbidden)
						return
					}
					c.Status(http.StatusNoContent)
				})
				want := http.StatusForbidden
				if rank {
					want = http.StatusNoContent
				}
				require.Equal(t, want, response.Code, response.Body.String())
				require.NoError(t, mock.ExpectationsWereMet())
			})
		}
	}
}

func TestExternalIdentityRejectsInactiveAndLookupFailures(t *testing.T) {
	for _, mode := range []string{"jwt", "oidc"} {
		for _, scenario := range []string{"inactive", "database-error", "missing-subject", "missing-repository"} {
			t.Run(mode+"/"+scenario, func(t *testing.T) {
				manager, middleware, mock := externalIdentityFixture(t, mode)
				subject := uuid.New().String()
				if scenario == "missing-subject" {
					subject = ""
				}
				token := signedExternalIdentity(t, manager, subject)
				if scenario == "missing-repository" {
					manager.repos = nil
					if oidc, ok := middleware.(*OIDCManager); ok {
						oidc.repos = nil
					}
				} else if scenario == "inactive" {
					mock.ExpectQuery(identityQuery).WithArgs(fixtureIssuer, subject).WillReturnRows(linkedExternalRow(uuid.New(), subject, false))
				} else if scenario == "database-error" {
					mock.ExpectQuery(identityQuery).WithArgs(fixtureIssuer, subject).WillReturnError(errors.New("lookup unavailable"))
				}
				reached := false
				response := identityRequest(middleware, token, func(c *gin.Context) { reached = true; c.Status(http.StatusNoContent) })
				require.Equal(t, http.StatusUnauthorized, response.Code, response.Body.String())
				require.False(t, reached)
				require.NoError(t, mock.ExpectationsWereMet())
			})
		}
	}
}

func TestOIDCExternalFirstUseStillProvisionsDeveloper(t *testing.T) {
	manager, middleware, mock := externalIdentityFixture(t, "oidc")
	manager.adminEmails = map[string]bool{}
	middleware.(*OIDCManager).adminEmails = manager.adminEmails
	subject := "new-provider-subject"
	token := signedExternalIdentity(t, manager, subject)
	mock.ExpectQuery(identityQuery).WithArgs(fixtureIssuer, subject).WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`FROM users WHERE email`).WithArgs("operator@example.org").WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(`INSERT INTO users`).WithArgs(sqlmock.AnyArg(), "operator@example.org", "", "", "developer", subject, fixtureIssuer, true, sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	expectExternalProjects(mock, sqlmock.AnyArg())
	response := identityRequest(middleware, token, func(c *gin.Context) {
		_, err := uuid.Parse(c.GetString("user_id"))
		require.NoError(t, err)
		require.NotEqual(t, subject, c.GetString("user_id"))
		require.Equal(t, "developer", c.GetString("user_role"))
		_, rank := c.Get("is_platform_admin")
		require.False(t, rank)
		c.Status(http.StatusNoContent)
	})
	require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestOIDCExternalDoesNotLinkInactiveEmailOrProvisionOnLookupFailure(t *testing.T) {
	for _, scenario := range []string{"inactive-email", "email-lookup-error"} {
		t.Run(scenario, func(t *testing.T) {
			_, manager, mock := externalIdentityFixture(t, "oidc")
			subject := "unlinked-provider-subject"
			mock.ExpectQuery(identityQuery).WithArgs(fixtureIssuer, subject).WillReturnError(sql.ErrNoRows)
			emailLookup := mock.ExpectQuery(`FROM users WHERE email`).WithArgs("operator@example.org")
			if scenario == "inactive-email" {
				emailLookup.WillReturnRows(linkedExternalRow(uuid.New(), "previous-subject", false))
			} else {
				emailLookup.WillReturnError(errors.New("lookup unavailable"))
			}
			user, created, err := manager.(*OIDCManager).getOrCreateUserFromExternalTokenWithStatus(context.Background(), &ExternalClaims{Email: "operator@example.org", RegisteredClaims: jwt.RegisteredClaims{Subject: subject, Issuer: fixtureIssuer}})
			require.Nil(t, user)
			require.False(t, created)
			require.Error(t, err)
			if scenario == "inactive-email" {
				require.EqualError(t, err, "external identity is not available")
			} else {
				require.Contains(t, err.Error(), "external account lookup failed")
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestJWTExternalUnknownIdentityFailsClosed(t *testing.T) {
	manager, middleware, mock := externalIdentityFixture(t, "jwt")
	subject := uuid.New().String()
	token := signedExternalIdentity(t, manager, subject)
	mock.ExpectQuery(identityQuery).WithArgs(fixtureIssuer, subject).WillReturnError(sql.ErrNoRows)
	response := identityRequest(middleware, token, func(c *gin.Context) { t.Fatal("unknown issuer/subject must not authorize using token email or role") })
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

type identityAPITokenValidator struct {
	userID uuid.UUID
	used   chan struct{}
}

func (v *identityAPITokenValidator) ValidateTokenForAuth(context.Context, string) (*db.APITokenInfo, error) {
	return &db.APITokenInfo{ID: uuid.New(), UserID: v.userID, Name: "fixture", Scopes: []string{"admin"}}, nil
}
func (v *identityAPITokenValidator) UpdateLastUsed(context.Context, uuid.UUID, string) error {
	close(v.used)
	return nil
}
func TestExternalIdentityChangesPreserveLocalAndAPIKeyIdentity(t *testing.T) {
	for _, mode := range []string{"jwt", "oidc"} {
		for _, credential := range []string{"local", "api-key"} {
			t.Run(mode+"/"+credential, func(t *testing.T) {
				manager, middleware, mock := externalIdentityFixture(t, mode)
				localID := uuid.New()
				token := "enclii_fixture"
				validator := &identityAPITokenValidator{userID: localID, used: make(chan struct{})}
				manager.apiTokenValidator = validator
				if credential == "local" {
					pair, err := manager.GenerateTokenPair(&User{ID: localID, Email: "local@example.org", Role: "admin", Active: true})
					require.NoError(t, err)
					token = pair.AccessToken
				}
				response := identityRequest(middleware, token, func(c *gin.Context) {
					require.Equal(t, localID.String(), c.GetString("user_id"))
					require.Equal(t, "admin", c.GetString("user_role"))
					if credential == "api-key" {
						require.Equal(t, "api_token", c.GetString("auth_type"))
					}
					c.Status(http.StatusNoContent)
				})
				require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
				if credential == "api-key" {
					select {
					case <-validator.used:
					case <-time.After(time.Second):
						t.Fatal("API-key last-use update did not complete")
					}
				}
				require.NoError(t, mock.ExpectationsWereMet())
			})
		}
	}
}
