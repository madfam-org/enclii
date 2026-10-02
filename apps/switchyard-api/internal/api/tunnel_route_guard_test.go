package api

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	route "github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// fixtureEnvs classifies the fixture namespaces the way loadProjectEnvironments
// would for the incident project.
func fixtureEnvs() projectEnvironments {
	return projectEnvironments{namespaceEnv: map[string]string{
		fixtureProdNS:    defaultProductionEnvironmentName,
		fixtureStagingNS: "staging",
	}}
}

func guardWith(health string, resolveErr error, allow ...string) tunnelRouteGuard {
	allowed := map[string]bool{}
	for _, host := range allow {
		allowed[host] = true
	}
	return tunnelRouteGuard{
		envs:         fixtureEnvs(),
		allowRepoint: allowed,
		liveHealth: func(context.Context, clusterBackend) liveBackendState {
			return liveBackendState{Health: health, Detail: "fixture"}
		},
		resolve: func(context.Context, *route.RouteSpec) error { return resolveErr },
	}
}

func updateRow(host, current, service, namespace, env string) tunnelRoutePlanItem {
	return tunnelRoutePlanItem{
		Hostname: host, Action: tunnelActionUpdate, CurrentService: current,
		ServiceName: service, Namespace: namespace, Port: 80, Environment: env, EnvironmentSource: junctionEnvDefault,
	}
}

