package api

// Whether a tunnel ingress rule's backend actually exists, and whether it
// actually serves.
//
// On 2026-08-27 at 16:23Z the domain provisioner captured janua's legacy
// single-doc enclii.yaml — `metadata.name: janua`, `runtime.port: 8080`, eight
// declared domains — and rewrote the tunnel's hand-set ingress rules onto
// `http://janua.janua.svc.cluster.local:8080`. No Service named `janua` has
// ever existed in that namespace; the real backends are janua-api, janua-admin,
// janua-dashboard and janua-website, all on port 80. Seven config versions in
// 37 seconds (v212→v218), one hostname each, and every SSO surface in the
// ecosystem went dark. Nothing checked the backend before writing, nothing
// checked the route after writing, and nothing noticed for hours.
//
// Two checks close that, and they are deliberately different in kind:
//
//   - resolveTunnelBackend is a CONTROL-PLANE check. It asks the API server
//     whether a Service of that name, in that namespace, exposes that port. It
//     runs BEFORE the write, and it is the check that would have refused the
//     janua rewrite outright.
//   - probeTunnelBackend is a DATA-PLANE check. It dials the URL cloudflared
//     will dial, from the same pod network, AFTER the write. It catches the
//     cases the control plane cannot see: a Service that exists but selects no
//     ready pods, a port that is declared but not listening.
//
// Neither subsumes the other. A Service can exist and serve nothing; a route
// can serve and still point at the wrong workload. The gate is the one that
// stops the outage class; the probe is what turns "we wrote something wrong"
// into "we wrote something wrong and put it back".

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
)

const (
	// tunnelCanaryTimeout bounds ONE probe attempt. Short on purpose: the
	// probe only has to prove a listener answers, and a reconciliation loop
	// may walk dozens of domains. Two attempts at 2s is the whole budget per
	// written rule.
	tunnelCanaryTimeout = 2 * time.Second
	// tunnelCanaryAttempts covers a Service whose endpoints are still
	// converging in the second after a write. One retry, not a poll loop.
	tunnelCanaryAttempts = 2
	// tunnelCanaryRetryDelay separates the two attempts.
	tunnelCanaryRetryDelay = 250 * time.Millisecond
)

// errTunnelBackendUnknown marks a resolution that could not be completed — a
// transient API-server failure, RBAC, no client at all — as opposed to one that
// completed and said "no". The distinction is load-bearing: "we could not
// check" must never be treated as "we checked and it was fine", and for a
// hostname that already has a working rule it must be treated as a refusal.
var errTunnelBackendUnknown = errors.New("tunnel backend resolution inconclusive")

// resolveTunnelBackend reports whether spec names a Service that exists in its
// namespace and exposes its port.
//
// Return contract:
//
//	nil                                    — resolved, and the backend is real.
//	err wrapping errTunnelBackendUnknown   — the check itself did not complete.
//	any other err                          — resolved, and the backend is not
//	                                         there (NotFound, or the port is
//	                                         not among svc.Spec.Ports).
//
// Callers branch on errors.Is(err, errTunnelBackendUnknown) to decide whether a
// failure is a definite "no" or an "ask again later"; the two get different
// treatment for a hostname that already routes.
func (h *Handler) resolveTunnelBackend(ctx context.Context, spec *services.RouteSpec) error {
	if spec == nil {
		return fmt.Errorf("%w: no route spec", errTunnelBackendUnknown)
	}
	if spec.ServiceName == "" || spec.ServiceNamespace == "" {
		return fmt.Errorf("route names no backend: service=%q namespace=%q",
			spec.ServiceName, spec.ServiceNamespace)
	}

	// Kube() over the concrete Clientset field so this path is unit-testable
	// against a fake client, the accessor contract #463 established. IsValid
	// additionally wants a REST config, which a fake has no use for, so the
	// usable-client test is the nil check on Kube() itself.
	if h == nil || h.k8sClient == nil || h.k8sClient.Kube() == nil {
		return fmt.Errorf("%w: no Kubernetes client is configured, so %s.%s cannot be verified",
			errTunnelBackendUnknown, spec.ServiceName, spec.ServiceNamespace)
	}

	svc, err := h.k8sClient.Kube().CoreV1().
		Services(spec.ServiceNamespace).
		Get(ctx, spec.ServiceName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("no Service %q exists in namespace %q",
				spec.ServiceName, spec.ServiceNamespace)
		}
		// RBAC, timeout, connection refused: the cluster did not answer the
		// question. Not an answer of "no".
		return fmt.Errorf("%w: could not read Service %s/%s: %v",
			errTunnelBackendUnknown, spec.ServiceNamespace, spec.ServiceName, err)
	}

	for _, port := range svc.Spec.Ports {
		if int(port.Port) == spec.ServicePort {
			return nil
		}
	}

	return fmt.Errorf("Service %s/%s exists but does not expose port %d (exposes %s)",
		spec.ServiceNamespace, spec.ServiceName, spec.ServicePort, describeServicePorts(svc.Spec.Ports))
}

