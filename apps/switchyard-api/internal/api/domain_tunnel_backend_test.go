package api

// The 2026-08-27 janua outage, as a test.
//
// What happened: switchyard's domain provisioner captured janua's legacy
// single-doc enclii.yaml — one Service doc, `metadata.name: janua`,
// `runtime.port: 8080`, eight declared domains including auth.madfam.io and
// madfam.io — and derived a RouteSpec of {ServiceName: "janua", Namespace:
// "janua", Port: 8080} from it. No Service by that name exists in that
// namespace; the workloads are janua-api, janua-admin, janua-dashboard and
// janua-website, every one of them on port 80. The provisioner then rewrote the
// tunnel's hand-set ingress rules — auth.madfam.io had been pointing correctly
// at janua-api.janua.svc:80 — onto http://janua.janua.svc.cluster.local:8080,
// one hostname per config version, v212 through v218, inside 37 seconds. Every
// SSO surface in the ecosystem went dark.
//
// TestEnsureTunnelRoute_JanuaOutage is that scenario, byte for byte in the
// parts that matter: the same spec, a fake cluster containing exactly the four
// real Services on port 80 and nothing named `janua`, and the working
// auth.madfam.io rule sitting in the tunnel config waiting to be clobbered.
// Case (a) asserts it is not.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	route "github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// k8sService builds a Service exposing one port, the shape the resolvability
// gate reads.
func k8sService(namespace, name string, port int32) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{Port: port}},
		},
	}
}

// fakeKubeWithServices wires a fake clientset into the k8s.Client through the
// injectable KubeClient field, which is what makes Kube() return it. No REST
// config is set — a fake needs none, and the resolver's usable-client test is
// the nil check on Kube() rather than IsValid, per #463.
func fakeKubeWithServices(services ...*corev1.Service) *k8s.Client {
	objects := make([]runtime.Object, 0, len(services))
	for _, svc := range services {
		objects = append(objects, svc)
	}
	return &k8s.Client{KubeClient: fake.NewSimpleClientset(objects...)}
}

// januaClusterServices is the real janua namespace: four Services, all on 80,
// none of them named `janua`.
func januaClusterServices() []*corev1.Service {
	return []*corev1.Service{
		k8sService("janua", "janua-api", 80),
		k8sService("janua", "janua-admin", 80),
		k8sService("janua", "janua-dashboard", 80),
		k8sService("janua", "janua-website", 80),
	}
}

// recordingLogger captures messages by level so a test can assert that a
// refusal was LOUD, not merely correct. The outage's second failure was that
// nothing said anything.
type recordingLogger struct {
	nopLogger
	errors []string
	warns  []string
}

func (r *recordingLogger) Error(_ context.Context, msg string, _ ...logging.Field) {
	r.errors = append(r.errors, msg)
}

func (r *recordingLogger) Warn(_ context.Context, msg string, _ ...logging.Field) {
	r.warns = append(r.warns, msg)
}

func (r *recordingLogger) sawError(substr string) bool {
	for _, msg := range r.errors {
		if strings.Contains(msg, substr) {
			return true
		}
	}
	return false
}

// failingServiceGetKube returns a client whose Service Gets fail with a
// non-NotFound error: the "we could not check" case, which must never be
// treated as "we checked and it was fine".
func failingServiceGetKube() *k8s.Client {
	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("get", "services",
		func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("etcdserver: request timed out")
		})
	return &k8s.Client{KubeClient: clientset}
}

// guardedTunnelRoutes is the mock manager plus a write counter, so a test can
// distinguish "the rule still has the right value" from "nothing was written".
type guardedTunnelRoutes struct {
	rules   map[string]route.IngressRule
	adds    int
	removes int
	// listFails makes ListRoutes error, so a test can reach the branch where
	// the tunnel config could not be read and the hostname's current state is
	// therefore unknown.
	listFails bool
}

func newGuardedTunnelRoutes() *guardedTunnelRoutes {
	return &guardedTunnelRoutes{rules: map[string]route.IngressRule{}}
}

func (m *guardedTunnelRoutes) seed(hostname, serviceURL string) {
	m.rules[strings.ToLower(hostname)] = route.IngressRule{Hostname: hostname, Service: serviceURL}
}

func (m *guardedTunnelRoutes) AddRoute(_ context.Context, spec *route.RouteSpec) error {
	m.adds++
	m.rules[strings.ToLower(spec.Hostname)] = route.IngressRule{
		Hostname: spec.Hostname,
		Service:  tunnelRouteServiceURL(spec),
	}
	return nil
}

