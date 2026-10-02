package api

// The repoint guard: a tunnel-route reconcile may fix drift, but it may never
// move a hostname off a workload that is serving it.
//
// On 2026-10-01 `tunnels-apply --project <p> --apply` planned five UPDATEs and
// executed all of them. Two production hostnames (the API and the admin) were
// moved off their own healthy services onto the project's web service, and two
// staging hostnames were moved off the staging workloads onto the PRODUCTION
// web service. The plan called all five "update: live route targets different
// backend". None of them was drift: every live route was right and every
// junction was wrong (all bound to the web service, and none of them to an
// environment).
//
// An UPDATE that changes the target SERVICE NAME or NAMESPACE is therefore not
// a drift fix. It is a repoint, and it is refused unless:
//
//   - the live backend is provably not serving (no such Service, the port is
//     not exposed, or no Ready pod behind it): replacing a dead route IS the
//     drift fix this command exists for; or
//   - the operator names that one hostname with --allow-repoint.
//
// "Could not tell" is treated as serving. Not knowing is never permission to
// move live traffic.
//
// Independently of health, a route never crosses between production and
// another environment by inference: a live route in a staging namespace is
// not moved to the production namespace (or the reverse) because a junction
// happens to have no environment recorded.

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// Plan row actions and labels. The action is the machine-readable verb; the
// label is what an operator reads in the plan table.
const (
	tunnelActionCreate  = "create"
	tunnelActionUpdate  = "update"
	tunnelActionSkip    = "skip"
	tunnelActionRepoint = "repoint"
	tunnelActionBlocked = "blocked"

	tunnelGuardRepoint      = "repoint"
	tunnelGuardCrossEnv     = "cross-environment"
	tunnelGuardUnresolvable = "unresolvable-backend"

	tunnelLabelRepointBlocked  = "REPOINT (blocked)"
	tunnelLabelRepointAllowed  = "REPOINT (allowed)"
	tunnelLabelCrossEnvBlocked = "CROSS-ENV (blocked)"
	tunnelLabelBlocked         = "BLOCKED"
)

// Live backend health, as far as the control plane can tell.
const (
	backendServing    = "serving"
	backendNotServing = "not-serving"
	backendUnknown    = "unknown"
)

type liveBackendState struct {
	Health string
	Detail string
}

// clusterBackend is a tunnel rule's in-cluster backend, parsed.
type clusterBackend struct {
	Service   string
	Namespace string
	Port      int
}

// clusterBackendURL accepts every spelling of an in-cluster backend a rule may
// carry: the full `.svc.cluster.local:<port>` form AddRoute writes, and the
// short `.svc[:port]` form a hand-edited rule tends to have.
var clusterBackendURL = regexp.MustCompile(
	`^(https?)://([a-z0-9][-a-z0-9]*)\.([a-z0-9][-a-z0-9]*)\.svc(?:\.cluster\.local)?(?::(\d+))?/?$`)

// parseClusterBackend reads a rule's backend. ok=false for anything that is not
// an in-cluster Service URL: an external origin, an http_status catch-all.
func parseClusterBackend(raw string) (clusterBackend, bool) {
	match := clusterBackendURL.FindStringSubmatch(strings.ToLower(strings.TrimSpace(raw)))
	if match == nil {
		return clusterBackend{}, false
	}
	port := 80
	if match[1] == "https" {
		port = 443
	}
	if match[4] != "" {
		parsed, err := strconv.Atoi(match[4])
		if err != nil {
			return clusterBackend{}, false
		}
		port = parsed
	}
	return clusterBackend{Service: match[2], Namespace: match[3], Port: port}, true
}

// sameClusterBackend reports whether a live rule already targets spec, however
// the rule's URL is spelled.
func sameClusterBackend(live string, spec *services.RouteSpec) bool {
	if spec == nil {
		return false
	}
	backend, ok := parseClusterBackend(live)
	return ok && backend.Service == spec.ServiceName &&
		backend.Namespace == spec.ServiceNamespace && backend.Port == spec.ServicePort
}

