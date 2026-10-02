package api

// Tests for the follow-up that makes ServiceRepository.GetByID return
// k8s_namespace: what the planners now compute for a service whose recorded
// namespace differs from its project namespace, and that the automatic
// junction reconcile refuses to act on that difference.

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	route "github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// fixtureAdoptedNS is a namespace of the API service's own, outside the
// project namespace: the shape of a workload adopted from a live cluster.
const fixtureAdoptedNS = "acme-adopted"

// adoptedAPICluster is servingCluster plus the API service serving in its
// adopted namespace. With projectAPIServing false, the API service has no
// Service in the project namespace at all, so a route pointing there is dead.
func adoptedAPICluster(projectAPIServing bool) *k8s.Client {
	objects := []runtime.Object{
		servingService(fixtureAdoptedNS, fixtureAPIService), readyPod(fixtureAdoptedNS, fixtureAPIService),
	}
	for _, ns := range []string{fixtureProdNS, fixtureStagingNS} {
		for _, name := range []string{fixtureWebService, fixtureAPIService, fixtureAdminServic} {
			if ns == fixtureProdNS && name == fixtureAPIService && !projectAPIServing {
				continue
			}
			objects = append(objects, servingService(ns, name), readyPod(ns, name))
		}
	}
	return &k8s.Client{KubeClient: fake.NewSimpleClientset(objects...)}
}

func recordedAPINamespace() map[string]string {
	return map[string]string{fixtureAPIService: fixtureAdoptedNS}
}

func setLiveRoute(f *tunnelFixture, host, service, namespace string) {
	f.routes.routes[host] = &route.RouteSpec{Hostname: host, ServiceName: service, ServiceNamespace: namespace, ServicePort: 80}
}

// --- tunnels-apply (operator path) ---

// The case GetByID used to get wrong: the live route already serves the API
// from its adopted namespace. Planned from the project namespace it was a
// REPOINT (blocked) row; planned from the recorded namespace it is a SKIP, and
// the row says where the namespace came from.
func TestTunnelsApply_RecordedNamespaceMatchingLiveRouteIsSkip(t *testing.T) {
	f := newTunnelFixtureWithRecordedNamespaces(t, correctedJunctions(), recordedAPINamespace())
	f.handler.k8sClient = adoptedAPICluster(true)
	setLiveRoute(f, "api.example.test", fixtureAPIService, fixtureAdoptedNS)
	before := f.snapshotRoutes()

	resp := f.handler.handleProviderCloudflareTunnelsApplyDryRun(context.Background(),
		"providers.cloudflare.tunnels-apply", tunnelsApplyRequest(true, nil))

	plan := planByHost(t, resp)
	api := plan["api.example.test"]
	assert.Equal(t, "SKIP", api.Label)
	assert.Equal(t, fixtureAdoptedNS, api.Namespace)
	assert.Contains(t, api.Reason, "recorded namespace")
	assert.Contains(t, api.Reason, "derived from the project: "+fixtureProdNS)

	// The recorded namespace is a production fact: the staging hostname of
	// the same service still plans into the staging namespace.
	assert.Equal(t, fixtureStagingNS, plan["staging-api.example.test"].Namespace)
	assert.Equal(t, "SKIP", plan["staging-api.example.test"].Label)
	assert.NotContains(t, plan["staging-api.example.test"].Reason, "recorded namespace")

	for host, item := range plan {
		assert.False(t, item.Blocked, host)
	}
	assert.Equal(t, before, f.snapshotRoutes())
}

// The opposite mismatch: the live route serves from the project namespace and
// the record names another. The plan now proposes the recorded namespace, and
// the repoint guard refuses it because the live backend is serving; an apply
// writes nothing.
func TestTunnelsApply_RecordedNamespaceRepointOfServingRouteIsBlocked(t *testing.T) {
	f := newTunnelFixtureWithRecordedNamespaces(t, correctedJunctions(), recordedAPINamespace())
	f.handler.k8sClient = adoptedAPICluster(true)

	resp := f.handler.handleProviderCloudflareTunnelsApplyDryRun(context.Background(),
		"providers.cloudflare.tunnels-apply", tunnelsApplyRequest(true, nil))

	assert.Equal(t, "blocked", resp.Status)
	api := planByHost(t, resp)["api.example.test"]
	assert.Equal(t, tunnelLabelRepointBlocked, api.Label)
	assert.Equal(t, tunnelGuardRepoint, api.Guard)
	assert.Equal(t, fixtureAdoptedNS, api.Namespace)
	assert.Contains(t, api.Reason, "namespace "+fixtureProdNS+" -> "+fixtureAdoptedNS)
	assert.Contains(t, api.Reason, "recorded namespace")

	_, code := f.handler.handleProviderCloudflareTunnelsApply(context.Background(),
		"providers.cloudflare.tunnels-apply", tunnelsApplyRequest(false, nil))
	assert.Equal(t, http.StatusConflict, code)
	f.assertLiveRoutesUnchanged()
}

