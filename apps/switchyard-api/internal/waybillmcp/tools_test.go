package waybillmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/mcp"
)

const dailyBody = `{"from":"2026-11-01","to":"2026-11-02","timezone":"America/Mexico_City",
 "captures":[{"date":"2026-11-01","cluster":"c1","usage_coverage":1,"requests_coverage":0.99,"workload_rows":1,"node_rows":1,"source":"prometheus","captured_at":"2026-11-02T06:20:00Z","future":"x"}],
 "uncaptured_dates":["2026-11-02"],
 "rows":[{"date":"2026-11-01","cluster":"c1","node":"node-a","namespace":"shop","workload_kind":"Deployment","workload_name":"api","container":"app","pod_count":2,
   "cpu_core_hours_used":1.25,"cpu_core_hours_requested":null,"mem_working_set_max_bytes":123456789012,"sample_count":288,"coverage":1,"source":"prometheus","unit_cost":9.99}],
 "extra_top_level":true}`

type fakeWaybill struct {
	path, query, key string
	status           int
	body             string
}

func (f *fakeWaybill) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.path, f.query, f.key = r.URL.Path, r.URL.RawQuery, r.Header.Get("X-API-Key")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(f.status)
		_, _ = w.Write([]byte(f.body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func callTool(t *testing.T, c *Client, name string, args map[string]any) (any, error) {
	t.Helper()
	for _, tool := range Tools(c) {
		if tool.Name == name {
			return tool.Call(context.Background(), mcp.Request{Caller: mcp.Caller{ClientID: "jnc_ops"}, Args: args})
		}
	}
	t.Fatalf("no tool %s", name)
	return nil, nil
}

func TestResourceDailyCallsTheWaybillRouteAndProjects(t *testing.T) {
	fake := &fakeWaybill{status: http.StatusOK, body: dailyBody}
	srv := fake.server(t)
	out, err := callTool(t, &Client{BaseURL: srv.URL + "/", APIKey: "svc-key"}, ToolResourceDaily,
		map[string]any{"from": "2026-11-01", "to": "2026-11-02", "namespace": "shop"})
	require.NoError(t, err)
	assert.Equal(t, PathResourceDaily, fake.path)
	assert.Equal(t, "from=2026-11-01&namespace=shop&to=2026-11-02", fake.query)
	assert.Equal(t, "svc-key", fake.key, "the billing proxy's service key is forwarded")

	raw, err := json.Marshal(out)
	require.NoError(t, err)
	text := string(raw)
	for _, gone := range []string{"unit_cost", "extra_top_level", "future"} {
		assert.NotContains(t, text, gone, "a field the output schema does not list stays off MCP")
	}
	for _, kept := range []string{`"cpu_core_hours_used":1.25`, `"cpu_core_hours_requested":null`, `"mem_working_set_max_bytes":123456789012`, `"uncaptured_dates":["2026-11-02"]`, `"timezone":"America/Mexico_City"`} {
		assert.Contains(t, text, kept)
	}
}

func TestNodeDailyCallsTheNodeRoute(t *testing.T) {
	fake := &fakeWaybill{status: http.StatusOK, body: `{"from":"2026-11-01","to":"2026-11-01","timezone":"UTC","captures":[],"uncaptured_dates":[],"nodes":null}`}
	srv := fake.server(t)
	out, err := callTool(t, &Client{BaseURL: srv.URL}, ToolNodeDaily, map[string]any{"from": "2026-11-01", "to": "2026-11-01", "cluster": "c1"})
	require.NoError(t, err)
	assert.Equal(t, PathNodeDaily, fake.path)
	assert.Equal(t, "cluster=c1&from=2026-11-01&to=2026-11-01", fake.query)
	assert.Empty(t, fake.key, "no key configured, none sent")
	assert.Contains(t, out.(map[string]any), "nodes")
}

func TestArgumentsTheCallerCanFix(t *testing.T) {
	fake := &fakeWaybill{status: http.StatusOK, body: `{}`}
	c := &Client{BaseURL: fake.server(t).URL}
	for name, tc := range map[string]struct {
		tool string
		args map[string]any
		want string
	}{
		"missing to":          {ToolResourceDaily, map[string]any{"from": "2026-11-01"}, "from and to are required"},
		"not a date":          {ToolResourceDaily, map[string]any{"from": "yesterday", "to": "2026-11-01"}, "from and to are required"},
		"number":              {ToolResourceDaily, map[string]any{"from": json.Number("20261101"), "to": "2026-11-01"}, "from must be a string"},
		"unknown argument":    {ToolResourceDaily, map[string]any{"from": "2026-11-01", "to": "2026-11-01", "cost": "yes"}, "unknown argument(s): cost"},
		"namespace on nodes":  {ToolNodeDaily, map[string]any{"from": "2026-11-01", "to": "2026-11-01", "namespace": "shop"}, "unknown argument(s): namespace"},
		"oversized namespace": {ToolResourceDaily, map[string]any{"from": "2026-11-01", "to": "2026-11-01", "namespace": strings.Repeat("a", 254)}, "longer than 253"},
	} {
		_, err := callTool(t, c, tc.tool, tc.args)
		var argErr *mcp.ArgumentError
		if assert.ErrorAs(t, err, &argErr, name) {
			assert.Contains(t, argErr.Message, tc.want, name)
		}
	}
}

func TestWaybillRefusalsAndFailures(t *testing.T) {
	args := map[string]any{"from": "2026-09-01", "to": "2026-11-30"}
	var argErr *mcp.ArgumentError

	fake := &fakeWaybill{status: http.StatusBadRequest, body: `{"error":"range is longer than 62 days"}`}
	_, err := callTool(t, &Client{BaseURL: fake.server(t).URL}, ToolResourceDaily, args)
	require.ErrorAs(t, err, &argErr, "Waybill's 400 is the caller's to fix")
	assert.Equal(t, "range is longer than 62 days", argErr.Message)

	fake = &fakeWaybill{status: http.StatusBadRequest, body: `not json`}
	_, err = callTool(t, &Client{BaseURL: fake.server(t).URL}, ToolResourceDaily, args)
	require.ErrorAs(t, err, &argErr)
	assert.Equal(t, "Waybill refused the arguments", argErr.Message)

	for _, status := range []int{http.StatusUnauthorized, http.StatusInternalServerError, http.StatusNotFound} {
		fake = &fakeWaybill{status: status, body: `{"error":"x"}`}
		_, err = callTool(t, &Client{BaseURL: fake.server(t).URL}, ToolResourceDaily, args)
		assert.True(t, errors.Is(err, errUpstream), "status %d is a server failure", status)
		assert.False(t, errors.As(err, &argErr))
	}

	fake = &fakeWaybill{status: http.StatusOK, body: `{"rows":`}
	_, err = callTool(t, &Client{BaseURL: fake.server(t).URL}, ToolResourceDaily, args)
	assert.ErrorIs(t, err, errUpstream)

	fake = &fakeWaybill{status: http.StatusOK, body: `"` + strings.Repeat("x", MaxUpstreamBytes) + `"`}
	_, err = callTool(t, &Client{BaseURL: fake.server(t).URL}, ToolResourceDaily, args)
	require.ErrorAs(t, err, &argErr, "an oversized answer asks the caller to narrow")
	assert.Contains(t, argErr.Message, "narrow from and to")

	_, err = callTool(t, &Client{}, ToolResourceDaily, args)
	assert.ErrorIs(t, err, errUpstream, "no base URL")

	srv := (&fakeWaybill{status: http.StatusOK}).server(t)
	srv.Close()
	_, err = callTool(t, &Client{BaseURL: srv.URL}, ToolResourceDaily, args)
	assert.ErrorIs(t, err, errUpstream, "unreachable")
}

func TestProjectLeavesNonMatchingShapesAlone(t *testing.T) {
	schema := resourceDailyOutputSchema()
	assert.Equal(t, "text", project("text", schema))
	assert.Nil(t, project(map[string]any{"rows": nil}, schema).(map[string]any)["rows"])
	assert.Equal(t, 3, project(3, map[string]any{"items": map[string]any{}}))
}
