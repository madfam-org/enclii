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

func accountTestManager(t *testing.T) (*JWTManager, sqlmock.Sqlmock) {
	t.Helper()
	manager, mock, _ := accountTestManagerDB(t)
	return manager, mock
}

func accountTestManagerDB(t *testing.T) (*JWTManager, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	conn, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, mock.ExpectationsWereMet()); conn.Close() })
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return &JWTManager{privateKey: key, publicKey: &key.PublicKey, tokenDuration: time.Hour, refreshDuration: 24 * time.Hour,
		repos: &db.Repositories{Users: db.NewUserRepository(conn), APITokens: db.NewAPITokenRepository(conn), ProjectAccess: db.NewProjectAccessRepository(conn)}}, mock, conn
}

func accountRows(id uuid.UUID, active bool) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "email", "password_hash", "name", "role", "oidc_subject", "oidc_issuer", "active", "created_at", "updated_at", "last_login_at"}).AddRow(id, "member@example.test", "", "Member", "developer", "provider-subject", "https://issuer.example.test", active, time.Now(), time.Now(), nil)
}

func TestLocalTokenAccountState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"jwt", "oidc"} {
		t.Run(mode, func(t *testing.T) {
			manager, mock := accountTestManager(t)
			user := &User{ID: uuid.New(), Email: "member@example.test", Role: "admin", ProjectIDs: []string{"project-a"}, Active: true}
			pair, err := manager.GenerateTokenPair(user)
			require.NoError(t, err)
			middleware := manager.AuthMiddleware()
			if mode == "oidc" {
				middleware = (&OIDCManager{jwtManager: manager, repos: manager.repos}).AuthMiddleware()
			}
			router := gin.New()
			reached := 0
			router.GET("/protected", middleware, func(c *gin.Context) {
				reached++
				require.Equal(t, user.ID.String(), c.GetString("user_id"))
				require.Equal(t, "admin", c.GetString("user_role"))
				require.Equal(t, user.ProjectIDs, c.MustGet("project_ids"))
				c.Status(http.StatusNoContent)
			})
			for _, state := range []string{"active", "disabled", "missing", "database error", "repository unavailable"} {
				if state == "repository unavailable" {
					manager.repos = nil
				} else {
					q := mock.ExpectQuery(`FROM users WHERE id = \$1`).WithArgs(user.ID)
					switch state {
					case "active":
						q.WillReturnRows(accountRows(user.ID, true))
					case "disabled":
						q.WillReturnRows(accountRows(user.ID, false))
					case "missing":
						q.WillReturnError(sql.ErrNoRows)
					default:
						q.WillReturnError(errors.New("database unavailable"))
					}
				}
				request := httptest.NewRequest(http.MethodGet, "/protected", nil)
				request.Header.Set("Authorization", "Bearer "+pair.AccessToken)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				want := http.StatusUnauthorized
				if state == "active" {
					want = http.StatusNoContent
				}
				require.Equal(t, want, response.Code, state)
			}
			require.Equal(t, 1, reached)
		})
	}
}

type decisionRevoker struct {
	revoked   bool
	err       error
	rotations int
}

func (s *decisionRevoker) IsSessionRevoked(context.Context, string) (bool, error) {
	return s.revoked, s.err
}
func (s *decisionRevoker) RevokeSession(context.Context, string, time.Duration) error {
	s.rotations++
	return nil
}

func TestSessionRevocationPreservesCacheDecision(t *testing.T) {
	manager, _ := accountTestManager(t)
	pair, err := manager.GenerateTokenPair(&User{ID: uuid.New(), Role: "developer"})
	require.NoError(t, err)
	for _, tc := range []struct {
		name    string
		revoked bool
		err     error
	}{
		{"allowed", false, nil}, {"revoked", true, nil}, {"fail closed", true, errors.New("cache unavailable")}, {"explicit fail open", false, errors.New("cache unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager.cache = &decisionRevoker{revoked: tc.revoked, err: tc.err}
			claims, err := manager.ValidateToken(pair.AccessToken)
			if tc.revoked {
				require.Error(t, err)
				require.Nil(t, claims)
			} else {
				require.NoError(t, err)
				require.NotNil(t, claims)
			}
		})
	}
}

func TestRefreshRequiresActiveLocalAccount(t *testing.T) {
	for _, state := range []string{"active", "disabled", "missing", "database error", "repository unavailable"} {
		t.Run(state, func(t *testing.T) {
			manager, mock := accountTestManager(t)
			revoker := &decisionRevoker{}
			manager.cache = revoker
			user := &User{ID: uuid.New(), Email: "member@example.test", Role: "admin", ProjectIDs: []string{"project-a"}}
			pair, err := manager.GenerateTokenPair(user)
			require.NoError(t, err)
			if state == "repository unavailable" {
				manager.repos = nil
			} else {
				q := mock.ExpectQuery(`FROM users WHERE id = \$1`).WithArgs(user.ID)
				switch state {
				case "active":
					q.WillReturnRows(accountRows(user.ID, true))
				case "disabled":
					q.WillReturnRows(accountRows(user.ID, false))
				case "missing":
					q.WillReturnError(sql.ErrNoRows)
				default:
					q.WillReturnError(errors.New("database unavailable"))
				}
			}
			// Supply signed refresh scope explicitly: standard issuance currently omits
			// it, and this repair must preserve whatever scope the token carries.
			refreshClaims, err := manager.validateRefreshToken(pair.RefreshToken)
			require.NoError(t, err)
			refreshClaims.ProjectIDs = user.ProjectIDs
			pair.RefreshToken, err = jwt.NewWithClaims(jwt.SigningMethodRS256, refreshClaims).SignedString(manager.privateKey)
			require.NoError(t, err)
			refreshed, err := manager.RefreshToken(pair.RefreshToken)
			if state != "active" {
				require.Error(t, err)
				require.Nil(t, refreshed)
				require.Zero(t, revoker.rotations)
				return
			}
			require.NoError(t, err)
			require.Equal(t, 1, revoker.rotations)
			claims, err := manager.ValidateToken(refreshed.AccessToken)
			require.NoError(t, err)
			require.Equal(t, user.ID, claims.UserID)
			require.Equal(t, user.Role, claims.Role)
			require.Equal(t, user.ProjectIDs, claims.ProjectIDs)
		})
	}
}