// describeServicePorts renders the ports a Service actually exposes, so the
// error an operator reads names the fix rather than only the failure. For the
// janua outage this is the difference between "port 8080 not exposed" and
// "port 8080 not exposed (exposes 80)".
func describeServicePorts(ports []corev1.ServicePort) string {
	if len(ports) == 0 {
		return "none"
	}
	out := ""
	for i, port := range ports {
		if i > 0 {
			out += ", "
		}
		out += fmt.Sprintf("%d", port.Port)
	}
	return out
}

// probeTunnelBackend dials the exact URL cloudflared will dial, from
// switchyard's own pod network.
//
// ANY HTTP response is a pass, status irrelevant: a 404, a 401, a 500 all prove
// a listener accepted the connection and spoke HTTP, which is the entire
// question. Only a dial or DNS failure — nothing is listening there — is a
// failure. Reading the status code instead would revert a perfectly good rule
// whose backend happens to 404 on `/`.
func (h *Handler) probeTunnelBackend(ctx context.Context, spec *services.RouteSpec) error {
	if spec == nil {
		return errors.New("no route spec to probe")
	}
	return h.probeURL(ctx, tunnelRouteServiceURL(spec))
}

// probeBackend is the seam the canary calls. It is probeTunnelBackend unless a
// test has substituted something — the revert logic has to be exercisable
// without a cluster DNS name that resolves nowhere and costs seconds to fail.
func (h *Handler) probeBackend(ctx context.Context, spec *services.RouteSpec) error {
	if h != nil && h.tunnelCanaryProbe != nil {
		return h.tunnelCanaryProbe(ctx, spec)
	}
	return h.probeTunnelBackend(ctx, spec)
}

// probeURL is probeTunnelBackend's mechanics, split out so the accept-any-HTTP
// rule can be exercised against a real listener rather than a cluster DNS name.
func (h *Handler) probeURL(ctx context.Context, url string) error {
	client := &http.Client{Timeout: tunnelCanaryTimeout}
	defer client.CloseIdleConnections()

	var lastErr error
	for attempt := 0; attempt < tunnelCanaryAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("canary probe of %s abandoned: %w", url, ctx.Err())
			case <-time.After(tunnelCanaryRetryDelay):
			}
		}

		attemptCtx, cancel := context.WithTimeout(ctx, tunnelCanaryTimeout)
		req, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, url+"/", nil)
		if err != nil {
			cancel()
			return fmt.Errorf("canary probe of %s could not be built: %w", url, err)
		}

		resp, err := client.Do(req)
		if err == nil {
			// Drained and closed; the body is of no interest, only that a
			// listener produced one.
			_ = resp.Body.Close()
			cancel()
			return nil
		}
		cancel()
		lastErr = err
	}

	return fmt.Errorf("nothing answered at %s after %d attempts: %w",
		url, tunnelCanaryAttempts, lastErr)
}

// canaryEnabled reports whether the post-write probe runs. Default ON — the
// flag exists as a break-glass switch and so unit tests can write rules without
// dialing anything. Mirrors ENCLII_TIMETABLE_RECONCILER_ENABLED /
// ENCLII_PGBOUNCER_DRIFT_CHECK_ENABLED: a safety loop that ships on and can be
// turned off deliberately, never one that ships off and has to be turned on.
func (h *Handler) canaryEnabled() bool {
	if h == nil || h.config == nil {
		return false
	}
	return h.config.TunnelRouteCanaryEnabled
}