// tunnelRouteGuard evaluates plan rows. Its two cluster lookups are fields so
// the decision table can be tested without a cluster.
type tunnelRouteGuard struct {
	envs         projectEnvironments
	allowRepoint map[string]bool
	liveHealth   func(ctx context.Context, backend clusterBackend) liveBackendState
	resolve      func(ctx context.Context, spec *services.RouteSpec) error
}

// evaluate decides one row's fate. It only ever makes a row MORE
// conservative: a skip stays a skip, and nothing becomes executable that the
// drift plan did not already propose.
func (g tunnelRouteGuard) evaluate(ctx context.Context, item *tunnelRoutePlanItem) {
	switch item.Action {
	case tunnelActionCreate:
		item.Label = "CREATE"
		if g.blockInconsistentNamespace(item) {
			return
		}
		g.blockUnresolvable(ctx, item)
	case tunnelActionUpdate:
		item.Label = "UPDATE"
		if g.blockInconsistentNamespace(item) {
			return
		}
		g.evaluateUpdate(ctx, item)
	default:
		item.Label = strings.ToUpper(item.Action)
	}
}

func (g tunnelRouteGuard) evaluateUpdate(ctx context.Context, item *tunnelRoutePlanItem) {
	live, parsed := parseClusterBackend(item.CurrentService)
	targetChanged := !parsed || live.Service != item.ServiceName || live.Namespace != item.Namespace
	if !targetChanged {
		item.Reason = fmt.Sprintf("same service and namespace; port %d -> %d", live.Port, item.Port)
		g.blockUnresolvable(ctx, item)
		return
	}

	state := liveBackendState{Health: backendUnknown, Detail: "the live backend is not an in-cluster Service URL"}
	if parsed && g.liveHealth != nil {
		state = g.liveHealth(ctx, live)
	}
	item.CurrentHealth = state.Health

	liveEnv := ""
	if parsed {
		liveEnv = g.envs.environmentOfNamespace(live.Namespace)
	}
	crossEnv := liveEnv != "" && liveEnv != item.Environment &&
		(liveEnv == defaultProductionEnvironmentName || item.Environment == defaultProductionEnvironmentName)

	if state.Health == backendNotServing && !crossEnv {
		item.Reason = fmt.Sprintf("live backend is not serving (%s); replacing a dead route", state.Detail)
		g.blockUnresolvable(ctx, item)
		return
	}

	item.Action = tunnelActionRepoint
	item.Guard = tunnelGuardRepoint
	change := describeBackendChange(live, parsed, item)
	if crossEnv {
		item.Guard = tunnelGuardCrossEnv
		item.Reason = fmt.Sprintf("%s: a %s route would move into the %s environment's namespace (junction environment from %s)",
			change, liveEnv, item.Environment, item.EnvironmentSource)
	} else {
		item.Reason = fmt.Sprintf("%s while the live backend is %s (%s)", change, state.Health, state.Detail)
	}

	if g.allowRepoint[item.Hostname] {
		item.Label = tunnelLabelRepointAllowed
		item.Reason += "; allowed by --allow-repoint"
		g.blockUnresolvable(ctx, item)
		return
	}
	// Every refused repoint carries the same label, cross-environment or
	// not; Guard says which rule refused it.
	item.Blocked = true
	item.Label = tunnelLabelRepointBlocked
	item.Reason += "; a repoint is not a drift fix: rebind the junction if its binding is wrong, or pass --allow-repoint " + item.Hostname
}