func (m *guardedTunnelRoutes) RemoveRoute(_ context.Context, hostname string) error {
	m.removes++
	delete(m.rules, strings.ToLower(hostname))
	return nil
}

func (m *guardedTunnelRoutes) ListRoutes(_ context.Context) ([]route.IngressRule, error) {
	if m.listFails {
		return nil, errors.New("cloudflared ConfigMap unreadable")
	}
	out := make([]route.IngressRule, 0, len(m.rules))
	for _, rule := range m.rules {
		out = append(out, rule)
	}
	return out, nil
}

func (m *guardedTunnelRoutes) RouteExists(_ context.Context, hostname string) (bool, error) {
	_, ok := m.rules[strings.ToLower(hostname)]
	return ok, nil
}

func (m *guardedTunnelRoutes) backend(hostname string) string {
	return m.rules[strings.ToLower(hostname)].Service
}

// deadBackendProbe stands in for the canary's dial. A real probe of a
// .svc.cluster.local name from a unit test spends seconds failing to resolve
// it, so the revert logic is exercised through this seam and the dial itself is
// tested directly in TestProbeTunnelBackend_AnyHTTPResponseIsAPass.
func deadBackendProbe(_ context.Context, spec *route.RouteSpec) error {
	return fmt.Errorf("nothing answered at %s", tunnelRouteServiceURL(spec))
}

