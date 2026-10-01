package api

import (
	"context"
	"database/sql/driver"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func rebindRequest(dryRun bool, host, service, env string) operatorOperationRequest {
	return operatorOperationRequest{
		Operation: "ops.junctions.rebind",
		DryRun:    dryRun,
		Reason:    "bind to the serving workload",
		Scope:     map[string]string{"project": fixtureProject},
		Args:      map[string]string{"target": host, "to_service": service, "environment": env},
	}
}

func rebindRows(t *testing.T, resp operatorOperationResponse) []junctionRebindRow {
	t.Helper()
	data, ok := resp.Data.(map[string]any)
	require.True(t, ok, "%#v", resp)
	rows, ok := data["rows"].([]junctionRebindRow)
	require.True(t, ok)
	return rows
}

// The owner's dry run for the production API hostname: the plan names the
// binding change and proves the follow-up tunnels-apply is a no-op for it.
func TestJunctionRebind_DryRunPlansTheIncidentRepair(t *testing.T) {
	f := newTunnelFixture(t, incidentJunctions())

	resp := f.handler.handleOpsJunctionsRebindDryRun(context.Background(), "ops.junctions.rebind",
		rebindRequest(true, "api.dhan.am", fixtureAPIService, "production"))

	require.Equal(t, "ready_to_apply", resp.Status, resp.Summary)
	rows := rebindRows(t, resp)
	require.Len(t, rows, 1)
	row := rows[0]
	assert.Equal(t, "rebind", row.Action)
	assert.Equal(t, fixtureWebService, row.CurrentService)
	assert.Equal(t, junctionEnvDefault, row.CurrentEnvSource)
	assert.Equal(t, fixtureAPIService, row.TargetService)
	assert.Equal(t, "production", row.TargetEnvironment)
	assert.Equal(t, "http://dhanam-api.dhanam.svc.cluster.local:80", row.BackendAfter)
	assert.True(t, row.LiveMatches, "the live route already serves the target binding")
	f.assertLiveRoutesUnchanged()
}

// A staging hostname rebinds into the staging environment's namespace.
func TestJunctionRebind_StagingTargetsStagingNamespace(t *testing.T) {
	f := newTunnelFixture(t, incidentJunctions())

	resp := f.handler.handleOpsJunctionsRebindDryRun(context.Background(), "ops.junctions.rebind",
		rebindRequest(true, "staging-admin.dhan.am", fixtureAdminServic, "staging"))

	require.Equal(t, "ready_to_apply", resp.Status, resp.Summary)
	row := rebindRows(t, resp)[0]
	assert.Equal(t, "http://dhanam-admin.enclii-dhanam-staging.svc.cluster.local:80", row.BackendAfter)
	assert.True(t, row.LiveMatches)
}

func TestJunctionRebind_ApplyRewritesOnlyTheBinding(t *testing.T) {
	f := newTunnelFixture(t, incidentJunctions())
	envID := f.envIDs["staging"]
	f.mock.ExpectExec(`UPDATE junctions\s+SET service_id = \$1, environment_id = \$2`).
		WithArgs(f.services[fixtureAPIService], uuid.NullUUID{UUID: envID, Valid: true}, sqlmock.AnyArg(),
			f.junctions["staging-api.dhan.am"], f.projectID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	resp, code := f.handler.handleOpsJunctionsRebindApply(context.Background(), "ops.junctions.rebind",
		rebindRequest(false, "staging-api.dhan.am", fixtureAPIService, "staging"))

	assert.Equal(t, http.StatusOK, code, resp.Summary)
	assert.Equal(t, "succeeded", resp.Status)
	assert.Contains(t, resp.Summary, "the tunnel route was not touched")
	f.assertLiveRoutesUnchanged()
	// The only follow-up offered is the scoped DRY RUN.
	require.Len(t, resp.Next, 1)
	assert.Equal(t, "enclii providers cloudflare tunnels-apply staging-api.dhan.am --project dhanam", resp.Next[0])
}

// Idempotent: a junction already bound as asked is reported and not written.
// No UPDATE expectation is registered, so a write would fail the test.
func TestJunctionRebind_AlreadyBoundIsANoop(t *testing.T) {
	f := newTunnelFixture(t, correctedJunctions())

	resp, code := f.handler.handleOpsJunctionsRebindApply(context.Background(), "ops.junctions.rebind",
		rebindRequest(false, "api.dhan.am", fixtureAPIService, "production"))

	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "succeeded", resp.Status)
	assert.Contains(t, resp.Summary, "already bound")
	assert.Equal(t, "noop", rebindRows(t, resp)[0].Action)
}

// Every unresolvable input fails closed, names what exists, and changes
// nothing.
func TestJunctionRebind_RefusesWhatItCannotResolve(t *testing.T) {
	cases := []struct {
		name, host, service, env, want string
	}{
		{"unknown hostname", "nope.dhan.am", fixtureAPIService, "production", "has no junction for nope.dhan.am"},
		{"unknown service", "api.dhan.am", "dhanam-worker", "production", "has no service \"dhanam-worker\""},
		{"unknown environment", "api.dhan.am", fixtureAPIService, "preview", "has no environment \"preview\""},
		{"missing input", "api.dhan.am", "", "production", "missing --to-service"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newTunnelFixture(t, incidentJunctions())
			resp, code := f.handler.handleOpsJunctionsRebindApply(context.Background(), "ops.junctions.rebind",
				rebindRequest(false, tc.host, tc.service, tc.env))
			assert.Equal(t, http.StatusBadRequest, code)
			assert.Equal(t, "invalid_request", resp.Status)
			assert.True(t, strings.Contains(resp.Summary, tc.want), resp.Summary)
		})
	}
}