// blockInconsistentNamespace refuses a row whose desired namespace belongs to
// a different environment than the one the junction is bound to, where one of
// the two is production. That is a data error, not an intent, so
// --allow-repoint does not override it.
func (g tunnelRouteGuard) blockInconsistentNamespace(item *tunnelRoutePlanItem) bool {
	nsEnv := g.envs.environmentOfNamespace(item.Namespace)
	if nsEnv == "" || nsEnv == item.Environment {
		return false
	}
	if nsEnv != defaultProductionEnvironmentName && item.Environment != defaultProductionEnvironmentName {
		return false
	}
	item.Action = tunnelActionBlocked
	item.Blocked = true
	item.Guard = tunnelGuardCrossEnv
	item.Label = tunnelLabelCrossEnvBlocked
	item.Reason = fmt.Sprintf("desired namespace %s belongs to the %s environment but the junction is bound to %s; fix the service or environment record",
		item.Namespace, nsEnv, item.Environment)
	return true
}

// blockUnresolvable refuses to write a rule whose desired backend does not
// resolve. tunnels-apply writes routes directly, so the resolve-before-write
// gate the provisioner applies (domain_tunnel_backend.go) has to be applied
// here too; an inconclusive check refuses as well.
func (g tunnelRouteGuard) blockUnresolvable(ctx context.Context, item *tunnelRoutePlanItem) {
	if g.resolve == nil {
		return
	}
	spec := &services.RouteSpec{
		Hostname: item.Hostname, ServiceName: item.ServiceName,
		ServiceNamespace: item.Namespace, ServicePort: item.Port,
	}
	if err := g.resolve(ctx, spec); err != nil {
		item.Action = tunnelActionBlocked
		item.Blocked = true
		item.Guard = tunnelGuardUnresolvable
		item.Label = tunnelLabelBlocked
		item.Reason = joinNotes(item.Reason, fmt.Sprintf("desired backend does not resolve: %v", err))
	}
}

func describeBackendChange(live clusterBackend, parsed bool, item *tunnelRoutePlanItem) string {
	if !parsed {
		return fmt.Sprintf("replaces %s with %s.%s", item.CurrentService, item.ServiceName, item.Namespace)
	}
	parts := []string{}
	if live.Service != item.ServiceName {
		parts = append(parts, fmt.Sprintf("service %s -> %s", live.Service, item.ServiceName))
	}
	if live.Namespace != item.Namespace {
		parts = append(parts, fmt.Sprintf("namespace %s -> %s", live.Namespace, item.Namespace))
	}
	return "changes " + strings.Join(parts, " and ")
}

// liveTunnelBackendHealth asks the control plane whether a live rule's backend
// is serving: the Service exists, exposes the port, and selects a Ready pod.
//
// Control plane, not data plane, on purpose: backends behind a NetworkPolicy
// admit only the tunnel, so a probe from this pod fails against a perfectly
// healthy workload, and that failure would read as permission to repoint.
func (h *Handler) liveTunnelBackendHealth(ctx context.Context, backend clusterBackend) liveBackendState {
	if h == nil || h.k8sClient == nil || h.k8sClient.Kube() == nil {
		return liveBackendState{Health: backendUnknown, Detail: "no Kubernetes client is configured"}
	}
	kube := h.k8sClient.Kube()
	svc, err := kube.CoreV1().Services(backend.Namespace).Get(ctx, backend.Service, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return liveBackendState{Health: backendNotServing,
				Detail: fmt.Sprintf("no Service %s in namespace %s", backend.Service, backend.Namespace)}
		}
		return liveBackendState{Health: backendUnknown, Detail: fmt.Sprintf("could not read the Service: %v", err)}
	}
	if svc.Spec.Type == corev1.ServiceTypeExternalName {
		return liveBackendState{Health: backendUnknown, Detail: "ExternalName Service; readiness is not observable"}
	}
	exposed := false
	for _, port := range svc.Spec.Ports {
		if int(port.Port) == backend.Port {
			exposed = true
		}
	}
	if !exposed {
		return liveBackendState{Health: backendNotServing,
			Detail: fmt.Sprintf("Service %s/%s does not expose port %d", backend.Namespace, backend.Service, backend.Port)}
	}
	if len(svc.Spec.Selector) == 0 {
		return liveBackendState{Health: backendUnknown, Detail: "Service has no selector; readiness is not observable"}
	}
	pods, err := kube.CoreV1().Pods(backend.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(svc.Spec.Selector).String(),
	})
	if err != nil {
		return liveBackendState{Health: backendUnknown, Detail: fmt.Sprintf("could not list the Service's pods: %v", err)}
	}
	ready := 0
	for i := range pods.Items {
		if podReady(&pods.Items[i]) {
			ready++
		}
	}
	if ready == 0 {
		return liveBackendState{Health: backendNotServing,
			Detail: fmt.Sprintf("Service %s/%s selects no Ready pod", backend.Namespace, backend.Service)}
	}
	return liveBackendState{Health: backendServing,
		Detail: fmt.Sprintf("%s/%s has %d Ready pod(s)", backend.Namespace, backend.Service, ready)}
}

