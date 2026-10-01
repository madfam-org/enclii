package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tunnelsApplyRequest(dryRun bool, args map[string]string) operatorOperationRequest {
	if args == nil {
		args = map[string]string{}
	}
	return operatorOperationRequest{
		Operation: "providers.cloudflare.tunnels-apply",
		DryRun:    dryRun,
		Reason:    "test",
		Scope:     map[string]string{"project": fixtureProject},
		Args:      args,
	}
}

func planByHost(t *testing.T, resp operatorOperationResponse) map[string]tunnelRoutePlanItem {
	t.Helper()
	data, ok := resp.Data.(map[string]any)
	require.True(t, ok, "response data: %#v", resp.Data)
	plan, ok := data["plan"].([]tunnelRoutePlanItem)
	require.True(t, ok, "plan: %#v", data["plan"])
	out := map[string]tunnelRoutePlanItem{}
	for _, item := range plan {
		out[item.Hostname] = item
	}
	return out
}

// The incident, replayed against the new planner: the same junction data that
// caused the outage now yields five refused repoints and nothing executable.
func TestTunnelsApply_IncidentBindings_DryRunRefusesEveryRepoint(t *testing.T) {
	f := newTunnelFixture(t, incidentJunctions())

	resp := f.handler.handleProviderCloudflareTunnelsApplyDryRun(context.Background(),
		"providers.cloudflare.tunnels-apply", tunnelsApplyRequest(true, nil))

	assert.Equal(t, "blocked", resp.Status)
	assert.True(t, strings.HasPrefix(resp.Summary, "REFUSED: 5 row(s) blocked (5 REPOINT (blocked))"), resp.Summary)

	plan := planByHost(t, resp)
	for _, host := range []string{"api.dhan.am", "admin.dhan.am"} {
		item := plan[host]
		assert.Equal(t, tunnelLabelRepointBlocked, item.Label, host)
		assert.Equal(t, tunnelGuardRepoint, item.Guard, host)
		assert.True(t, item.Blocked, host)
		assert.Equal(t, backendServing, item.CurrentHealth, host)
		assert.Contains(t, item.Reason, "service ", host)
	}
	// Staging hostnames: the junction says nothing about an environment, so
	// it plans production, and moving a staging route into production is
	// refused as a cross-environment move.
	for _, host := range []string{"staging-api.dhan.am", "staging-admin.dhan.am", "staging.dhan.am"} {
		item := plan[host]
		assert.Equal(t, tunnelLabelRepointBlocked, item.Label, host)
		assert.Equal(t, tunnelGuardCrossEnv, item.Guard, host)
		assert.Equal(t, fixtureProdNS, item.Namespace, host)
		assert.Equal(t, junctionEnvDefault, item.EnvironmentSource, host)
	}
	for _, host := range []string{"dhan.am", "www.dhan.am", "app.dhan.am"} {
		assert.Equal(t, "SKIP", plan[host].Label, host)
	}

	data := resp.Data.(map[string]any)
	assert.Equal(t, 0, data["count"])
	assert.Equal(t, "", data["plan_fingerprint"])
	// Only dry-run rebind previews are suggested; never an apply.
	require.Len(t, resp.Next, 5)
	for _, next := range resp.Next {
		assert.True(t, strings.HasPrefix(next, "enclii ops junctions rebind "), next)
		assert.NotContains(t, next, "--apply")
	}
	assert.Contains(t, resp.Next, "enclii ops junctions rebind api.dhan.am --project dhanam --to-service dhanam-api --environment production")
	assert.Contains(t, resp.Next, "enclii ops junctions rebind staging-admin.dhan.am --project dhanam --to-service dhanam-admin --environment staging")
}

// The command that caused the outage, run again: refused as a whole, and the
// tunnel is not touched.
func TestTunnelsApply_IncidentBindings_ApplyWritesNothing(t *testing.T) {
	f := newTunnelFixture(t, incidentJunctions())

	resp, code := f.handler.handleProviderCloudflareTunnelsApply(context.Background(),
		"providers.cloudflare.tunnels-apply", tunnelsApplyRequest(false, nil))

	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, "blocked", resp.Status)
	assert.True(t, strings.HasPrefix(resp.Summary, "REFUSED:"), resp.Summary)
	assert.Contains(t, resp.Summary, "Nothing was applied")
	f.assertLiveRoutesUnchanged()
}

