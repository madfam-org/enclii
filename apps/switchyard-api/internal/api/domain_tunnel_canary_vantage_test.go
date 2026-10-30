package api

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	route "github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
)

const (
	vantageTenantNS  = "tenant-a"
	vantageProberNS  = "control-plane"
	vantageTunnelNS  = "tunnel-edge"
	vantageHostname  = "api.example.com"
	vantageServiceID = "example-api"
)

// withProberNamespace pins the namespace the canary believes it probes from,
// and points the service-account fallback at a file that does not exist so a
// test run inside a cluster pod reads the same answer as one on a laptop.
func withProberNamespace(t *testing.T, namespace string) {
	t.Helper()
	t.Setenv("POD_NAMESPACE", namespace)
	previous := serviceAccountNamespaceFile
	serviceAccountNamespaceFile = filepath.Join(t.TempDir(), "absent-namespace")
	t.Cleanup(func() { serviceAccountNamespaceFile = previous })
}

// withFastRevertRetries shrinks the revert read-back backoff.
func withFastRevertRetries(t *testing.T, retries int) {
	t.Helper()
	previous := tunnelRevertRetryDelays
	delays := make([]time.Duration, retries)
	for i := range delays {
		delays[i] = time.Millisecond
	}
	tunnelRevertRetryDelays = delays
	t.Cleanup(func() { tunnelRevertRetryDelays = previous })
}

func selectorService(namespace, name string, port int32, selector map[string]string) *corev1.Service {
	svc := k8sService(namespace, name, port)
	svc.Spec.Selector = selector
	return svc
}

func backendPod(namespace, name string, podLabels map[string]string, ready bool) *corev1.Pod {
	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: podLabels},
		Status: corev1.PodStatus{Conditions: []corev1.PodCondition{
			{Type: corev1.PodReady, Status: status},
		}},
	}
}

// tunnelOnlyIngressPolicy is the tenant default: every pod isolated for
// ingress, admitting only the tunnel's namespace.
func tunnelOnlyIngressPolicy(namespace string) *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "allow-tunnel-ingress", Namespace: namespace},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{{
				From: []networkingv1.NetworkPolicyPeer{{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"kubernetes.io/metadata.name": vantageTunnelNS},
					},
				}},
			}},
		},
	}
}

func fakeKubeWithObjects(objects ...runtime.Object) *k8s.Client {
	return &k8s.Client{KubeClient: fake.NewSimpleClientset(objects...)}
}

// A healthy backend behind the tenant default NetworkPolicy, and the web
// app's rule sitting in the tunnel before a write repoints the hostname at the
// API. The canary's probe from switchyard's namespace cannot get through.
func vantageFixture(extra ...runtime.Object) (*k8s.Client, *route.RouteSpec, *route.IngressRule) {
	objects := []runtime.Object{
		selectorService(vantageTenantNS, vantageServiceID, 80, map[string]string{"app": vantageServiceID}),
		backendPod(vantageTenantNS, "example-api-0", map[string]string{"app": vantageServiceID}, true),
	}
	objects = append(objects, extra...)
	spec := &route.RouteSpec{
		Hostname:         vantageHostname,
		ServiceName:      vantageServiceID,
		ServiceNamespace: vantageTenantNS,
		ServicePort:      80,
	}
	previous := &route.IngressRule{
		Hostname: vantageHostname,
		Service:  "http://example-web." + vantageTenantNS + ".svc.cluster.local:80",
	}
	return fakeKubeWithObjects(objects...), spec, previous
}

func vantageHandler(kube *k8s.Client, tunnel route.TunnelRoutesManager, logger *recordingLogger) *Handler {
	return &Handler{
		tunnelRoutesService: tunnel,
		logger:              logger,
		k8sClient:           kube,
		config:              &config.Config{TunnelRouteCanaryEnabled: true},
		tunnelCanaryProbe:   deadBackendProbe,
	}
}

