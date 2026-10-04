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
	"github.com/lib/pq"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/stretchr/testify/require"
)

// Keep validation on the real repository; observe the asynchronous usage write
// without allowing a goroutine to outlive its test database.
type observedTokenRepository struct {
	*db.APITokenRepository
	used chan struct{}
}

func (r *observedTokenRepository) UpdateLastUsed(context.Context, uuid.UUID, string) error {
	r.used <- struct{}{}
	return nil
}

func TestAPITokenMiddlewareOwnerState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"jwt", "oidc"} {
		for _, state := range []string{"active", "disabled", "missing", "database error"} {
			t.Run(mode+"/"+state, func(t *testing.T) {
				manager, mock := accountTestManager(t)
				validator := &observedTokenRepository{APITokenRepository: manager.repos.APITokens, used: make(chan struct{}, 1)}
				manager.SetAPITokenValidator(validator)
				middleware := manager.AuthMiddleware()
				if mode == "oidc" {
					middleware = (&OIDCManager{jwtManager: manager, repos: manager.repos}).AuthMiddleware()
				}
				owner, id := uuid.New(), uuid.New()
				q := mock.ExpectQuery(`AND EXISTS \(SELECT 1 FROM users WHERE users.id = api_tokens.user_id AND users.active = true\)`)
				if state == "active" {
					q.WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "name", "prefix", "token_hash", "scopes", "expires_at", "last_used_at", "last_used_ip", "revoked", "revoked_at", "created_at", "updated_at"}).AddRow(id, owner, "fixture", "enclii_", "hash", pq.Array([]string{"admin", "read"}), nil, nil, nil, false, nil, time.Now(), time.Now()))
				} else if state == "database error" {
					q.WillReturnError(errors.New("database unavailable"))
				} else {
					q.WillReturnError(sql.ErrNoRows)
				}
				router := gin.New()
				reached := false
				router.GET("/protected", middleware, func(c *gin.Context) {
					reached = true
					require.Equal(t, owner.String(), c.GetString("user_id"))
					require.Equal(t, "admin", c.GetString("user_role"))
					c.Status(http.StatusNoContent)
				})
				request := httptest.NewRequest(http.MethodGet, "/protected", nil)
				request.Header.Set("Authorization", "Bearer enclii_fixture")
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				if state == "active" {
					require.Equal(t, http.StatusNoContent, response.Code)
					require.True(t, reached)
					select {
					case <-validator.used:
					case <-time.After(time.Second):
						t.Fatal("successful authentication did not record usage")
					}
				} else {
					require.Equal(t, http.StatusUnauthorized, response.Code)
					require.False(t, reached)
					select {
					case <-validator.used:
						t.Fatal("denied credential recorded usage")
					default:
					}
				}
			})
		}
	}
}
