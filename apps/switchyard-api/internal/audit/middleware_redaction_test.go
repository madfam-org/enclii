package audit

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func decodeBody(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(raw), &body))
	return body
}

func TestSanitizeRequestBody_Recursive(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"values map", `{"values":{"K":"v"}}`, `{"values":"[REDACTED]"}`},
		{"key value pair keeps the name", `{"key":"K","value":"v"}`, `{"key":"K","value":"[REDACTED]"}`},
		{"nested object", `{"spec":{"config":{"password":"p","port":5432}}}`, `{"spec":{"config":{"password":"[REDACTED]","port":5432}}}`},
		{"array of pairs", `{"vars":[{"key":"A","value":"1"}]}`, `{"vars":"[REDACTED]"}`},
		{"array under a neutral key", `{"items":[{"key":"A","value":"1"},{"name":"B","secret_ref":"x"}]}`,
			`{"items":[{"key":"A","value":"[REDACTED]"},{"name":"B","secret_ref":"[REDACTED]"}]}`},
		{"kubernetes secret shape", `{"metadata":{"name":"s"},"stringData":{"K":"v"},"data":{"K":"dg=="}}`,
			`{"metadata":{"name":"s"},"stringData":"[REDACTED]","data":"[REDACTED]"}`},
		{"env map", `{"env":{"DATABASE_URL":"postgres://u:p@h/db"},"environment":"production"}`,
			`{"env":"[REDACTED]","environment":"production"}`},
		{"separator and case variants", `{"API-Key":"a","Private_Key":"b","PlainText":"c","client_secret":"d"}`,
			`{"API-Key":"[REDACTED]","Private_Key":"[REDACTED]","PlainText":"[REDACTED]","client_secret":"[REDACTED]"}`},
		{"neutral fields kept", `{"name":"svc","replicas":2,"tags":["a","b"]}`, `{"name":"svc","replicas":2,"tags":["a","b"]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeRequestBody(decodeBody(t, tc.in))
			gotJSON, err := json.Marshal(got)
			require.NoError(t, err)
			assert.JSONEq(t, tc.want, string(gotJSON))
		})
	}
}

func TestSanitizeRequestBody_DepthBounded(t *testing.T) {
	var v interface{} = "leaf"
	for i := 0; i < maxSanitizeDepth+5; i++ {
		v = map[string]interface{}{"n": v}
	}
	out := sanitizeRequestBody(v.(map[string]interface{}))
	raw, err := json.Marshal(out)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "[TRUNCATED]")
	assert.NotContains(t, string(raw), "leaf")
}

func TestSanitizeRequestBody_DoesNotMutateInput(t *testing.T) {
	in := decodeBody(t, `{"spec":{"value":"v"}}`)
	_ = sanitizeRequestBody(in)
	assert.Equal(t, "v", in["spec"].(map[string]interface{})["value"])
}

func TestIsCredentialRoute(t *testing.T) {
	omitted := []string{
		"/v1/services/:id/env-vars",
		"/v1/services/:id/env-vars/bulk",
		"/v1/services/:id/env-vars/:var_id/reveal",
		"/v1/secrets/intake",
		"/v1/admin/provision/secrets",
		"/v1/user/tokens",
		"/v1/user/tokens/:token_id",
		"/v1/auth/login",
		"/v1/auth/register",
		"/v1/auth/logout",
		"/v1/signup",
		"/v1/addons/:id/data-api/token",
		"/v1/lifecycle-webhooks/:sub_id/rotate-secret",
		"/v1/projects/:slug/webhooks",
		"/v1/integrations/github/link",
		"/v1/invitations/:token/accept",
	}
	for _, route := range omitted {
		assert.True(t, isCredentialRoute(route, ""), route)
	}
	kept := []string{
		"/v1/projects/:slug/services",
		"/v1/services/:id/deploy",
		"/v1/services/:id/scale",
		"/v1/projects/:slug/environments",
	}
	for _, route := range kept {
		assert.False(t, isCredentialRoute(route, ""), route)
	}
	// Path parameters never trigger omission by their name, and an unmatched
	// route falls back to the raw path.
	assert.False(t, isCredentialRoute("/v1/services/:token_hint/deploy", ""))
	assert.True(t, isCredentialRoute("", "/v1/secrets/intake"))
}

func TestSanitizeQuery(t *testing.T) {
	got, err := url.ParseQuery(sanitizeQuery("token=abc&limit=5&api_key=k"))
	require.NoError(t, err)
	assert.Equal(t, "[REDACTED]", got.Get("token"))
	assert.Equal(t, "[REDACTED]", got.Get("api_key"))
	assert.Equal(t, "5", got.Get("limit"))
	assert.Equal(t, "", sanitizeQuery(""))
	assert.Equal(t, "[UNPARSEABLE]", sanitizeQuery("%zz"))
}

// TestAuditMiddleware_CredentialRouteStoresNoBody drives the middleware end to
// end: on a credential route the handler still receives the body, and the
// audit row carries no copy of it.
func TestAuditMiddleware_CredentialRouteStoresNoBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		route    string
		path     string
		body     string
		wantBody bool
	}{
		{"/v1/services/:id/env-vars", "/v1/services/abc/env-vars", `{"key":"K","value":"v"}`, false},
		{"/v1/secrets/intake", "/v1/secrets/intake", `{"values":{"K":"v"}}`, false},
		{"/v1/user/tokens", "/v1/user/tokens", `{"name":"ci","scopes":["viewer"]}`, false},
		{"/v1/services/:id/deploy", "/v1/services/abc/deploy", `{"release_id":"r1","config":{"value":"v"}}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.route, func(t *testing.T) {
			logger := &AsyncLogger{logChan: make(chan *types.AuditLog, 1), fallbackChan: make(chan *types.AuditLog, 1)}
			m := &Middleware{asyncLogger: logger}
			router := gin.New()
			var handlerSaw string
			router.POST(tc.route, func(c *gin.Context) {
				c.Set("user_id", uuid.New().String())
				c.Next()
			}, m.AuditMiddleware(), func(c *gin.Context) {
				raw, _ := io.ReadAll(c.Request.Body)
				handlerSaw = string(raw)
				c.Status(http.StatusOK)
			})
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(httptest.NewRecorder(), req)

			assert.Equal(t, tc.body, handlerSaw, "the handler must still receive the body")
			var row *types.AuditLog
			select {
			case row = <-logger.logChan:
			default:
				t.Fatal("no audit row was written")
			}
			raw, err := json.Marshal(row.Context)
			require.NoError(t, err)
			assert.NotContains(t, string(raw), `"v"`, "no secret value may reach the audit row")
			if tc.wantBody {
				assert.Contains(t, row.Context, "request_body")
				assert.NotContains(t, row.Context, "request_body_omitted")
			} else {
				assert.NotContains(t, row.Context, "request_body")
				assert.Equal(t, true, row.Context["request_body_omitted"])
			}
		})
	}
}
