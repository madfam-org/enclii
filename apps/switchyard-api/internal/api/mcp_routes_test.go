package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/waybillmcp"
)

// mcpAuthFake is an AuthManager that also verifies Janua machine tokens:
// "ops" is an org-bound client holding enclii-cogs:read.
type mcpAuthFake struct{ audience string }

func (mcpAuthFake) AuthMiddleware() gin.HandlerFunc       { return func(c *gin.Context) { c.Next() } }
func (mcpAuthFake) RequireRole(...string) gin.HandlerFunc { return func(c *gin.Context) { c.Next() } }
func (f mcpAuthFake) ExternalAudience() string            { return f.audience }
func (mcpAuthFake) ValidateExternalMachineClaims(token string) (*auth.MachineClaims, error) {
	if token != "ops" {
		return nil, errors.New("invalid")
	}
	return &auth.MachineClaims{ClientID: "jnc_ops", TokenUse: auth.TokenUseClientCredentials, TenantID: "org-ops",
		Scope: auth.ScopeClaim{MCPScope}}, nil
}

// plainAuth cannot verify machine tokens.
type plainAuth struct{}

func (plainAuth) AuthMiddleware() gin.HandlerFunc       { return func(c *gin.Context) { c.Next() } }
func (plainAuth) RequireRole(...string) gin.HandlerFunc { return func(c *gin.Context) { c.Next() } }

func mcpRouter(h *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	registerMCPRoutes(r, h)
	return r
}

func mcpPost(r http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, MCPPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer ops")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestMCPRouteIsAbsentUnlessEverythingIsInPlace(t *testing.T) {
	on := config.MCPConfig{Enabled: true, OrgIDs: []string{"org-ops"}, RatePerMinute: 60, Burst: 10}
	proxy := &BillingProxyConfig{WaybillBaseURL: "http://waybill"}
	for name, h := range map[string]*Handler{
		"no config":           {},
		"switch off":          {config: &config.Config{MCP: config.MCPConfig{OrgIDs: on.OrgIDs}}, auth: mcpAuthFake{"enclii-api"}, billingProxy: proxy},
		"no org":              {config: &config.Config{MCP: config.MCPConfig{Enabled: true}}, auth: mcpAuthFake{"enclii-api"}, billingProxy: proxy},
		"no machine verifier": {config: &config.Config{MCP: on}, auth: plainAuth{}, billingProxy: proxy},
		"no external JWKS":    {config: &config.Config{MCP: on}, auth: mcpAuthFake{}, billingProxy: proxy},
		"no Waybill":          {config: &config.Config{MCP: on}, auth: mcpAuthFake{"enclii-api"}},
	} {
		w := mcpPost(mcpRouter(h), `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
		assert.Equal(t, http.StatusNotFound, w.Code, name)
	}
}

func TestMCPRouteServesTheWaybillTools(t *testing.T) {
	var gotPath, gotKey string
	waybill := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotKey = r.URL.Path+"?"+r.URL.RawQuery, r.Header.Get("X-API-Key")
		_, _ = w.Write([]byte(`{"from":"2026-11-01","to":"2026-11-01","timezone":"America/Mexico_City","captures":[],"uncaptured_dates":["2026-11-01"],"nodes":[]}`))
	}))
	t.Cleanup(waybill.Close)
	h := &Handler{
		config:       &config.Config{MCP: config.MCPConfig{Enabled: true, OrgIDs: []string{"org-ops"}, RatePerMinute: 60, Burst: 10}},
		auth:         mcpAuthFake{"enclii-api"},
		billingProxy: &BillingProxyConfig{WaybillBaseURL: waybill.URL + "/", InternalAPIKey: "svc-key"},
	}
	r := mcpRouter(h)

	w := mcpPost(r, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var list struct {
		Result struct {
			Tools []struct {
				Name, Title string
			}
		}
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	var names []string
	for _, tool := range list.Result.Tools {
		names = append(names, tool.Name)
	}
	assert.Equal(t, []string{waybillmcp.ToolResourceDaily, waybillmcp.ToolNodeDaily}, names)

	w = mcpPost(r, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"usage_node_daily","arguments":{"from":"2026-11-01","to":"2026-11-01"}}}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"isError":false`)
	assert.Equal(t, waybillmcp.PathNodeDaily+"?from=2026-11-01&to=2026-11-01", gotPath)
	assert.Equal(t, "svc-key", gotKey)

	req := httptest.NewRequest(http.MethodGet, MCPPath, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)

	req = httptest.NewRequest(http.MethodPost, MCPPath, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
