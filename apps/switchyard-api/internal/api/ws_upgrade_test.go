package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
)

const testAllowedWSOrigin = "https://app.enclii.test"

func TestWebsocketOriginAllowed(t *testing.T) {
	allowed := []string{testAllowedWSOrigin}
	cases := []struct {
		name       string
		origin     string
		allowList  []string
		headerAuth bool
		want       bool
	}{
		{name: "no origin, bearer header (CLI)", headerAuth: true, allowList: allowed, want: true},
		{name: "no origin, bearer header, empty allow-list", headerAuth: true, want: true},
		{name: "no origin, query token", allowList: allowed, want: false},
		{name: "allowed origin, query token (browser)", origin: testAllowedWSOrigin, allowList: allowed, want: true},
		{name: "allowed origin, bearer header", origin: testAllowedWSOrigin, allowList: allowed, headerAuth: true, want: true},
		{name: "foreign origin, query token (cross-site hijack)", origin: "https://evil.example", allowList: allowed, want: false},
		{name: "foreign origin, bearer header", origin: "https://evil.example", allowList: allowed, headerAuth: true, want: false},
		{name: "any origin, empty allow-list", origin: testAllowedWSOrigin, want: false},
		{name: "origin null (sandboxed frame)", origin: "null", allowList: allowed, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, websocketOriginAllowed(tc.origin, tc.allowList, tc.headerAuth))
		})
	}
}

// TestLogStreamUpgrade_OriginPolicyBehindAuthMiddleware drives real upgrades
// through the real AuthMiddleware and getWebSocketUpgrader, the way the CLI
// (Bearer header, no Origin), nodeLogsTail (Bearer header, optional Origin)
// and a browser (query token, page Origin) connect.
func TestLogStreamUpgrade_OriginPolicyBehindAuthMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager, err := auth.NewJWTManager(15*time.Minute, time.Hour, nil, nil)
	require.NoError(t, err)
	pair, err := manager.GenerateTokenPair(&auth.User{ID: uuid.New(), Email: "dev@example.com", Role: "developer"})
	require.NoError(t, err)
	token := pair.AccessToken

	h := &Handler{config: &config.Config{WebSocketAllowedOrigins: []string{testAllowedWSOrigin}}}
	engine := gin.New()
	engine.Use(manager.AuthMiddleware())
	engine.GET("/v1/ws-probe", func(c *gin.Context) {
		conn, err := h.getWebSocketUpgrader(c).Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return // the upgrader already answered 403
		}
		defer func() { _ = conn.Close() }()
		_ = conn.WriteMessage(websocket.TextMessage, []byte("ok"))
	})
	srv := httptest.NewServer(engine)
	defer srv.Close()
	wsBase := "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/ws-probe"

	cases := []struct {
		name       string
		bearer     bool
		queryToken bool
		origin     string
		wantStatus int
	}{
		{name: "CLI: bearer header, no origin", bearer: true, wantStatus: http.StatusSwitchingProtocols},
		{name: "nodeLogsTail with origin", bearer: true, origin: testAllowedWSOrigin, wantStatus: http.StatusSwitchingProtocols},
		{name: "browser logs.tail: query token, allowed origin", queryToken: true, origin: testAllowedWSOrigin, wantStatus: http.StatusSwitchingProtocols},
		{name: "query token without origin", queryToken: true, wantStatus: http.StatusForbidden},
		{name: "cross-site page: query token, foreign origin", queryToken: true, origin: "https://evil.example", wantStatus: http.StatusForbidden},
		{name: "bearer header with foreign origin", bearer: true, origin: "https://evil.example", wantStatus: http.StatusForbidden},
		{name: "no credential at all", wantStatus: http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			url := wsBase
			if tc.queryToken {
				url += "?token=" + token
			}
			header := http.Header{}
			if tc.bearer {
				header.Set("Authorization", "Bearer "+token)
			}
			if tc.origin != "" {
				header.Set("Origin", tc.origin)
			}
			conn, resp, err := websocket.DefaultDialer.Dial(url, header)
			if resp != nil {
				defer func() { _ = resp.Body.Close() }()
			}
			require.NotNil(t, resp, "dial error: %v", err)
			assert.Equal(t, tc.wantStatus, resp.StatusCode)
			if tc.wantStatus != http.StatusSwitchingProtocols {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			defer func() { _ = conn.Close() }()
			_, msg, err := conn.ReadMessage()
			require.NoError(t, err)
			assert.Equal(t, "ok", string(msg))
		})
	}
}
