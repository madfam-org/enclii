package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
)

const testScope = "enclii-cogs:read"

// fakeVerifier maps a bearer to claims; any other bearer is invalid.
type fakeVerifier map[string]*auth.MachineClaims

func (f fakeVerifier) ValidateExternalMachineClaims(token string) (*auth.MachineClaims, error) {
	if c, ok := f[token]; ok {
		return c, nil
	}
	return nil, errors.New("invalid token")
}

func machine(client, org string, scopes ...string) *auth.MachineClaims {
	return &auth.MachineClaims{ClientID: client, TokenUse: auth.TokenUseClientCredentials, TenantID: org, Scope: scopes}
}

var verifier = fakeVerifier{
	"ops":       machine("jnc_ops", "org-ops", testScope),
	"other-org": machine("jnc_x", "org-other", testScope),
	"no-org":    machine("jnc_y", "", testScope),
	"no-scope":  machine("jnc_z", "org-ops", "enclii-cogs", "enclii:read"),
	"person":    {RegisteredClaims: jwt.RegisteredClaims{Subject: "5f0c3c1e-0000-4000-8000-000000000001"}, TenantID: "org-ops", Scope: []string{testScope}},
	"no-client": {TokenUse: auth.TokenUseClientCredentials, TenantID: "org-ops", Scope: []string{testScope}},
}

func echoTool() Tool {
	return Tool{
		Name: "usage_echo", Title: "usage.echo", Description: "echo",
		InputSchema:  map[string]any{"type": "object"},
		OutputSchema: map[string]any{"type": "object"},
		Call: func(_ context.Context, req Request) (any, error) {
			if err := req.OnlyArgs("say", "fail"); err != nil {
				return nil, err
			}
			if v, _, _ := req.String("fail"); v == "internal" {
				return nil, errors.New("database exploded with secret detail")
			}
			say, _, err := req.String("say")
			if err != nil {
				return nil, err
			}
			return map[string]any{"said": say, "client": req.Caller.ClientID, "org": req.Caller.OrgID}, nil
		},
	}
}

func newTestServer(t *testing.T, rate, burst int) (*Server, *gin.Engine, *test.Hook) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	log, hook := test.NewNullLogger()
	s, err := NewServer(Config{Name: "enclii", Title: "Enclii", Version: "1.0.0", Instructions: "read", Scope: testScope,
		OrgIDs: []string{" org-ops "}, Verifier: verifier, RatePerMinute: rate, Burst: burst, Log: log}, echoTool())
	require.NoError(t, err)
	r := gin.New()
	r.POST("/mcp", s.Prelude, s.Handle)
	r.GET("/mcp", MethodNotAllowed)
	return s, r, hook
}

func post(r http.Handler, bearer, body string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out), w.Body.String())
	return out
}

func lastAudit(t *testing.T, hook *test.Hook) *logrus.Entry {
	t.Helper()
	e := hook.LastEntry()
	require.NotNil(t, e)
	require.Equal(t, AuditMessage, e.Message)
	return e
}

func TestGatesInOrder(t *testing.T) {
	_, r, hook := newTestServer(t, 600, 100)
	call := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	for name, tc := range map[string]struct {
		bearer  string
		headers []string
		status  int
		code    string
		outcome string
	}{
		"browser origin": {bearer: "ops", headers: []string{"Origin", "https://evil.example"}, status: 403, code: "origin_not_allowed"},
		"no token":       {status: 401, outcome: "unauthenticated"},
		"bad token":      {bearer: "forged", status: 401, outcome: "invalid_token"},
		"basic scheme":   {headers: []string{"Authorization", "Basic b3BzOg=="}, status: 401, outcome: "unauthenticated"},
		"person":         {bearer: "person", status: 403, code: "machine_token_required", outcome: "denied_not_machine"},
		"no org":         {bearer: "no-org", status: 403, code: "org_not_allowed", outcome: "denied_org"},
		"other org":      {bearer: "other-org", status: 403, code: "org_not_allowed", outcome: "denied_org"},
		"no client id":   {bearer: "no-client", status: 403, code: "org_not_allowed", outcome: "denied_org"},
		"near scopes":    {bearer: "no-scope", status: 403, code: "missing_scope", outcome: "denied_scope"},
		"ok":             {bearer: "ops", status: 200, outcome: "ok"},
	} {
		hook.Reset()
		w := post(r, tc.bearer, call, tc.headers...)
		assert.Equal(t, tc.status, w.Code, name)
		assert.Equal(t, "no-store", w.Header().Get("Cache-Control"), name)
		if tc.code != "" {
			assert.Equal(t, tc.code, decode(t, w)["code"], name)
		}
		if tc.status == 401 {
			assert.Equal(t, Challenge, w.Header().Get("WWW-Authenticate"), name)
		}
		if tc.code == "missing_scope" {
			assert.Contains(t, w.Header().Get("WWW-Authenticate"), `insufficient_scope", scope="enclii-cogs:read"`)
		}
		if tc.outcome != "" {
			assert.Equal(t, tc.outcome, lastAudit(t, hook).Data["outcome"], name)
		} else {
			assert.Nil(t, hook.LastEntry(), "the Origin gate answers before the handler")
		}
	}
}