// Without a rebind, a hostname whose domain record says staging is planned in
// the staging environment, and tunnels-apply then plans it there.
func TestResolveJunctionEnvironmentFallsBackToDomainRecord(t *testing.T) {
	f := newTunnelFixture(t, incidentJunctions(),
		fixtureDomainRecord{Host: "staging.dhan.am", Service: fixtureWebService, Environment: "staging"})
	project := &types.Project{ID: f.projectID, Slug: fixtureProject}
	envs := f.handler.loadProjectEnvironments(project)

	got := f.handler.resolveJunctionEnvironment(context.Background(),
		&types.Junction{Domain: "staging.dhan.am", ServiceID: f.services[fixtureWebService]}, envs)
	assert.Equal(t, "staging", got.Name)
	assert.Equal(t, junctionEnvFromDomainRecord, got.Source)
	assert.Equal(t, fixtureStagingNS, f.handler.namespaceForEnvironment(context.Background(), project, nil, got.Name, envs))

	// The same hostname in the full plan: no longer a cross-environment
	// repoint, because the record puts it in staging, where it is served.
	resp := f.handler.handleProviderCloudflareTunnelsApplyDryRun(context.Background(),
		"providers.cloudflare.tunnels-apply", tunnelsApplyRequest(true, map[string]string{"target": "staging.dhan.am"}))
	item := planByHost(t, resp)["staging.dhan.am"]
	assert.Equal(t, "SKIP", item.Label, item.Reason)
	assert.Equal(t, junctionEnvFromDomainRecord, item.EnvironmentSource)
}

func customDomainColumnNames() []string {
	return []string{
		"id", "service_id", "environment_id", "domain", "verified", "tls_enabled", "tls_issuer",
		"created_at", "updated_at", "verified_at", "cloudflare_tunnel_id", "is_platform_domain",
		"zero_trust_enabled", "access_policy_id", "tls_provider", "status", "dns_cname",
		"custom_hostname_id", "custom_hostname_status", "custom_hostname_ssl_status",
		"pending_dns_records", "provisioning_error", "provisioning_checked_at",
	}
}

func customDomainRowValues(id, serviceID, envID uuid.UUID, domain string, now time.Time) []driver.Value {
	return []driver.Value{
		id, serviceID, envID, domain, true, true, "letsencrypt-staging",
		now, now, nil, nil, false,
		false, nil, "cert-manager", "active", nil,
		nil, nil, nil,
		nil, nil, nil,
	}
}
