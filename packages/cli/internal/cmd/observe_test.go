package cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func TestNewObserveCommand(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	cmd := NewObserveCommand(cfg)
	require.NotNil(t, cmd)
	assert.Equal(t, "observe", cmd.Use)
	assert.Contains(t, cmd.Aliases, "metrics")
}

func TestObserveCommand_Subcommands(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	cmd := NewObserveCommand(cfg)

	names := make([]string, 0, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}
	for _, want := range []string{"metrics", "history", "health", "errors", "alerts"} {
		assert.Contains(t, names, want)
	}
}

func TestObserveHistory_DefaultWindow(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	historyCmd := findSubcommand(NewObserveCommand(cfg), "history")
	require.NotNil(t, historyCmd)
	assert.Equal(t, "1h", historyCmd.Flags().Lookup("window").DefValue)
}

func TestObserveMetrics_RequiresService(t *testing.T) {
	assert.Error(t, requireServiceFlag(""))
	assert.NoError(t, requireServiceFlag("svc_abc"))
}

// metricsHistoryHandlerBody is GET /v1/observability/metrics/history?range=24h
// as the Switchyard handler writes it (monitoring.MetricsHistory and
// monitoring.MetricsDataPoint JSON tags): the second minute had no requests,
// so its latency and error rate are null.
const metricsHistoryHandlerBody = `{"time_range":"24h","resolution":"15m","data_points":[` +
	`{"timestamp":"2026-11-04T09:00:00Z","requests_per_sec":12.5,"average_latency_ms":41.25,"error_rate":0.02,"cpu_usage":0.125,"memory_usage":182.4},` +
	`{"timestamp":"2026-11-04T09:01:00Z","requests_per_sec":0,"average_latency_ms":null,"error_rate":null,"cpu_usage":0.01,"memory_usage":181}` +
	`],"scope":"switchyard-api replica switchyard-api-abc","retained_since":"2026-11-04T08:00:00Z"}`

func TestFetchMetricsHistory_DecodesHandlerJSON(t *testing.T) {
	var seen url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/observability/metrics/history", r.URL.Path)
		seen = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(metricsHistoryHandlerBody))
	}))
	t.Cleanup(srv.Close)
	cfg := &config.Config{APIEndpoint: srv.URL, APIToken: "test-token"}

	resp, err := fetchMetricsHistory(context.Background(), cfg, "svc_abc", "24h")
	require.NoError(t, err)

	// The handler reads the window from `range`; `window` was ignored.
	assert.Equal(t, "24h", seen.Get("range"))
	assert.Empty(t, seen.Get("window"))

	assert.Equal(t, "24h", resp.TimeRange)
	assert.Equal(t, "15m", resp.Resolution)
	assert.Equal(t, "switchyard-api replica switchyard-api-abc", resp.Scope)
	require.NotNil(t, resp.RetainedSince)
	require.Len(t, resp.DataPoints, 2)

	first := resp.DataPoints[0]
	assert.Equal(t, "2026-11-04T09:00:00Z", first.Timestamp.UTC().Format("2006-01-02T15:04:05Z"))
	assert.InDelta(t, 12.5, first.RequestsPerSec, 1e-9)
	assert.InDelta(t, 0.125, first.CPUUsage, 1e-9)
	assert.InDelta(t, 182.4, first.MemoryUsage, 1e-9)
	require.NotNil(t, first.AverageLatencyMs)
	assert.InDelta(t, 41.25, *first.AverageLatencyMs, 1e-9)
	require.NotNil(t, first.ErrorRate)
	assert.InDelta(t, 0.02, *first.ErrorRate, 1e-9)

	idle := resp.DataPoints[1]
	assert.Nil(t, idle.AverageLatencyMs)
	assert.Nil(t, idle.ErrorRate)
}

func TestRenderMetricsHistory(t *testing.T) {
	latency := 41.25
	errorRate := 0.02
	resp := &metricsHistoryResponse{
		Scope: "switchyard-api replica switchyard-api-abc",
		DataPoints: []metricsHistoryPoint{
			{RequestsPerSec: 12.5, AverageLatencyMs: &latency, ErrorRate: &errorRate, CPUUsage: 0.125, MemoryUsage: 182.4},
			{RequestsPerSec: 0, CPUUsage: 0.01, MemoryUsage: 181},
		},
	}
	var out bytes.Buffer
	require.NoError(t, renderMetricsHistory(&out, resp))
	text := out.String()
	assert.Contains(t, text, "Scope: switchyard-api replica switchyard-api-abc")
	assert.Contains(t, text, "41.25ms")
	assert.Contains(t, text, "2.00%")
	assert.Contains(t, text, "182.4")
	assert.NotContains(t, text, "No history points")
}

func TestRenderMetricsHistory_EmptyShowsNote(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, renderMetricsHistory(&out, &metricsHistoryResponse{Note: "No history is kept by this replica."}))
	assert.Contains(t, out.String(), "Note: No history is kept by this replica.")
	assert.Contains(t, out.String(), "No history points returned")
}
