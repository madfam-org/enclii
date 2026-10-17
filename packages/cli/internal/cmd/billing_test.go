package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

// Bodies shaped like Waybill's getProjectCost (apps/waybill/internal/api/
// cost_handlers.go) as relayed by the switchyard billing proxy.
const (
	costNotRunningBody = `{"project_id":"7c1d0f8e-6a2b-4c3d-9e8f-1a2b3c4d5e6f","period_start":"2026-09-17T20:33:53Z","period_end":"2026-10-17T20:33:53Z","total_cents":0,"spend_known":false,"metering":{"status":"not_running","detail":"the waybill aggregator has never run against this database; spend is unknown","last_heartbeat_at":null,"last_aggregated_hour":null,"project_usage_rows":0,"unaggregated_events":0,"stale_after_seconds":3600,"checked_at":"2026-10-17T20:33:53Z"},"group_by":"metric","series":null,"breakdown":null}`
	costOKBody         = `{"project_id":"7c1d0f8e-6a2b-4c3d-9e8f-1a2b3c4d5e6f","period_start":"2026-09-17T20:33:53Z","period_end":"2026-10-17T20:33:53Z","total_cents":1234,"spend_known":true,"metering":{"status":"ok","detail":"aggregator alive and up to date","last_heartbeat_at":"2026-10-17T20:30:00Z","last_aggregated_hour":"2026-10-17T19:00:00Z","project_usage_rows":40,"unaggregated_events":0,"stale_after_seconds":3600,"checked_at":"2026-10-17T20:33:53Z"},"group_by":"metric","breakdown":[{"key":"compute_gb_hours","cost_cents":1000}]}`
	costLegacyBody     = `{"project_id":"7c1d0f8e-6a2b-4c3d-9e8f-1a2b3c4d5e6f","period_start":"a","period_end":"b","total_cents":0,"group_by":"metric"}`
	budgetsOneBody     = `{"budgets":[{"id":"b1","amount_cents":5000,"currency":"USD","period":"monthly","alert_thresholds":[50,80,100]}]}`
)

func newBillingTestServer(t *testing.T, costBody, budgetsBody string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/projects/demo/billing/cost":
			assert.Equal(t, "30d", r.URL.Query().Get("period"))
			_, _ = w.Write([]byte(costBody))
		case "/v1/projects/demo/billing/budgets":
			_, _ = w.Write([]byte(budgetsBody))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func runBillingShow(t *testing.T, costBody, budgetsBody string, args ...string) string {
	t.Helper()
	srv := newBillingTestServer(t, costBody, budgetsBody)
	cfg := &config.Config{APIEndpoint: srv.URL, APIToken: "test-token"}
	cmd := newBillingShowCommand(cfg)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs(append([]string{"--project", "demo"}, args...))
	require.NoError(t, cmd.Execute())
	return out.String()
}

func TestBillingShow_HasJSONFlag(t *testing.T) {
	cmd := newBillingShowCommand(&config.Config{})
	assert.NotNil(t, cmd.Flags().Lookup("json"))
}

func TestBillingShow_NotRunningNeverPrintsZeroSpend(t *testing.T) {
	out := runBillingShow(t, costNotRunningBody, budgetsOneBody)
	assert.Contains(t, out, "Spend:   unknown — metering is not running")
	assert.NotContains(t, out, "$0.00", "a not-running meter must never print as a $0.00 spend")
	assert.Contains(t, out, "Metering: not_running")
	assert.Contains(t, out, "consumption unknown")
	assert.NotContains(t, out, "% consumed")
}

func TestBillingShow_OKPrintsFigureAndConsumption(t *testing.T) {
	out := runBillingShow(t, costOKBody, budgetsOneBody)
	assert.Contains(t, out, "Spend:   $12.34\n")
	assert.Contains(t, out, "24% consumed")
	assert.NotContains(t, out, "Metering:")
	assert.Contains(t, out, "compute_gb_hours")
}

func TestBillingShow_LegacyAPIIsUnverified(t *testing.T) {
	out := runBillingShow(t, costLegacyBody, `{"budgets":[]}`)
	assert.Contains(t, out, "$0.00 (unverified: this API does not report metering status)")
}

func TestBillingShow_JSONCarriesMetering(t *testing.T) {
	out := runBillingShow(t, costNotRunningBody, `{"budgets":null}`, "--json")
	var got struct {
		Project string `json:"project"`
		Cost    struct {
			TotalCents int64 `json:"total_cents"`
			SpendKnown *bool `json:"spend_known"`
			Metering   struct {
				Status string `json:"status"`
			} `json:"metering"`
		} `json:"cost"`
		Budgets []json.RawMessage `json:"budgets"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &got), out)
	assert.Equal(t, "demo", got.Project)
	require.NotNil(t, got.Cost.SpendKnown)
	assert.False(t, *got.Cost.SpendKnown)
	assert.Equal(t, "not_running", got.Cost.Metering.Status)
	assert.NotNil(t, got.Budgets, "budgets is an empty array, not null")
	assert.True(t, strings.HasPrefix(strings.TrimSpace(out), "{"))
}

func TestBillingSpendLine_Stale(t *testing.T) {
	beat := "2026-10-17T15:00:00Z"
	c := &billingCost{TotalCents: 500, Metering: &billingMetering{Status: "stale", LastHeartbeatAt: &beat}}
	line, known := c.spendLine()
	assert.False(t, known)
	assert.Equal(t, "at least $5.00 — metering is stale (last aggregator report: 2026-10-17T15:00:00Z)", line)

	c = &billingCost{TotalCents: 0, Metering: &billingMetering{Status: "stale"}}
	line, known = c.spendLine()
	assert.False(t, known)
	assert.Equal(t, "unknown — metering is stale (last aggregator report: never)", line)

	c = &billingCost{Metering: &billingMetering{Status: "unknown"}}
	line, known = c.spendLine()
	assert.False(t, known)
	assert.Contains(t, line, `unknown — metering status "unknown"`)
}
