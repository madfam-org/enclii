package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// TestAuthMiddlewareRecordsCredentialSource pins what the WebSocket origin
// check relies on: CredentialFromAuthorizationHeader is true only when the
// token that authenticated the request came from `Authorization: Bearer`.
func TestAuthMiddlewareRecordsCredentialSource(t *testing.T) {
	manager, token := newCredentialSourceTestManager(t)
	assertRecordsCredentialSource(t, manager.AuthMiddleware(), token)
}

// TestOIDCAuthMiddlewareRecordsCredentialSource pins the same contract for the
// OIDC-mode middleware, which production runs (ENCLII_AUTH_MODE=oidc). #625
// recorded the source only in JWTManager.AuthMiddleware, so in production the
// origin check never saw a Bearer-header credential and `enclii logs
// --follow` kept getting 403. The local-token path needs no OIDC provider,
// so a bare OIDCManager around the same JWTManager exercises it.
func TestOIDCAuthMiddlewareRecordsCredentialSource(t *testing.T) {
	manager, token := newCredentialSourceTestManager(t)
	oidcManager := &OIDCManager{jwtManager: manager, adminEmails: map[string]bool{}}
	assertRecordsCredentialSource(t, oidcManager.AuthMiddleware(), token)
}

func newCredentialSourceTestManager(t *testing.T) (*JWTManager, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	manager, err := NewJWTManager(15*time.Minute, time.Hour, nil, nil)
	if err != nil {
		t.Fatalf("NewJWTManager: %v", err)
	}
	pair, err := manager.GenerateTokenPair(&User{ID: uuid.New(), Email: "dev@example.com", Role: "developer"})
	if err != nil {
		t.Fatalf("GenerateTokenPair: %v", err)
	}
	return manager, pair.AccessToken
}

func assertRecordsCredentialSource(t *testing.T, middleware gin.HandlerFunc, token string) {
	t.Helper()
	engine := gin.New()
	engine.Use(middleware)
	var fromHeader bool
	engine.GET("/probe", func(c *gin.Context) {
		fromHeader = CredentialFromAuthorizationHeader(c)
		c.Status(http.StatusNoContent)
	})

	cases := []struct {
		name       string
		header     string
		query      string
		wantStatus int
		wantHeader bool
	}{
		{name: "bearer header", header: "Bearer " + token, wantStatus: http.StatusNoContent, wantHeader: true},
		{name: "query token", query: "?token=" + token, wantStatus: http.StatusNoContent, wantHeader: false},
		{name: "empty bearer falls back to query", header: "Bearer ", query: "?token=" + token, wantStatus: http.StatusNoContent, wantHeader: false},
		{name: "non-bearer scheme falls back to query", header: "Token opaque", query: "?token=" + token, wantStatus: http.StatusNoContent, wantHeader: false},
		{name: "bad bearer is rejected, not retried from query", header: "Bearer not-a-jwt", query: "?token=" + token, wantStatus: http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fromHeader = false
			req := httptest.NewRequest(http.MethodGet, "/probe"+tc.query, nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, req)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", w.Code, tc.wantStatus, w.Body.String())
			}
			if tc.wantStatus == http.StatusNoContent && fromHeader != tc.wantHeader {
				t.Fatalf("CredentialFromAuthorizationHeader = %v, want %v", fromHeader, tc.wantHeader)
			}
		})
	}
}