func TestTunnelRouteGuard(t *testing.T) {
	apiLive := "http://acme-api.acme.svc.cluster.local:80"
	stagingLive := "http://acme-web.enclii-acme-staging.svc.cluster.local:80"

	tests := []struct {
		name       string
		guard      tunnelRouteGuard
		item       tunnelRoutePlanItem
		wantAction string
		wantLabel  string
		wantGuard  string
		wantBlock  bool
	}{
		{
			name:       "repoint of a serving production route is refused",
			guard:      guardWith(backendServing, nil),
			item:       updateRow("api.example.test", apiLive, "acme-web", fixtureProdNS, "production"),
			wantAction: tunnelActionRepoint, wantLabel: tunnelLabelRepointBlocked, wantGuard: tunnelGuardRepoint, wantBlock: true,
		},
		{
			name:       "health that cannot be determined is treated as serving",
			guard:      guardWith(backendUnknown, nil),
			item:       updateRow("api.example.test", apiLive, "acme-web", fixtureProdNS, "production"),
			wantAction: tunnelActionRepoint, wantLabel: tunnelLabelRepointBlocked, wantGuard: tunnelGuardRepoint, wantBlock: true,
		},
		{
			name:       "a route whose backend is not serving is a drift fix",
			guard:      guardWith(backendNotServing, nil),
			item:       updateRow("api.example.test", apiLive, "acme-web", fixtureProdNS, "production"),
			wantAction: tunnelActionUpdate, wantLabel: "UPDATE", wantBlock: false,
		},
		{
			name:       "allow-repoint names the hostname",
			guard:      guardWith(backendServing, nil, "api.example.test"),
			item:       updateRow("api.example.test", apiLive, "acme-web", fixtureProdNS, "production"),
			wantAction: tunnelActionRepoint, wantLabel: tunnelLabelRepointAllowed, wantGuard: tunnelGuardRepoint, wantBlock: false,
		},
		{
			name:       "allow-repoint for another hostname does not apply",
			guard:      guardWith(backendServing, nil, "admin.example.test"),
			item:       updateRow("api.example.test", apiLive, "acme-web", fixtureProdNS, "production"),
			wantAction: tunnelActionRepoint, wantLabel: tunnelLabelRepointBlocked, wantGuard: tunnelGuardRepoint, wantBlock: true,
		},
		{
			name:       "staging route is never moved into production, even when its backend is down",
			guard:      guardWith(backendNotServing, nil),
			item:       updateRow("staging.example.test", stagingLive, "acme-web", fixtureProdNS, "production"),
			wantAction: tunnelActionRepoint, wantLabel: tunnelLabelRepointBlocked, wantGuard: tunnelGuardCrossEnv, wantBlock: true,
		},
		{
			name:       "production route is never moved into staging by inference",
			guard:      guardWith(backendNotServing, nil),
			item:       updateRow("api.example.test", apiLive, "acme-api", fixtureStagingNS, "staging"),
			wantAction: tunnelActionRepoint, wantLabel: tunnelLabelRepointBlocked, wantGuard: tunnelGuardCrossEnv, wantBlock: true,
		},
		{
			name:  "a desired namespace from another environment is a data error that allow-repoint cannot override",
			guard: guardWith(backendServing, nil, "staging-api.example.test"),
			item: updateRow("staging-api.example.test", "http://acme-api.enclii-acme-staging.svc.cluster.local:80",
				"acme-api", fixtureProdNS, "staging"),
			wantAction: tunnelActionBlocked, wantLabel: tunnelLabelCrossEnvBlocked, wantGuard: tunnelGuardCrossEnv, wantBlock: true,
		},
		{
			name:  "same service and namespace with a different port is ordinary drift",
			guard: guardWith(backendServing, nil),
			item: updateRow("nauta.example.com", "http://web.nauta.svc.cluster.local:3000",
				"web", "nauta", "production"),
			wantAction: tunnelActionUpdate, wantLabel: "UPDATE", wantBlock: false,
		},
		{
			name:  "an external origin is never replaced without allow-repoint",
			guard: guardWith(backendServing, nil),
			item: updateRow("api.example.test", "https://origin.example.net",
				"acme-api", fixtureProdNS, "production"),
			wantAction: tunnelActionRepoint, wantLabel: tunnelLabelRepointBlocked, wantGuard: tunnelGuardRepoint, wantBlock: true,
		},
		{
			name:  "a drift fix to a backend that does not resolve is refused",
			guard: guardWith(backendNotServing, errors.New("no Service")),
			item: updateRow("api.example.test", apiLive,
				"acme-web", fixtureProdNS, "production"),
			wantAction: tunnelActionBlocked, wantLabel: tunnelLabelBlocked, wantGuard: tunnelGuardUnresolvable, wantBlock: true,
		},
		{
			name:  "a create whose backend does not resolve is refused",
			guard: guardWith(backendServing, errors.New("no Service")),
			item: tunnelRoutePlanItem{Hostname: "new.example.test", Action: tunnelActionCreate,
				ServiceName: "acme-web", Namespace: fixtureProdNS, Port: 80, Environment: "production"},
			wantAction: tunnelActionBlocked, wantLabel: tunnelLabelBlocked, wantGuard: tunnelGuardUnresolvable, wantBlock: true,
		},
		{
			name:  "a create into the right environment proceeds",
			guard: guardWith(backendServing, nil),
			item: tunnelRoutePlanItem{Hostname: "staging-new.example.test", Action: tunnelActionCreate,
				ServiceName: "acme-web", Namespace: fixtureStagingNS, Port: 80, Environment: "staging"},
			wantAction: tunnelActionCreate, wantLabel: "CREATE", wantBlock: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := tt.item
			tt.guard.evaluate(context.Background(), &item)
			assert.Equal(t, tt.wantAction, item.Action, item.Reason)
			assert.Equal(t, tt.wantLabel, item.Label, item.Reason)
			assert.Equal(t, tt.wantGuard, item.Guard, item.Reason)
			assert.Equal(t, tt.wantBlock, item.Blocked, item.Reason)
			assert.Equal(t, !tt.wantBlock && tt.wantAction != tunnelActionSkip, item.executable())
		})
	}
}

func TestParseClusterBackendAcceptsEverySpelling(t *testing.T) {
	for raw, want := range map[string]clusterBackend{
		"http://acme-api.acme.svc.cluster.local:80": {"acme-api", "acme", 80},
		"http://acme-api.acme.svc:80":               {"acme-api", "acme", 80},
		"http://acme-api.acme.svc":                  {"acme-api", "acme", 80},
		"https://acme-api.acme.svc.cluster.local":   {"acme-api", "acme", 443},
		"http://web.nauta.svc.cluster.local:3000/":  {"web", "nauta", 3000},
	} {
		got, ok := parseClusterBackend(raw)
		require.True(t, ok, raw)
		assert.Equal(t, want, got, raw)
	}
	for _, raw := range []string{"http_status:404", "https://origin.example.net", ""} {
		_, ok := parseClusterBackend(raw)
		assert.False(t, ok, raw)
	}
}

// A hand-edited rule spelled in the short form is the same backend, not drift.
func TestPlanTunnelRouteDriftsTreatsEquivalentSpellingAsSkip(t *testing.T) {
	live := []route.IngressRule{{Hostname: "api.example.test", Service: "http://acme-api.acme.svc:80"}}
	plan := planTunnelRouteDrifts(live, []*route.RouteSpec{{
		Hostname: "api.example.test", ServiceName: "acme-api", ServiceNamespace: "acme", ServicePort: 80,
	}})
	require.Len(t, plan, 1)
	assert.Equal(t, tunnelActionSkip, plan[0].Action)
}