// TestEnsureTunnelRoute_JanuaOutage is the regression test for the outage
// itself: the manifest-derived spec, the real cluster contents, and the four
// outcomes that together make the rewrite impossible.
func TestEnsureTunnelRoute_JanuaOutage(t *testing.T) {
	const (
		hostname    = "auth.madfam.io"
		workingRule = "http://janua-api.janua.svc.cluster.local:80"
		deadBackend = "http://janua.janua.svc.cluster.local:8080"
	)

	januaNamespace := "janua"
	// The service row the legacy single-doc manifest produces: bare name,
	// runtime port 8080.
	januaService := func() *types.Service {
		return &types.Service{
			ID:           uuid.New(),
			ProjectID:    uuid.New(),
			Name:         "janua",
			K8sNamespace: &januaNamespace,
		}
	}

	tests := []struct {
		name string
		// cluster contents; nil means "use the real janua namespace".
		services []*corev1.Service
		// kube overrides services entirely when non-nil.
		kube *k8s.Client
		// seedRule, when non-empty, is the ingress rule already in place.
		seedRule string
		// listFails makes the tunnel config unreadable, so whether this
		// hostname is currently serving is unknown.
		listFails bool
		// service + port describe the write being attempted.
		service *types.Service
		port    int
		// canaryEnabled runs the post-write probe. The probe dials the spec's
		// cluster DNS name, which no unit test can serve, so cases that assert
		// a rule SURVIVES leave it off and the probe/revert behaviour is
		// covered separately by TestCanaryTunnelRoute_RevertsOnDialFailure and
		// TestProbeTunnelBackend_AnyHTTPResponseIsAPass.
		canaryEnabled bool

		wantBackend  string // "" means no rule at all
		wantWrites   int
		wantErrorLog string
	}{
		{
			// (a) THE OUTAGE. A working rule exists; the manifest asks to
			// repoint it at a Service that does not exist. Nothing is written,
			// the incumbent survives, and the refusal is logged at Error with
			// the backend it declined to destroy.
			name:         "refuses to rewrite a working rule onto a nonexistent service",
			seedRule:     workingRule,
			service:      januaService(),
			port:         8080,
			wantBackend:  workingRule,
			wantWrites:   0,
			wantErrorLog: "REFUSED to rewrite a live tunnel ingress rule",
		},
		{
			// (b) Same unresolvable spec, but nothing is routing this hostname
			// yet. Still refused — a rule pointing at a black hole is worse
			// than no rule, which falls through to the tunnel's catch-all.
			name:         "refuses to add a first rule for a nonexistent service",
			service:      januaService(),
			port:         8080,
			wantBackend:  "",
			wantWrites:   0,
			wantErrorLog: "Refusing to add a tunnel ingress rule for a backend that does not exist",
		},
		{
			// (c) The manifest janua should have had. janua-api on 80 resolves,
			// so the gate lets the write through.
			name: "writes a resolvable backend",
			services: []*corev1.Service{
				k8sService("janua", "janua-api", 80),
			},
			service: &types.Service{
				ID: uuid.New(), ProjectID: uuid.New(),
				Name: "janua-api", K8sNamespace: &januaNamespace,
			},
			port:        80,
			wantBackend: workingRule,
			wantWrites:  1,
		},
		{
			// (c') Same write, canary ON. The cluster DNS name is unreachable
			// from a unit test, so the probe fails and the rule it just added
			// is withdrawn — which is the guard behaving correctly, and proves
			// the canary is wired into the write path rather than only
			// unit-tested in isolation.
			name: "canary withdraws a written rule whose backend answers nothing",
			services: []*corev1.Service{
				k8sService("janua", "janua-api", 80),
			},
			service: &types.Service{
				ID: uuid.New(), ProjectID: uuid.New(),
				Name: "janua-api", K8sNamespace: &januaNamespace,
			},
			port:          80,
			canaryEnabled: true,
			wantBackend:   "",
			wantWrites:    1,
			wantErrorLog:  "newly added rule withdrawn",
		},
		{
			// (e) The transient branch on the replace side. A Service Get that
			// times out is not an answer, and for a hostname that is currently
			// serving it must be treated as a refusal.
			name:         "fails safe when the backend check is inconclusive and a rule exists",
			kube:         failingServiceGetKube(),
			seedRule:     workingRule,
			service:      januaService(),
			port:         8080,
			wantBackend:  workingRule,
			wantWrites:   0,
			wantErrorLog: "REFUSED to rewrite a live tunnel ingress rule",
		},
		{
			// The transient branch on the add side: nothing is at risk, so the
			// rule is deferred rather than refused outright, and the next
			// reconciliation pass asks the API server again. Still no write.
			name:        "defers a new rule when the backend check is inconclusive",
			kube:        failingServiceGetKube(),
			service:     januaService(),
			port:        8080,
			wantBackend: "",
			wantWrites:  0,
		},
		{
			// A Service that exists under the right name but on a different
			// port is the other half of the janua spec being wrong. Port 8080
			// against a Service exposing only 80 is not resolvable.
			name: "refuses a service that exists but does not expose the port",
			services: []*corev1.Service{
				k8sService("janua", "janua", 80),
			},
			seedRule:     workingRule,
			service:      januaService(),
			port:         8080,
			wantBackend:  workingRule,
			wantWrites:   0,
			wantErrorLog: "REFUSED to rewrite a live tunnel ingress rule",
		},
		{
			// The tunnel config could not be read, so whether this hostname is
			// currently serving is unknown — and not knowing is not a licence
			// to write. Treated as a replacement, refused loudly.
			name:         "treats an unreadable tunnel config as a live rule worth protecting",
			listFails:    true,
			service:      januaService(),
			port:         8080,
			wantBackend:  "",
			wantWrites:   0,
			wantErrorLog: "REFUSED to rewrite a live tunnel ingress rule",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Guards the table against itself: no case may ever declare the
			// dead backend as the wanted outcome. That expectation IS the
			// outage, and writing it here is the easiest way to "fix" a
			// failing guard without noticing what was given up.
			if tt.wantBackend == deadBackend {
				t.Fatalf("case expects the dead backend %q to be written; that is the outage", deadBackend)
			}

			tunnel := newGuardedTunnelRoutes()
			if tt.seedRule != "" {
				tunnel.seed(hostname, tt.seedRule)
			}
			tunnel.listFails = tt.listFails

			kube := tt.kube
			if kube == nil {
				services := tt.services
				if services == nil {
					services = januaClusterServices()
				}
				kube = fakeKubeWithServices(services...)
			}

			logger := &recordingLogger{}
			h := &Handler{
				tunnelRoutesService: tunnel,
				logger:              logger,
				k8sClient:           kube,
				config:              &config.Config{TunnelRouteCanaryEnabled: tt.canaryEnabled},
			}
			if tt.canaryEnabled {
				h.tunnelCanaryProbe = deadBackendProbe
			}

			h.ensureTunnelRoute(context.Background(), hostname, tt.service, "production", tt.port, nil)

			if got := tunnel.backend(hostname); got != tt.wantBackend {
				t.Fatalf("ingress rule for %s = %q, want %q", hostname, got, tt.wantBackend)
			}
			if tunnel.adds != tt.wantWrites {
				t.Fatalf("AddRoute called %d times, want %d", tunnel.adds, tt.wantWrites)
			}
			if tt.wantErrorLog != "" && !logger.sawError(tt.wantErrorLog) {
				t.Fatalf("no Error log containing %q; errors=%v warns=%v",
					tt.wantErrorLog, logger.errors, logger.warns)
			}
		})
	}
}