func podReady(pod *corev1.Pod) bool {
	if pod == nil || pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// refuseRepointOfServingRoute is the repoint guard on the AUTOMATED paths: the
// push reconcile, the junction reconcile that runs on every junction create,
// `ops domains reconcile`, and `domains add`. None of them takes an
// --allow-repoint, so for them a repoint of a serving route is simply refused,
// loudly, and recorded where `enclii domains status` shows it.
//
// Returns true when the write must not happen.
func (h *Handler) refuseRepointOfServingRoute(
	ctx context.Context,
	spec *services.RouteSpec,
	existing *services.IngressRule,
	service *types.Service,
	envName string,
	owner *domainOwner,
) bool {
	if existing == nil || spec == nil {
		return false
	}
	live, parsed := parseClusterBackend(existing.Service)
	if parsed && live.Service == spec.ServiceName && live.Namespace == spec.ServiceNamespace {
		return false
	}

	state := liveBackendState{Health: backendUnknown, Detail: "the live backend is not an in-cluster Service URL"}
	crossEnv := false
	if parsed {
		state = h.liveTunnelBackendHealth(ctx, live)
		envs := h.projectEnvironmentsForService(ctx, service)
		liveEnv := envs.environmentOfNamespace(live.Namespace)
		want := normalizeEnvironmentName(envName)
		crossEnv = liveEnv != "" && liveEnv != want &&
			(liveEnv == defaultProductionEnvironmentName || want == defaultProductionEnvironmentName)
	}
	if state.Health == backendNotServing && !crossEnv {
		return false
	}

	h.logger.Error(ctx, "REFUSED to repoint a serving tunnel route; a repoint is not a reconcile",
		logging.String("domain", spec.Hostname),
		logging.String("existing_backend", existing.Service),
		logging.String("attempted_backend", tunnelRouteServiceURL(spec)),
		logging.String("live_health", state.Health),
		logging.Bool("cross_environment", crossEnv))
	h.recordTunnelRouteFailure(ctx, spec.Hostname, fmt.Sprintf(
		"refused to repoint %s from %s to %s (live backend %s: %s; cross-environment: %t); rebind the junction and use tunnels-apply --allow-repoint if the move is intended",
		spec.Hostname, existing.Service, tunnelRouteServiceURL(spec), state.Health, state.Detail, crossEnv), owner)
	return true
}

// projectEnvironmentsForService loads the environments of a service's
// project, or an empty index when the project cannot be read.
func (h *Handler) projectEnvironmentsForService(ctx context.Context, service *types.Service) projectEnvironments {
	if h == nil || service == nil || h.repos == nil || h.repos.Projects == nil {
		return h.loadProjectEnvironments(nil)
	}
	project, err := h.repos.Projects.GetByID(ctx, service.ProjectID)
	if err != nil || project == nil {
		return h.loadProjectEnvironments(nil)
	}
	return h.loadProjectEnvironments(project)
}