// --allow-repoint names ONE hostname. The other refusals still stand, and an
// apply is all-or-nothing, so the project-wide apply is still refused.
func TestTunnelsApply_AllowRepointIsPerHostname(t *testing.T) {
	f := newTunnelFixture(t, incidentJunctions())

	resp := f.handler.handleProviderCloudflareTunnelsApplyDryRun(context.Background(),
		"providers.cloudflare.tunnels-apply", tunnelsApplyRequest(true, map[string]string{"allow_repoint": "api.dhan.am"}))
	plan := planByHost(t, resp)
	assert.Equal(t, tunnelLabelRepointAllowed, plan["api.dhan.am"].Label)
	assert.False(t, plan["api.dhan.am"].Blocked)
	assert.Equal(t, tunnelLabelRepointBlocked, plan["admin.dhan.am"].Label)
	assert.Equal(t, "blocked", resp.Status)

	_, code := f.handler.handleProviderCloudflareTunnelsApply(context.Background(),
		"providers.cloudflare.tunnels-apply", tunnelsApplyRequest(false, map[string]string{"allow_repoint": "api.dhan.am"}))
	assert.Equal(t, http.StatusConflict, code)
	f.assertLiveRoutesUnchanged()
}

// Scoped to one allowed hostname, the repoint does execute: the guard is an
// explicit, per-host decision, not a wall.
func TestTunnelsApply_ScopedAllowedRepointExecutes(t *testing.T) {
	f := newTunnelFixture(t, incidentJunctions())
	args := map[string]string{"target": "api.dhan.am", "allow_repoint": "api.dhan.am"}

	dry := f.handler.handleProviderCloudflareTunnelsApplyDryRun(context.Background(),
		"providers.cloudflare.tunnels-apply", tunnelsApplyRequest(true, args))
	require.Equal(t, "ready_to_apply", dry.Status, dry.Summary)
	fingerprint := dry.Data.(map[string]any)["plan_fingerprint"].(string)
	require.NotEmpty(t, fingerprint)
	assert.Contains(t, dry.Next[0], "--expect-plan "+fingerprint)

	args["expect_plan"] = fingerprint
	resp, code := f.handler.handleProviderCloudflareTunnelsApply(context.Background(),
		"providers.cloudflare.tunnels-apply", tunnelsApplyRequest(false, args))
	assert.Equal(t, http.StatusAccepted, code, resp.Summary)
	assert.Equal(t, fixtureWebService+"."+fixtureProdNS, f.liveRoute("api.dhan.am"))
	assert.Equal(t, fixtureAdminServic+"."+fixtureProdNS, f.liveRoute("admin.dhan.am"), "out-of-scope hostname touched")
}

// A plan that changed since the reviewed dry run is refused, not applied.
func TestTunnelsApply_ExpectPlanMismatchRefuses(t *testing.T) {
	f := newTunnelFixture(t, incidentJunctions())
	args := map[string]string{"target": "api.dhan.am", "allow_repoint": "api.dhan.am", "expect_plan": "000000000000"}

	resp, code := f.handler.handleProviderCloudflareTunnelsApply(context.Background(),
		"providers.cloudflare.tunnels-apply", tunnelsApplyRequest(false, args))
	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, "plan_changed", resp.Status)
	f.assertLiveRoutesUnchanged()
}

// The target bindings: every hostname on the service and environment that
// serve it. The plan is all SKIP, which is how the owner verifies the rebind.
func TestTunnelsApply_CorrectedBindingsPlanNothing(t *testing.T) {
	f := newTunnelFixture(t, correctedJunctions())

	resp := f.handler.handleProviderCloudflareTunnelsApplyDryRun(context.Background(),
		"providers.cloudflare.tunnels-apply", tunnelsApplyRequest(true, nil))

	assert.Equal(t, "succeeded", resp.Status, resp.Summary)
	plan := planByHost(t, resp)
	require.Len(t, plan, len(fixtureLiveRoutes))
	for host, item := range plan {
		assert.Equal(t, "SKIP", item.Label, "%s: %s", host, item.Reason)
		assert.Equal(t, junctionEnvFromJunction, item.EnvironmentSource, host)
	}
	assert.Equal(t, fixtureStagingNS, plan["staging-api.dhan.am"].Namespace)
	assert.Empty(t, resp.Next)
}