// The production failure: a probe NetworkPolicy refuses is not evidence the
// backend is broken. The written rule stays, nothing is reverted, and the
// decision is logged as a warning.
func TestCanaryTunnelRoute_NetworkPolicyBlockedProbeDoesNotRevert(t *testing.T) {
	withProberNamespace(t, vantageProberNS)
	kube, spec, previous := vantageFixture(tunnelOnlyIngressPolicy(vantageTenantNS))

	t.Run("replacement is kept", func(t *testing.T) {
		tunnel := newGuardedTunnelRoutes()
		tunnel.seed(vantageHostname, tunnelRouteServiceURL(spec))
		logger := &recordingLogger{}

		vantageHandler(kube, tunnel, logger).canaryTunnelRoute(context.Background(), spec, previous, true, nil)

		if got := tunnel.backend(vantageHostname); got != tunnelRouteServiceURL(spec) {
			t.Fatalf("rule = %q, want the written rule %q kept", got, tunnelRouteServiceURL(spec))
		}
		if tunnel.adds != 0 || tunnel.removes != 0 {
			t.Fatalf("an inconclusive probe must write nothing: adds=%d removes=%d", tunnel.adds, tunnel.removes)
		}
		if len(logger.errors) != 0 {
			t.Fatalf("an inconclusive probe is not an error: %v", logger.errors)
		}
		warned := false
		for _, msg := range logger.warns {
			if strings.Contains(msg, "INCONCLUSIVE") {
				warned = true
			}
		}
		if !warned {
			t.Fatalf("the kept rule must be logged at warning; warns=%v", logger.warns)
		}
	})

	t.Run("fresh add is kept", func(t *testing.T) {
		tunnel := newGuardedTunnelRoutes()
		tunnel.seed(vantageHostname, tunnelRouteServiceURL(spec))

		vantageHandler(kube, tunnel, &recordingLogger{}).canaryTunnelRoute(context.Background(), spec, nil, false, nil)

		if tunnel.removes != 0 || tunnel.backend(vantageHostname) == "" {
			t.Fatalf("a first-time add behind NetworkPolicy must not be withdrawn: removes=%d", tunnel.removes)
		}
	})
}