// TestCanaryTunnelRoute_RevertsOnDialFailure is case (d): a rule was written,
// the backend answers nothing, and the previous rule comes back.
//
// The dial is stubbed out (deadBackendProbe) so these cases test the reaction
// to a failed probe rather than the probe itself; the accept-any-HTTP rule and
// the dial-failure rule are tested directly against a real listener in
// TestProbeTunnelBackend_AnyHTTPResponseIsAPass.
func TestCanaryTunnelRoute_RevertsOnDialFailure(t *testing.T) {
	const hostname = "auth.madfam.io"

	// The outage's own backend: the rule that was just written, which nothing
	// answers at.
	deadSpec := &route.RouteSpec{
		Hostname:         hostname,
		ServiceName:      "janua",
		ServiceNamespace: "janua",
		ServicePort:      8080,
	}

	t.Run("update reverts to the previous rule", func(t *testing.T) {
		tunnel := newGuardedTunnelRoutes()
		previous := &route.IngressRule{
			Hostname: hostname,
			Service:  "http://janua-api.janua.svc.cluster.local:80",
		}
		tunnel.seed(hostname, previous.Service)

		logger := &recordingLogger{}
		h := &Handler{
			tunnelRoutesService: tunnel,
			logger:              logger,
			config:              &config.Config{TunnelRouteCanaryEnabled: true},
			tunnelCanaryProbe:   deadBackendProbe,
		}

		h.canaryTunnelRoute(context.Background(), deadSpec, previous, true, nil)

		if got := tunnel.backend(hostname); got != previous.Service {
			t.Fatalf("after revert, rule = %q, want %q", got, previous.Service)
		}
		if tunnel.adds != 1 {
			t.Fatalf("AddRoute called %d times during revert, want 1", tunnel.adds)
		}
		if !logger.sawError("canary failed, rule reverted") {
			t.Fatalf("revert not logged at Error; errors=%v", logger.errors)
		}
	})

	t.Run("fresh add is withdrawn", func(t *testing.T) {
		tunnel := newGuardedTunnelRoutes()
		tunnel.seed(hostname, tunnelRouteServiceURL(deadSpec))

		logger := &recordingLogger{}
		h := &Handler{
			tunnelRoutesService: tunnel,
			logger:              logger,
			config:              &config.Config{TunnelRouteCanaryEnabled: true},
			tunnelCanaryProbe:   deadBackendProbe,
		}

		h.canaryTunnelRoute(context.Background(), deadSpec, nil, false, nil)

		if got := tunnel.backend(hostname); got != "" {
			t.Fatalf("rule survived withdrawal: %q", got)
		}
		if tunnel.removes != 1 {
			t.Fatalf("RemoveRoute called %d times, want 1", tunnel.removes)
		}
		if !logger.sawError("newly added rule withdrawn") {
			t.Fatalf("withdrawal not logged at Error; errors=%v", logger.errors)
		}
	})

	t.Run("a previous rule that cannot be rebuilt is left alone and shouted about", func(t *testing.T) {
		tunnel := newGuardedTunnelRoutes()
		// An external origin: not an in-cluster service URL, so no RouteSpec
		// can be reconstructed from it.
		previous := &route.IngressRule{Hostname: hostname, Service: "https://origin.example.com"}
		tunnel.seed(hostname, tunnelRouteServiceURL(deadSpec))

		logger := &recordingLogger{}
		h := &Handler{
			tunnelRoutesService: tunnel,
			logger:              logger,
			config:              &config.Config{TunnelRouteCanaryEnabled: true},
			tunnelCanaryProbe:   deadBackendProbe,
		}

		h.canaryTunnelRoute(context.Background(), deadSpec, previous, true, nil)

		if tunnel.adds != 0 || tunnel.removes != 0 {
			t.Fatalf("unrebuildable previous rule provoked a write: adds=%d removes=%d",
				tunnel.adds, tunnel.removes)
		}
		if !logger.sawError("could not be rebuilt") {
			t.Fatalf("unrebuildable revert not logged at Error; errors=%v", logger.errors)
		}
	})

	t.Run("disabled canary writes nothing", func(t *testing.T) {
		tunnel := newGuardedTunnelRoutes()
		h := &Handler{
			tunnelRoutesService: tunnel,
			logger:              newNopLogger(),
			config:              &config.Config{TunnelRouteCanaryEnabled: false},
		}

		h.canaryTunnelRoute(context.Background(), deadSpec, nil, false, nil)

		if tunnel.adds != 0 || tunnel.removes != 0 {
			t.Fatalf("disabled canary acted: adds=%d removes=%d", tunnel.adds, tunnel.removes)
		}
	})
}