func TestLiveTunnelBackendHealth(t *testing.T) {
	ctx := context.Background()
	serving := (&Handler{k8sClient: servingCluster()}).liveTunnelBackendHealth(ctx, clusterBackend{"acme-api", fixtureProdNS, 80})
	assert.Equal(t, backendServing, serving.Health, serving.Detail)

	missing := (&Handler{k8sClient: servingCluster()}).liveTunnelBackendHealth(ctx, clusterBackend{"nope", fixtureProdNS, 80})
	assert.Equal(t, backendNotServing, missing.Health, missing.Detail)

	wrongPort := (&Handler{k8sClient: servingCluster()}).liveTunnelBackendHealth(ctx, clusterBackend{"acme-api", fixtureProdNS, 8080})
	assert.Equal(t, backendNotServing, wrongPort.Health, wrongPort.Detail)

	noPods := &k8s.Client{KubeClient: fake.NewSimpleClientset([]runtime.Object{servingService(fixtureProdNS, "acme-api")}...)}
	idle := (&Handler{k8sClient: noPods}).liveTunnelBackendHealth(ctx, clusterBackend{"acme-api", fixtureProdNS, 80})
	assert.Equal(t, backendNotServing, idle.Health, idle.Detail)

	unknown := (&Handler{}).liveTunnelBackendHealth(ctx, clusterBackend{"acme-api", fixtureProdNS, 80})
	assert.Equal(t, backendUnknown, unknown.Health)
}

// The automated reconcile paths (push, junction create, ops domains
// reconcile, domains add) all write through ensureTunnelRoute. With the
// incident bindings, re-asserting the API hostname on the web service must be
// refused while the API workload is serving.
func TestEnsureTunnelRouteRefusesRepointOfServingRoute(t *testing.T) {
	routes := newMockTunnelRoutesManager()
	routes.routes["api.example.test"] = &route.RouteSpec{
		Hostname: "api.example.test", ServiceName: "acme-api", ServiceNamespace: fixtureProdNS, ServicePort: 80,
	}
	logger := &recordingLogger{}
	prodNS := fixtureProdNS
	h := &Handler{
		tunnelRoutesService: routes,
		logger:              logger,
		k8sClient:           servingCluster(),
		config:              &config.Config{},
	}

	h.ensureTunnelRoute(context.Background(), "api.example.test", &types.Service{
		ID: uuid.New(), Name: "acme-web", K8sNamespace: &prodNS,
	}, "production", 80, nil)

	assert.Equal(t, "acme-api", routes.routes["api.example.test"].ServiceName, "a serving route was repointed")
	require.NotEmpty(t, logger.errors)
	assert.Contains(t, logger.errors[0], "REFUSED to repoint a serving tunnel route")
}

// The other half of the same rule: a route whose backend is gone is still
// repaired by the automated path.
func TestEnsureTunnelRouteRepairsRouteToMissingBackend(t *testing.T) {
	routes := newMockTunnelRoutesManager()
	routes.routes["api.example.test"] = &route.RouteSpec{
		Hostname: "api.example.test", ServiceName: "retired-api", ServiceNamespace: fixtureProdNS, ServicePort: 80,
	}
	prodNS := fixtureProdNS
	h := &Handler{
		tunnelRoutesService: routes,
		logger:              newNopLogger(),
		k8sClient:           servingCluster(),
		config:              &config.Config{},
	}

	h.ensureTunnelRoute(context.Background(), "api.example.test", &types.Service{
		ID: uuid.New(), Name: "acme-api", K8sNamespace: &prodNS,
	}, "production", 80, nil)

	assert.Equal(t, "acme-api", routes.routes["api.example.test"].ServiceName)
}

// A non-production environment with no recorded namespace never falls back to
// the service's own (production) namespace.
func TestResolveServiceNamespaceNeverRoutesStagingToProductionNamespace(t *testing.T) {
	prodNS := fixtureProdNS
	h := &Handler{logger: newNopLogger()}
	got := h.resolveServiceNamespace(context.Background(), &types.Service{
		ID: uuid.New(), Name: "acme-api", K8sNamespace: &prodNS,
	}, "staging")
	assert.NotEqual(t, fixtureProdNS, got)
}
