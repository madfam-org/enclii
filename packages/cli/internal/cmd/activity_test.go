package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func TestNewActivityCommand(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	cmd := NewActivityCommand(cfg)
	require.NotNil(t, cmd)
	assert.Equal(t, "activity", cmd.Use)
}

func TestActivityCommand_Subcommands(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	cmd := NewActivityCommand(cfg)

	names := make([]string, 0, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}
	for _, want := range []string{"list", "actions", "resource-types"} {
		assert.Contains(t, names, want)
	}
}

func TestActivityList_Flags(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	listCmd := findSubcommand(NewActivityCommand(cfg), "list")
	require.NotNil(t, listCmd)
	for _, flag := range []string{"action", "resource-type", "limit", "json"} {
		assert.NotNil(t, listCmd.Flags().Lookup(flag), "missing flag: %s", flag)
	}
}

// activityHandlerBody is GET /v1/activity?limit=2 exactly as the Switchyard
// handler writes it: the output of GetActivity in
// TestGetActivity_ResponseShape (apps/switchyard-api/internal/api/
// activity_response_shape_test.go), which pins the same keys server-side.
const activityHandlerBody = `{"activities":[{"id":"0b9f6c1e-3c7a-4d57-9a53-2f1f6f3f0a01","timestamp":"2026-09-24T09:14:00Z","actor_email":"dev@example.com","actor_role":"developer","action":"deploy","resource_type":"service","resource_id":"5d0c2b7e-8e0a-4f7e-b1a4-0a4c1d3e9f10","resource_name":"storefront","ip_address":"203.0.113.7","user_agent":"enclii-cli/test","outcome":"success","context":{}}],"count":1,"limit":2,"offset":0}`

func newActivityTestServer(t *testing.T, body string, seen *url.Values) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/activity", r.URL.Path)
		if seen != nil {
			*seen = r.URL.Query()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchActivity_DecodesHandlerJSON(t *testing.T) {
	var seen url.Values
	srv := newActivityTestServer(t, activityHandlerBody, &seen)
	cfg := &config.Config{APIEndpoint: srv.URL, APIToken: "test-token"}

	resp, err := fetchActivity(context.Background(), cfg, map[string]string{"limit": "2", "action": "deploy"})
	require.NoError(t, err)
	assert.Equal(t, "2", seen.Get("limit"))
	assert.Equal(t, "deploy", seen.Get("action"))

	require.Len(t, resp.Activities, 1)
	row := resp.Activities[0]
	assert.Equal(t, "deploy", row.Action)
	assert.Equal(t, "service", row.ResourceType)
	assert.Equal(t, "storefront", row.ResourceName)
	assert.Equal(t, "dev@example.com", row.ActorEmail)
	assert.Equal(t, "success", row.Outcome)
	assert.Equal(t, 1, resp.Count)
	assert.Equal(t, 2, resp.Limit)
	assert.Equal(t, 0, resp.Offset)

	// --json re-emits the API's own keys.
	out, err := json.Marshal(resp)
	require.NoError(t, err)
	var top map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(out, &top))
	for _, key := range []string{"activities", "count", "limit", "offset"} {
		assert.Contains(t, top, key)
	}
	assert.NotContains(t, top, "events")
}

func TestActivityList_PrintsHandlerRows(t *testing.T) {
	srv := newActivityTestServer(t, activityHandlerBody, nil)
	cfg := &config.Config{APIEndpoint: srv.URL, APIToken: "test-token"}

	root := NewActivityCommand(cfg)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"list", "--limit", "2"})
	require.NoError(t, root.Execute())

	text := out.String()
	assert.Contains(t, text, "TIMESTAMP")
	assert.Contains(t, text, "2026-09-24 09:14")
	assert.Contains(t, text, "storefront")
	assert.Contains(t, text, "dev@example.com")
	assert.Contains(t, text, "success")
	assert.NotContains(t, text, "No activity events")
}

func TestActivityList_ResourceFallsBackToID(t *testing.T) {
	var out bytes.Buffer
	resp := &activityListResponse{}
	require.NoError(t, json.Unmarshal([]byte(`{"activities":[{"id":"0b9f6c1e-3c7a-4d57-9a53-2f1f6f3f0a01","timestamp":"2026-09-24T09:14:00Z","action":"delete","resource_type":"env_var","resource_id":"DATABASE_URL","resource_name":"","actor_email":"","outcome":"success"}],"count":1,"limit":50,"offset":0}`), resp))
	require.NoError(t, renderActivity(&out, resp))
	assert.Contains(t, out.String(), "DATABASE_URL")
}

func TestActivityList_EmptyList(t *testing.T) {
	srv := newActivityTestServer(t, `{"activities":[],"count":0,"limit":50,"offset":0}`, nil)
	cfg := &config.Config{APIEndpoint: srv.URL, APIToken: "test-token"}
	resp, err := fetchActivity(context.Background(), cfg, nil)
	require.NoError(t, err)
	var out bytes.Buffer
	require.NoError(t, renderActivity(&out, resp))
	assert.Contains(t, out.String(), "No activity events match the given filters.")
}