// Everything the vantage check cannot positively establish keeps the old
// behaviour: revert.
func TestCanaryTunnelRoute_VantageCheckFallsBackToRevert(t *testing.T) {
	admitsProber := tunnelOnlyIngressPolicy(vantageTenantNS)
	admitsProber.Name = "allow-control-plane"
	admitsProber.Spec.Ingress[0].From[0].NamespaceSelector.MatchLabels = map[string]string{
		"kubernetes.io/metadata.name": vantageProberNS,
	}

	allSources := tunnelOnlyIngressPolicy(vantageTenantNS)
	allSources.Spec.Ingress[0].From = nil

	ipBlock := tunnelOnlyIngressPolicy(vantageTenantNS)
	ipBlock.Spec.Ingress[0].From = []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: "10.0.0.0/8"}}}

	egressOnly := tunnelOnlyIngressPolicy(vantageTenantNS)
	egressOnly.Spec.PolicyTypes = []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}

	otherPods := tunnelOnlyIngressPolicy(vantageTenantNS)
	otherPods.Spec.PodSelector = metav1.LabelSelector{MatchLabels: map[string]string{"app": "something-else"}}

	tests := []struct {
		name     string
		prober   string
		kube     func() *k8s.Client
		noClient bool
	}{
		{
			name:   "no NetworkPolicy at all",
			prober: vantageProberNS,
			kube:   func() *k8s.Client { k, _, _ := vantageFixture(); return k },
		},
		{
			name:   "a policy admits the prober's namespace",
			prober: vantageProberNS,
			kube: func() *k8s.Client {
				k, _, _ := vantageFixture(tunnelOnlyIngressPolicy(vantageTenantNS), admitsProber)
				return k
			},
		},
		{
			name:   "a rule with no from admits every source",
			prober: vantageProberNS,
			kube:   func() *k8s.Client { k, _, _ := vantageFixture(allSources); return k },
		},
		{
			name:   "an ipBlock peer cannot be evaluated",
			prober: vantageProberNS,
			kube:   func() *k8s.Client { k, _, _ := vantageFixture(ipBlock); return k },
		},
		{
			name:   "an egress-only policy does not isolate ingress",
			prober: vantageProberNS,
			kube:   func() *k8s.Client { k, _, _ := vantageFixture(egressOnly); return k },
		},
		{
			name:   "the policy selects other pods",
			prober: vantageProberNS,
			kube:   func() *k8s.Client { k, _, _ := vantageFixture(otherPods); return k },
		},
		{
			name:   "own namespace unknown",
			prober: "",
			kube: func() *k8s.Client {
				k, _, _ := vantageFixture(tunnelOnlyIngressPolicy(vantageTenantNS))
				return k
			},
		},
		{
			name:   "no Ready pod: broken from every vantage",
			prober: vantageProberNS,
			kube: func() *k8s.Client {
				return fakeKubeWithObjects(
					selectorService(vantageTenantNS, vantageServiceID, 80, map[string]string{"app": vantageServiceID}),
					backendPod(vantageTenantNS, "example-api-0", map[string]string{"app": vantageServiceID}, false),
					tunnelOnlyIngressPolicy(vantageTenantNS),
				)
			},
		},
		{
			name:     "no Kubernetes client",
			prober:   vantageProberNS,
			noClient: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withProberNamespace(t, tt.prober)
			withFastRevertRetries(t, 2)
			_, spec, previous := vantageFixture()

			var kube *k8s.Client
			if !tt.noClient {
				kube = tt.kube()
			}
			tunnel := newGuardedTunnelRoutes()
			tunnel.seed(vantageHostname, tunnelRouteServiceURL(spec))
			logger := &recordingLogger{}

			vantageHandler(kube, tunnel, logger).canaryTunnelRoute(context.Background(), spec, previous, true, nil)

			if got := tunnel.backend(vantageHostname); got != previous.Service {
				t.Fatalf("rule = %q, want reverted to %q", got, previous.Service)
			}
			if !logger.sawError("canary failed, rule reverted") {
				t.Fatalf("revert not logged at Error; errors=%v warns=%v", logger.errors, logger.warns)
			}
		})
	}
}

// lossyTunnelRoutes is guardedTunnelRoutes whose first `lose` writes are
// acknowledged and then clobbered, the way a concurrent whole-config write
// that read before them and wrote after them would.
type lossyTunnelRoutes struct {
	*guardedTunnelRoutes
	lose      int
	failFirst bool
}

func (m *lossyTunnelRoutes) AddRoute(ctx context.Context, spec *route.RouteSpec) error {
	if m.failFirst {
		m.failFirst = false
		m.adds++
		return errors.New("cloudflare: 503")
	}
	before, had := m.rules[strings.ToLower(spec.Hostname)]
	if err := m.guardedTunnelRoutes.AddRoute(ctx, spec); err != nil {
		return err
	}
	if m.lose > 0 {
		m.lose--
		if had {
			m.rules[strings.ToLower(spec.Hostname)] = before
		} else {
			delete(m.rules, strings.ToLower(spec.Hostname))
		}
	}
	return nil
}

func (m *lossyTunnelRoutes) RemoveRoute(ctx context.Context, hostname string) error {
	before, had := m.rules[strings.ToLower(hostname)]
	if err := m.guardedTunnelRoutes.RemoveRoute(ctx, hostname); err != nil {
		return err
	}
	if m.lose > 0 && had {
		m.lose--
		m.rules[strings.ToLower(hostname)] = before
	}
	return nil
}