// --- the automatic junction reconcile ---

// The reconcile that runs for every junction of a project whenever one is
// created must not move a route because the recorded namespace is now read.
// Each case below is one the reconcile WOULD now write without the guard:
// the recorded backend resolves, and either the live route is dead, absent,
// or serving from the namespace the reconcile used to derive.
func TestJunctionReconcile_RefusesRecordedNamespaceShift(t *testing.T) {
	cases := []struct {
		name              string
		projectAPIServing bool
		liveRoute         bool
	}{
		{"live route in the project namespace is dead", false, true},
		{"no live route yet", true, false},
		{"live route in the project namespace is serving", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			junctions := []fixtureJunction{{"api.example.test", fixtureAPIService, "production"}}
			f := newTunnelFixtureWithRecordedNamespaces(t, junctions, recordedAPINamespace())
			f.handler.k8sClient = adoptedAPICluster(tc.projectAPIServing)
			logger := &recordingLogger{}
			f.handler.logger = logger
			if !tc.liveRoute {
				delete(f.routes.routes, "api.example.test")
			}
			before := f.liveRoute("api.example.test")

			summary := f.handler.reconcileJunctionTunnelRoutesForProject(context.Background(), f.project())

			assert.Equal(t, []string{"api.example.test"}, summary.Refused)
			assert.Equal(t, before, f.liveRoute("api.example.test"), "the automatic reconcile changed a route")
			assert.True(t, logger.sawError("REFUSED: the automatic junction reconcile does not move a route"),
				"errors: %v", logger.errors)
		})
	}
}

// When the live rule already targets the recorded backend there is nothing to
// refuse: the reconcile proceeds and, the route being right, writes nothing.
func TestJunctionReconcile_LiveRouteOnRecordedNamespaceIsNotRefused(t *testing.T) {
	junctions := []fixtureJunction{{"api.example.test", fixtureAPIService, "production"}}
	f := newTunnelFixtureWithRecordedNamespaces(t, junctions, recordedAPINamespace())
	f.handler.k8sClient = adoptedAPICluster(false)
	setLiveRoute(f, "api.example.test", fixtureAPIService, fixtureAdoptedNS)

	summary := f.handler.reconcileJunctionTunnelRoutesForProject(context.Background(), f.project())

	assert.Empty(t, summary.Refused)
	assert.Equal(t, fixtureAPIService+"."+fixtureAdoptedNS, f.liveRoute("api.example.test"))
}

// A service with no recorded namespace is never refused by this guard: the
// reconcile behaves exactly as before the change.
func TestRefuseRecordedNamespaceShift_NoRecordedNamespaceNeverRefuses(t *testing.T) {
	junctions := []fixtureJunction{{"api.example.test", fixtureAPIService, "production"}}
	f := newTunnelFixture(t, junctions)
	service, err := f.handler.repos.Services.GetByID(f.services[fixtureAPIService])
	require.NoError(t, err)
	require.Nil(t, service.K8sNamespace)

	assert.False(t, f.handler.refuseRecordedNamespaceShift(context.Background(), "api.example.test", service, "production"))
}

// --- helpers ---

func (f *tunnelFixture) project() *types.Project {
	return &types.Project{ID: f.projectID, Name: "Acme", Slug: fixtureProject}
}

// snapshotRoutes is every live route as hostname -> service.namespace.
func (f *tunnelFixture) snapshotRoutes() map[string]string {
	out := map[string]string{}
	for host := range f.routes.routes {
		out[host] = f.liveRoute(host)
	}
	return out
}
