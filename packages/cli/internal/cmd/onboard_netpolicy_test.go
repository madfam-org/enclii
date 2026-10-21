package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func refusedPlan(dryRun bool) map[string]interface{} {
	return map[string]interface{}{
		"namespace": "crea-map",
		"blocked":   true,
		"dry_run":   dryRun,
		"refusals":  []interface{}{map[string]interface{}{"kind": "selector_narrowed"}},
		"diff": []interface{}{
			"~ crea-map-egress (update)",
			"    podSelector: {} → app=crea-map",
			"REFUSE crea-map-egress: podSelector {} → app=crea-map; it stops applying to every pod without those labels",
		},
	}
}

// --dry-run calls only the read-only plan endpoint and prints the diff.
func TestRunOnboardEnsure_DryRunPrintsDiffAndChangesNothing(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		var req types.OnboardingRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.False(t, req.AllowNetpolNarrowing)
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{
			"namespace": "crea-map", "network_declared": true, "network_policy_plan": refusedPlan(true),
		}))
	}))
	defer server.Close()

	cfg := &config.Config{APIEndpoint: server.URL, APIToken: "test-token"}
	var err error
	out := captureStdoutStderr(t, func() {
		err = runOnboardEnsure(cfg, onboardEnsureOpts{repo: "madfam-org/crea-map", project: "crea-map", dryRun: true})
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"/v1/admin/onboard/netpolicy-plan"}, paths, "a dry run must not call ensure")
	assert.Contains(t, out, "NetworkPolicy diff (namespace crea-map): DRY RUN, the step WOULD BE REFUSED:")
	assert.Contains(t, out, "podSelector: {} → app=crea-map")
	assert.Contains(t, out, "--allow-netpol-narrowing")
}

// A refused step prints the diff and exits non-zero; the opt-in is sent.
func TestRunOnboardEnsure_RefusedPlanPrintsDiffAndFails(t *testing.T) {
	var sent types.OnboardingRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&sent))
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "partial", "mode": "repair",
			"step_results":        []interface{}{map[string]interface{}{"name": "network_policies", "status": "failed", "detail": "refused: ..."}},
			"network_policy_plan": refusedPlan(false),
		}))
	}))
	defer server.Close()

	cfg := &config.Config{APIEndpoint: server.URL, APIToken: "test-token"}
	var err error
	out := captureStdoutStderr(t, func() {
		err = runOnboardEnsure(cfg, onboardEnsureOpts{repo: "madfam-org/crea-map", project: "crea-map", allowNarrow: false})
	})
	require.Error(t, err)
	assert.Contains(t, out, "NetworkPolicy diff (namespace crea-map): REFUSED, nothing applied:")
	assert.Contains(t, out, "REFUSE crea-map-egress")

	_ = captureStdoutStderr(t, func() {
		_ = runOnboardEnsure(cfg, onboardEnsureOpts{repo: "madfam-org/crea-map", project: "crea-map", allowNarrow: true})
	})
	assert.True(t, sent.AllowNetpolNarrowing, "--allow-netpol-narrowing must reach the request")
}

func TestOnboardCommandsRegisterNarrowingFlag(t *testing.T) {
	cfg := &config.Config{}
	for _, c := range []struct {
		name  string
		flags []string
	}{
		{"onboard", []string{"allow-netpol-narrowing"}},
		{"ensure", []string{"allow-netpol-narrowing", "dry-run"}},
	} {
		cmd := NewOnboardCommand(cfg)
		if c.name == "ensure" {
			cmd = NewOnboardEnsureCommand(cfg)
		}
		for _, f := range c.flags {
			assert.NotNil(t, cmd.Flags().Lookup(f), "%s --%s", c.name, f)
		}
	}
}
