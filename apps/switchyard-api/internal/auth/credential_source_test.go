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
	gin.SetMode(gin.TestMode)
	manager, err := NewJWTManager(15*time.Minute, time.Hour, nil, nil)
	if err != nil {
		t.Fatalf("NewJWTManager: %v", err)
	}
	pair, err := manager.GenerateTokenPair(&User{ID: uuid.New(), Email: "dev@example.com", Role: "developer"})
	if err != nil {
		t.Fatalf("GenerateTokenPair: %v", err)
	}
	token := pair.AccessToken

	engine := gin.New()
	engine.Use(manager.AuthMiddleware())
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