// The lost revert: the revert PUT is accepted and then overwritten. It is read
// back, found missing, and re-applied until it sticks.
func TestCanaryTunnelRoute_RevertIsConfirmedAndReapplied(t *testing.T) {
	withProberNamespace(t, "")
	withFastRevertRetries(t, 3)

	_, spec, previous := vantageFixture()

	t.Run("a revert clobbered once is re-applied", func(t *testing.T) {
		tunnel := &lossyTunnelRoutes{guardedTunnelRoutes: newGuardedTunnelRoutes(), lose: 1}
		tunnel.seed(vantageHostname, tunnelRouteServiceURL(spec))
		logger := &recordingLogger{}

		vantageHandler(nil, tunnel, logger).canaryTunnelRoute(context.Background(), spec, previous, true, nil)

		if got := tunnel.backend(vantageHostname); got != previous.Service {
			t.Fatalf("rule = %q after confirmation, want %q", got, previous.Service)
		}
		if tunnel.adds != 2 {
			t.Fatalf("AddRoute called %d times, want 2 (revert + one re-application)", tunnel.adds)
		}
		if logger.sawError("did NOT land") {
			t.Fatalf("a revert that landed on retry must not be reported as failed: %v", logger.errors)
		}
	})

	t.Run("a revert whose first write errors is retried", func(t *testing.T) {
		tunnel := &lossyTunnelRoutes{guardedTunnelRoutes: newGuardedTunnelRoutes(), failFirst: true}
		tunnel.seed(vantageHostname, tunnelRouteServiceURL(spec))

		vantageHandler(nil, tunnel, &recordingLogger{}).canaryTunnelRoute(context.Background(), spec, previous, true, nil)

		if got := tunnel.backend(vantageHostname); got != previous.Service {
			t.Fatalf("rule = %q, want %q", got, previous.Service)
		}
	})

	t.Run("a revert that never lands is reported loudly", func(t *testing.T) {
		tunnel := &lossyTunnelRoutes{guardedTunnelRoutes: newGuardedTunnelRoutes(), lose: 100}
		tunnel.seed(vantageHostname, tunnelRouteServiceURL(spec))
		logger := &recordingLogger{}

		vantageHandler(nil, tunnel, logger).canaryTunnelRoute(context.Background(), spec, previous, true, nil)

		if tunnel.adds != 1+len(tunnelRevertRetryDelays) {
			t.Fatalf("AddRoute called %d times, want %d (bounded retries)", tunnel.adds, 1+len(tunnelRevertRetryDelays))
		}
		if !logger.sawError("did NOT land") {
			t.Fatalf("a revert that never landed must be an Error; errors=%v", logger.errors)
		}
	})

	t.Run("a withdrawal clobbered once is re-applied", func(t *testing.T) {
		tunnel := &lossyTunnelRoutes{guardedTunnelRoutes: newGuardedTunnelRoutes(), lose: 1}
		tunnel.seed(vantageHostname, tunnelRouteServiceURL(spec))
		logger := &recordingLogger{}

		vantageHandler(nil, tunnel, logger).canaryTunnelRoute(context.Background(), spec, nil, false, nil)

		if got := tunnel.backend(vantageHostname); got != "" {
			t.Fatalf("withdrawn rule came back: %q", got)
		}
		if tunnel.removes != 2 {
			t.Fatalf("RemoveRoute called %d times, want 2", tunnel.removes)
		}
		if !logger.sawError("newly added rule withdrawn") {
			t.Fatalf("withdrawal not logged; errors=%v", logger.errors)
		}
	})

	t.Run("an unreadable tunnel config is not taken as confirmation", func(t *testing.T) {
		tunnel := &lossyTunnelRoutes{guardedTunnelRoutes: newGuardedTunnelRoutes()}
		tunnel.seed(vantageHostname, tunnelRouteServiceURL(spec))
		tunnel.listFails = true
		logger := &recordingLogger{}

		vantageHandler(nil, tunnel, logger).canaryTunnelRoute(context.Background(), spec, previous, true, nil)

		if !logger.sawError("did NOT land") {
			t.Fatalf("an unconfirmable revert must be reported; errors=%v", logger.errors)
		}
	})
}