func TestAuditLineNamesTheMachineNeverThePersonOrTheArguments(t *testing.T) {
	_, r, hook := newTestServer(t, 600, 100)
	w := post(r, "ops", `{"jsonrpc":"2.0","id":"a","method":"tools/call","params":{"name":"usage_echo","arguments":{"say":"secret-namespace"}}}`)
	require.Equal(t, 200, w.Code)
	e := lastAudit(t, hook)
	assert.Equal(t, "jnc_ops", e.Data["mcp_client_id"])
	assert.Equal(t, "org-ops", e.Data["org_id"])
	assert.Equal(t, "tools/call", e.Data["rpc_method"])
	assert.Equal(t, "usage_echo", e.Data["tool"])
	assert.Len(t, e.Data["args_sha256"], 64)
	line, _ := e.String()
	assert.NotContains(t, line, "secret-namespace")
	assert.NotContains(t, line, "Bearer")

	hook.Reset()
	post(r, "person", `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	e = lastAudit(t, hook)
	assert.NotContains(t, e.Data, "mcp_client_id")
	assert.NotContains(t, e.Data, "org_id")

	hook.Reset()
	post(r, "ops", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"usage_echo","arguments":{"fail":"internal"}}}`)
	e = lastAudit(t, hook)
	assert.Equal(t, logrus.ErrorLevel, e.Level)
	assert.Equal(t, "internal_error", e.Data["outcome"])
}

func TestProtocol(t *testing.T) {
	s, r, _ := newTestServer(t, 600, 100)
	require.Len(t, s.Tools(), 1)

	w := post(r, "ops", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	res := decode(t, w)["result"].(map[string]any)
	assert.Equal(t, "2025-06-18", res["protocolVersion"])
	assert.Equal(t, "enclii", res["serverInfo"].(map[string]any)["name"])
	assert.Equal(t, "read", res["instructions"])

	w = post(r, "ops", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`)
	assert.Equal(t, LatestProtocolVersion, decode(t, w)["result"].(map[string]any)["protocolVersion"])

	w = post(r, "ops", `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	tools := decode(t, w)["result"].(map[string]any)["tools"].([]any)
	require.Len(t, tools, 1)
	tool := tools[0].(map[string]any)
	assert.Equal(t, "usage_echo", tool["name"])
	assert.Equal(t, "usage.echo", tool["title"])
	assert.Equal(t, true, tool["annotations"].(map[string]any)["readOnlyHint"])
	assert.NotNil(t, tool["outputSchema"])

	w = post(r, "ops", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"usage_echo","arguments":{"say":"hi"}}}`)
	res = decode(t, w)["result"].(map[string]any)
	assert.Equal(t, false, res["isError"])
	assert.Equal(t, map[string]any{"said": "hi", "client": "jnc_ops", "org": "org-ops"}, res["structuredContent"])
	assert.Contains(t, res["content"].([]any)[0].(map[string]any)["text"], `"said":"hi"`)

	w = post(r, "ops", `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"usage_echo","arguments":{"nope":1}}}`)
	res = decode(t, w)["result"].(map[string]any)
	assert.Equal(t, true, res["isError"])
	assert.Contains(t, res["content"].([]any)[0].(map[string]any)["text"], "unknown argument(s): nope")

	w = post(r, "ops", `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"usage_echo","arguments":{"fail":"internal"}}}`)
	assert.Equal(t, 500, w.Code)
	assert.NotContains(t, w.Body.String(), "secret detail")
	assert.Equal(t, float64(CodeInternalError), decode(t, w)["error"].(map[string]any)["code"])

	for body, code := range map[string]float64{
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"usage.echo"}}`:                 CodeInvalidParams,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{}}`:                                    CodeInvalidParams,
		`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"usage_echo","arguments":[1]}}`: CodeInvalidParams,
		`{"jsonrpc":"2.0","id":9,"method":"resources/list"}`:                                            CodeMethodNotFound,
	} {
		w = post(r, "ops", body)
		assert.Equal(t, 200, w.Code, body)
		assert.Equal(t, code, decode(t, w)["error"].(map[string]any)["code"], body)
	}

	w = post(r, "ops", `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	assert.Equal(t, map[string]any{}, decode(t, w)["result"])
}