func TestProbeTunnelBackend_AnyHTTPResponseIsAPass(t *testing.T) {
	// A 404 proves a listener. Reading the status instead of the dial is how a
	// perfectly good rule whose backend 404s on `/` gets reverted.
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }))
	defer srv.Close()

	h := &Handler{logger: newNopLogger(), config: &config.Config{}}

	// tunnelRouteServiceURL always renders a cluster DNS name, so the probe's
	// mechanics are exercised through probeURL against the test server.
	if err := h.probeURL(context.Background(), srv.URL); err != nil {
		t.Fatalf("a 404 from a live listener must pass the canary, got %v", err)
	}

	// A closed port is the only kind of failure that counts.
	if err := h.probeURL(context.Background(), "http://127.0.0.1:1"); err == nil {
		t.Fatal("a dial failure must fail the canary")
	}
}

func TestResolveTunnelBackend(t *testing.T) {
	ns := "janua"
	h := &Handler{
		logger:    newNopLogger(),
		config:    &config.Config{},
		k8sClient: fakeKubeWithServices(januaClusterServices()...),
	}

	if err := h.resolveTunnelBackend(context.Background(), &route.RouteSpec{
		ServiceName: "janua-api", ServiceNamespace: ns, ServicePort: 80,
	}); err != nil {
		t.Fatalf("janua-api:80 exists and must resolve, got %v", err)
	}

	err := h.resolveTunnelBackend(context.Background(), &route.RouteSpec{
		ServiceName: "janua", ServiceNamespace: ns, ServicePort: 8080,
	})
	if err == nil {
		t.Fatal("the outage spec (janua:8080) must not resolve")
	}
	if errors.Is(err, errTunnelBackendUnknown) {
		t.Fatalf("a NotFound is a definite answer, not an inconclusive one: %v", err)
	}

	err = h.resolveTunnelBackend(context.Background(), &route.RouteSpec{
		ServiceName: "janua-api", ServiceNamespace: ns, ServicePort: 8080,
	})
	if err == nil {
		t.Fatal("janua-api does not expose 8080; that must not resolve")
	}
	if !strings.Contains(err.Error(), "exposes 80") {
		t.Fatalf("the error should name the ports that ARE exposed, got %v", err)
	}

	// No client at all is inconclusive, never a pass.
	bare := &Handler{logger: newNopLogger(), config: &config.Config{}}
	err = bare.resolveTunnelBackend(context.Background(), &route.RouteSpec{
		ServiceName: "janua-api", ServiceNamespace: ns, ServicePort: 80,
	})
	if !errors.Is(err, errTunnelBackendUnknown) {
		t.Fatalf("a missing Kubernetes client must be inconclusive, got %v", err)
	}

	// A transient API error is inconclusive too.
	transient := &Handler{logger: newNopLogger(), config: &config.Config{}, k8sClient: failingServiceGetKube()}
	err = transient.resolveTunnelBackend(context.Background(), &route.RouteSpec{
		ServiceName: "janua-api", ServiceNamespace: ns, ServicePort: 80,
	})
	if !errors.Is(err, errTunnelBackendUnknown) {
		t.Fatalf("a timed-out Service Get must be inconclusive, got %v", err)
	}
}

func TestRouteSpecFromIngressRule(t *testing.T) {
	spec, err := routeSpecFromIngressRule(&route.IngressRule{
		Hostname: "auth.madfam.io",
		Service:  "http://janua-api.janua.svc.cluster.local:80",
	})
	if err != nil {
		t.Fatalf("an in-cluster URL must rebuild, got %v", err)
	}
	if spec.ServiceName != "janua-api" || spec.ServiceNamespace != "janua" || spec.ServicePort != 80 {
		t.Fatalf("rebuilt spec = %+v", spec)
	}

	if _, err := routeSpecFromIngressRule(&route.IngressRule{
		Hostname: "auth.madfam.io", Service: "https://origin.example.com",
	}); err == nil {
		t.Fatal("an external origin must not rebuild into a RouteSpec")
	}
}