func TestMessageShapes(t *testing.T) {
	_, r, _ := newTestServer(t, 600, 100)
	for name, tc := range map[string]struct {
		body    string
		headers []string
		status  int
	}{
		"notification":         {`{"jsonrpc":"2.0","method":"notifications/initialized"}`, nil, 202},
		"client response":      {`{"jsonrpc":"2.0","result":{}}`, nil, 202},
		"empty message":        {`{"jsonrpc":"2.0"}`, nil, 400},
		"batch":                {`[{"jsonrpc":"2.0","id":1,"method":"ping"}]`, nil, 400},
		"not json":             {`{`, nil, 400},
		"wrong version":        {`{"jsonrpc":"1.0","id":1,"method":"ping"}`, nil, 400},
		"null id":              {`{"jsonrpc":"2.0","id":null,"method":"ping"}`, nil, 400},
		"float id":             {`{"jsonrpc":"2.0","id":1.5,"method":"ping"}`, nil, 400},
		"object id":            {`{"jsonrpc":"2.0","id":{},"method":"ping"}`, nil, 400},
		"no method":            {`{"jsonrpc":"2.0","id":1}`, nil, 400},
		"bad protocol version": {`{"jsonrpc":"2.0","id":1,"method":"ping"}`, []string{"MCP-Protocol-Version", "1999-01-01"}, 400},
		"good protocol":        {`{"jsonrpc":"2.0","id":1,"method":"ping"}`, []string{"MCP-Protocol-Version", "2025-03-26"}, 200},
		"text/plain":           {`{"jsonrpc":"2.0","id":1,"method":"ping"}`, []string{"Content-Type", "text/plain"}, 415},
		"too large":            {`{"jsonrpc":"2.0","id":1,"method":"ping","params":{"x":"` + strings.Repeat("x", MaxBodyBytes) + `"}}`, nil, 413},
	} {
		w := post(r, "ops", tc.body, tc.headers...)
		assert.Equal(t, tc.status, w.Code, name)
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/mcp", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	assert.Equal(t, http.MethodPost, w.Header().Get("Allow"))
}

func TestRateLimitPerClient(t *testing.T) {
	s, r, hook := newTestServer(t, 1, 2)
	clock := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	ping := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	assert.Equal(t, 200, post(r, "ops", ping).Code)
	assert.Equal(t, 200, post(r, "ops", ping).Code)
	w := post(r, "ops", ping)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, "60", w.Header().Get("Retry-After"))
	assert.Equal(t, float64(CodeRateLimited), decode(t, w)["error"].(map[string]any)["code"])
	assert.Equal(t, "rate_limited", lastAudit(t, hook).Data["outcome"])

	clock = clock.Add(61 * time.Second)
	assert.Equal(t, 200, post(r, "ops", ping).Code, "the bucket refills")
}

func TestNewServerRefusesBadConfigs(t *testing.T) {
	ok := Config{Scope: testScope, OrgIDs: []string{"org"}, Verifier: verifier}
	for name, tc := range map[string]struct {
		cfg   Config
		tools []Tool
	}{
		"no scope":     {Config{OrgIDs: ok.OrgIDs, Verifier: verifier}, nil},
		"no verifier":  {Config{Scope: testScope, OrgIDs: ok.OrgIDs}, nil},
		"no org":       {Config{Scope: testScope, OrgIDs: []string{" ", ""}, Verifier: verifier}, nil},
		"dotted name":  {ok, []Tool{{Name: "usage.echo", InputSchema: map[string]any{}, Call: echoTool().Call}}},
		"no call":      {ok, []Tool{{Name: "usage_echo", InputSchema: map[string]any{}}}},
		"duplicate":    {ok, []Tool{echoTool(), echoTool()}},
		"long name":    {ok, []Tool{{Name: strings.Repeat("a", 65), InputSchema: map[string]any{}, Call: echoTool().Call}}},
		"no input set": {ok, []Tool{{Name: "usage_echo", Call: echoTool().Call}}},
	} {
		_, err := NewServer(tc.cfg, tc.tools...)
		assert.Error(t, err, name)
	}
	s, err := NewServer(ok)
	require.NoError(t, err)
	assert.Empty(t, s.Tools())
}

func TestArgsDigestIsCanonical(t *testing.T) {
	a := argsDigest(map[string]any{"b": "2", "a": json.Number("1")})
	b := argsDigest(map[string]any{"a": json.Number("1"), "b": "2"})
	assert.Equal(t, a, b)
	assert.NotEqual(t, a, argsDigest(map[string]any{"a": json.Number("1")}))
	var buf bytes.Buffer
	buf.WriteString(a)
	assert.Len(t, buf.String(), 64)
}
