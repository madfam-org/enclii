package api

// Where a domain's traffic is sent once it has been provisioned: the tunnel
// ingress rule, and the Kubernetes namespace that rule points at.
//
// Split out of domain_provisioner.go, which was over the 600-line mark before
// this round added to it. Nothing here decides WHETHER a domain may be routed
// -- that is the ownership question, in domain_ownership.go -- these are the
// mechanics of routing one that already may be.

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ensureTunnelRoute adds a Cloudflare tunnel route for a domain.
//
// Three things happen before this function will change a live ingress rule, in
// this order, and each of them exists because the 2026-08-27 janua outage got
// past the absence of it:
//
//  1. The existing rule for this hostname is READ and kept. Without it there is
//     nothing to compare against, nothing to revert to, and no way to tell a
//     first-time add from a rewrite of something that already works.
//  2. The backend is RESOLVED against the API server. The rewrite that broke
//     every SSO surface pointed eight hostnames at a Service that has never
//     existed; one Get would have refused all eight.
//  3. After the write, the backend is PROBED, and a rule that answers nothing
//     is put back the way it was found.
//
// Failures are recorded on the domain record as well as logged, so `enclii
// domains status` shows them. owner names the service whose provisioning this
// is; a nil owner records nothing, which is the cross-tenant rule
// persistDomainProvisioningResult enforces.
func (h *Handler) ensureTunnelRoute(
	ctx context.Context,
	domain string,
	service *types.Service,
	envName string,
	servicePort int,
	owner *domainOwner,
) {
	if h.tunnelRoutesService == nil {
		return
	}
	if service == nil {
		h.logger.Warn(ctx, "Skipping tunnel route for nil service",
			logging.String("domain", domain))
		return
	}

	// Determine namespace from the service's project record
	namespace := h.resolveServiceNamespace(ctx, service, envName)

	// Connect/keepAlive timeouts intentionally omitted: Cloudflare's
	// Configuration API rejects them as quoted strings (`Bad Configuration:
	// strconv.ParseInt: parsing "30s": invalid syntax`). Cloudflare's
	// per-rule defaults (30s connect, 90s keepalive) match what we want,
	// so dropping the explicit fields is functionally equivalent and
	// avoids the API rejection. Re-introduce when our cloudflare client
	// switches to numeric serialization.
	routeSpec := &services.RouteSpec{
		Hostname:         domain,
		ServiceName:      service.Name,
		ServiceNamespace: namespace,
		ServicePort:      servicePort,
	}

	// Read the incumbent BEFORE deciding anything. This single read serves
	// three purposes that used to be spread across a match check and a blind
	// write: it answers "is this already right", it distinguishes an add from
	// a replace, and it is the value the canary reverts to.
	existing, existingKnown := h.existingTunnelRoute(ctx, routeSpec.Hostname)

	if existingKnown && existing != nil && existing.Service == tunnelRouteServiceURL(routeSpec) {
		h.logger.Debug(ctx, "Tunnel route already targets desired service",
			logging.String("domain", domain),
			logging.String("namespace", namespace))
		return
	}

	// GUARD 1. A replacement of a working rule and a first-time add are not
	// the same risk, so an unresolvable backend is not refused the same way.
	//
	// A hostname whose current rule could NOT be read counts as a replacement.
	// The read failed, so this hostname may well be serving traffic right now,
	// and the softer treatment reserved for a genuinely-new rule would be
	// granted on the strength of not knowing. Same principle as the
	// inconclusive backend check: not knowing is never a licence to write.
	replacing := !existingKnown || existing != nil
	if err := h.resolveTunnelBackend(ctx, routeSpec); err != nil {
		h.refuseUnresolvableTunnelRoute(ctx, routeSpec, existing, replacing, err, owner)
		return
	}

	if err := h.tunnelRoutesService.AddRoute(ctx, routeSpec); err != nil {
		h.logger.Warn(ctx, "Failed to add tunnel route for domain",
			logging.String("domain", domain),
			logging.Error("error", err))
		return
	}

	h.logger.Info(ctx, "Tunnel route added for domain",
		logging.String("domain", domain),
		logging.String("service", service.Name),
		logging.String("namespace", namespace),
		logging.Int("port", servicePort))

	// GUARD 2. The rule is live now; prove it serves, and undo it if it does
	// not. Only reached when a rule was actually written, which in steady
	// state is close to never.
	h.canaryTunnelRoute(ctx, routeSpec, existing, replacing, owner)
}

// refuseUnresolvableTunnelRoute is the terminal state of Guard 1: the write did
// not happen, and this is where that becomes visible.
//
// The two cases are logged differently on purpose. Refusing to ADD a first rule
// for a backend that is not there is ordinary safety — the hostname was not
// serving before and is not serving now. Refusing to REPLACE a rule that
// already points somewhere is the outage-killer, so it logs the incumbent
// backend it just declined to destroy; that line is the one an operator needs
// when they wonder why a manifest is not taking effect.
func (h *Handler) refuseUnresolvableTunnelRoute(
	ctx context.Context,
	spec *services.RouteSpec,
	existing *services.IngressRule,
	replacing bool,
	resolveErr error,
	owner *domainOwner,
) {
	attempted := tunnelRouteServiceURL(spec)
	inconclusive := errors.Is(resolveErr, errTunnelBackendUnknown)

	if replacing {
		// Fails SAFE whether the answer was "no such backend" or "could not
		// find out". An inconclusive check is not permission to overwrite a
		// hostname that is currently serving traffic; "we could not check" is
		// never "we checked and it was fine".
		//
		// existing is nil here only when the tunnel config itself could not be
		// read, in which case we do not know what is being protected — which is
		// exactly why it is being protected.
		incumbent := "unknown (tunnel config could not be read)"
		if existing != nil {
			incumbent = existing.Service
		}
		h.logger.Error(ctx, "REFUSED to rewrite a live tunnel ingress rule onto an unresolvable backend",
			logging.String("domain", spec.Hostname),
			logging.String("attempted_backend", attempted),
			logging.String("existing_backend", incumbent),
			logging.Bool("resolution_inconclusive", inconclusive),
			logging.Error("error", resolveErr))
		h.recordTunnelRouteFailure(ctx, spec.Hostname, fmt.Sprintf(
			"refused to repoint %s at %s (kept %s): %v",
			spec.Hostname, attempted, incumbent, resolveErr), owner)
		return
	}

	if inconclusive {
		// No rule exists, so nothing is at risk from waiting. The next
		// reconciliation pass asks again.
		h.logger.Warn(ctx, "Deferring a new tunnel ingress rule: its backend could not be verified this pass",
			logging.String("domain", spec.Hostname),
			logging.String("attempted_backend", attempted),
			logging.Error("error", resolveErr))
		h.recordTunnelRouteFailure(ctx, spec.Hostname, fmt.Sprintf(
			"tunnel route for %s deferred, backend %s could not be verified: %v",
			spec.Hostname, attempted, resolveErr), owner)
		return
	}

	h.logger.Error(ctx, "Refusing to add a tunnel ingress rule for a backend that does not exist",
		logging.String("domain", spec.Hostname),
		logging.String("attempted_backend", attempted),
		logging.Error("error", resolveErr))
	h.recordTunnelRouteFailure(ctx, spec.Hostname, fmt.Sprintf(
		"no tunnel route written for %s: %v", spec.Hostname, resolveErr), owner)
}

// canaryTunnelRoute probes a freshly written rule and undoes it if nothing
// answers.
//
// The revert is what makes the probe worth running: detecting a broken route
// and leaving it in place would only have added a log line to the outage. An
// UPDATE is reverted to the rule that was there; a fresh ADD is removed
// outright, which returns the hostname to the tunnel's catch-all rather than
// leaving it pointed at a black hole.
func (h *Handler) canaryTunnelRoute(
	ctx context.Context,
	spec *services.RouteSpec,
	previous *services.IngressRule,
	replaced bool,
	owner *domainOwner,
) {
	if !h.canaryEnabled() {
		return
	}

	err := h.probeBackend(ctx, spec)
	if err == nil {
		h.logger.Debug(ctx, "Tunnel route canary passed",
			logging.String("domain", spec.Hostname),
			logging.String("backend", tunnelRouteServiceURL(spec)))
		return
	}

	if replaced && previous != nil {
		h.revertTunnelRoute(ctx, spec, previous, err, owner)
		return
	}
	h.removeFailedTunnelRoute(ctx, spec, err, owner)
}

// revertTunnelRoute restores the rule that was in place before a failed write.
func (h *Handler) revertTunnelRoute(
	ctx context.Context,
	spec *services.RouteSpec,
	previous *services.IngressRule,
	probeErr error,
	owner *domainOwner,
) {
	restore, parseErr := routeSpecFromIngressRule(previous)
	if parseErr != nil {
		// The incumbent rule is not a cluster-service URL we can rebuild a
		// spec from — an external origin, say. Loud and untouched: a wrong
		// rule left in place is bad, but destroying a rule we cannot restore
		// is worse, and this is exactly the state a human has to see.
		h.logger.Error(ctx, "Tunnel route canary FAILED and the previous rule could not be rebuilt to restore it",
			logging.String("domain", spec.Hostname),
			logging.String("backend", tunnelRouteServiceURL(spec)),
			logging.String("previous_backend", previous.Service),
			logging.Error("error", probeErr))
		h.recordTunnelRouteFailure(ctx, spec.Hostname, fmt.Sprintf(
			"canary failed for %s and the previous rule (%s) could not be restored automatically: %v",
			tunnelRouteServiceURL(spec), previous.Service, probeErr), owner)
		return
	}

	if err := h.tunnelRoutesService.AddRoute(ctx, restore); err != nil {
		h.logger.Error(ctx, "Tunnel route canary FAILED and the revert to the previous rule also failed",
			logging.String("domain", spec.Hostname),
			logging.String("backend", tunnelRouteServiceURL(spec)),
			logging.String("previous_backend", previous.Service),
			logging.Error("error", err))
		h.recordTunnelRouteFailure(ctx, spec.Hostname, fmt.Sprintf(
			"canary failed for %s and the revert to %s failed: %v",
			tunnelRouteServiceURL(spec), previous.Service, err), owner)
		return
	}

	h.logger.Error(ctx, "Tunnel route canary failed, rule reverted to the previous backend",
		logging.String("domain", spec.Hostname),
		logging.String("backend", tunnelRouteServiceURL(spec)),
		logging.String("reverted_to", previous.Service),
		logging.Error("error", probeErr))
	h.recordTunnelRouteFailure(ctx, spec.Hostname, fmt.Sprintf(
		"canary failed, rule reverted: %v", probeErr), owner)
}

// removeFailedTunnelRoute withdraws a rule that was newly added and never
// served.
func (h *Handler) removeFailedTunnelRoute(
	ctx context.Context, spec *services.RouteSpec, probeErr error, owner *domainOwner,
) {
	if err := h.tunnelRoutesService.RemoveRoute(ctx, spec.Hostname); err != nil {
		h.logger.Error(ctx, "Tunnel route canary FAILED and the new rule could not be withdrawn",
			logging.String("domain", spec.Hostname),
			logging.String("backend", tunnelRouteServiceURL(spec)),
			logging.Error("error", err))
		h.recordTunnelRouteFailure(ctx, spec.Hostname, fmt.Sprintf(
			"canary failed for %s and the new rule could not be withdrawn: %v",
			tunnelRouteServiceURL(spec), err), owner)
		return
	}

	h.logger.Error(ctx, "Tunnel route canary failed, newly added rule withdrawn",
		logging.String("domain", spec.Hostname),
		logging.String("backend", tunnelRouteServiceURL(spec)),
		logging.Error("error", probeErr))
	h.recordTunnelRouteFailure(ctx, spec.Hostname, fmt.Sprintf(
		"canary failed, rule reverted: %v", probeErr), owner)
}

// recordTunnelRouteFailure puts a routing failure where `enclii domains status`
// reads it, via the same owner-keyed write every other provisioning outcome
// uses. A hostname with no CustomDomain row of its own (junctions provision by
// hostname alone) is a no-op there, by design.
func (h *Handler) recordTunnelRouteFailure(
	ctx context.Context, domain, message string, owner *domainOwner,
) {
	if h == nil || owner == nil || message == "" {
		return
	}
	result := domainProvisioningResult{Domain: domain, Mechanism: mechanismZoneCNAME}
	result.setErr(errors.New(message))
	h.persistDomainProvisioningResult(ctx, result, owner)
}

// existingTunnelRoute returns the rule currently serving hostname, and whether
// the tunnel config could be read at all.
//
// The second return is the point. A failed list is NOT "no rule exists" — that
// conflation is how a read error becomes permission to write. known=false makes
// every caller treat the hostname as potentially live.
func (h *Handler) existingTunnelRoute(ctx context.Context, hostname string) (*services.IngressRule, bool) {
	if h == nil || h.tunnelRoutesService == nil {
		return nil, false
	}

	routes, err := h.tunnelRoutesService.ListRoutes(ctx)
	if err != nil {
		h.logger.Warn(ctx, "Failed to list tunnel routes before reconciliation",
			logging.String("domain", hostname),
			logging.Error("error", err))
		return nil, false
	}

	for i := range routes {
		// Case-insensitive: a mixed-case rule names the same host, and reading
		// it as a different one reports "no route" and provokes a duplicate.
		if strings.EqualFold(strings.TrimSpace(routes[i].Hostname), strings.TrimSpace(hostname)) {
			rule := routes[i]
			return &rule, true
		}
	}
	return nil, true
}

// clusterServiceURL matches the in-cluster backend form AddRoute writes, which
// is the only form a RouteSpec can be rebuilt from.
var clusterServiceURL = regexp.MustCompile(
	`^http://([a-z0-9][-a-z0-9.]*)\.([a-z0-9][-a-z0-9]*)\.svc\.cluster\.local:(\d+)$`)

// routeSpecFromIngressRule rebuilds the spec that produced a rule, so a failed
// write can be undone by re-adding what was there. Anything not in the
// in-cluster form — an external origin, an http_status catch-all — cannot be
// rebuilt, and the caller must then leave the rule alone and say so loudly.
func routeSpecFromIngressRule(rule *services.IngressRule) (*services.RouteSpec, error) {
	if rule == nil {
		return nil, errors.New("no previous rule")
	}
	match := clusterServiceURL.FindStringSubmatch(strings.TrimSpace(rule.Service))
	if match == nil {
		return nil, fmt.Errorf("previous backend %q is not an in-cluster service URL", rule.Service)
	}
	port, err := strconv.Atoi(match[3])
	if err != nil {
		return nil, fmt.Errorf("previous backend %q has an unreadable port: %w", rule.Service, err)
	}
	return &services.RouteSpec{
		Hostname:         rule.Hostname,
		ServiceName:      match[1],
		ServiceNamespace: match[2],
		ServicePort:      port,
	}, nil
}

func tunnelRouteServiceURL(spec *services.RouteSpec) string {
	return fmt.Sprintf("http://%s.%s.svc.cluster.local:%d",
		spec.ServiceName, spec.ServiceNamespace, spec.ServicePort)
}

// serviceNameOf is a nil-safe service name for log fields.
func serviceNameOf(service *types.Service) string {
	if service == nil {
		return ""
	}
	return service.Name
}

// resolveServiceNamespace determines the Kubernetes namespace for a service.
// It prefers the explicit service namespace because imported/adopted workloads
// can live outside their Enclii project namespace. Non-production routes prefer
// the environment namespace first so staging custom domains do not point at
// production workloads when the service row was adopted from a live namespace.
func (h *Handler) resolveServiceNamespace(ctx context.Context, service *types.Service, envName string) string {
	if service == nil {
		return ""
	}
	if !isProductionEnvironmentName(envName) {
		if namespace := h.environmentNamespace(service, envName); namespace != "" {
			return namespace
		}
	}
	if service.K8sNamespace != nil && *service.K8sNamespace != "" {
		return *service.K8sNamespace
	}

	if namespace := h.environmentNamespace(service, envName); namespace != "" {
		return namespace
	}

	// Fall back to project slug
	project, err := h.repos.Projects.GetByID(ctx, service.ProjectID)
	if err == nil && project.Slug != "" {
		return project.Slug
	}

	// Last resort: use project name or service name prefix
	h.logger.Warn(ctx, "Could not resolve namespace from project, using service name prefix",
		logging.String("service", service.Name))
	return service.Name
}

func (h *Handler) environmentNamespace(service *types.Service, envName string) string {
	if h == nil || h.repos == nil || h.repos.Environments == nil || service == nil || strings.TrimSpace(envName) == "" {
		return ""
	}

	env, err := h.repos.Environments.GetByProjectAndName(service.ProjectID, envName)
	if err == nil && env != nil && env.KubeNamespace != "" {
		return env.KubeNamespace
	}
	return ""
}

func isProductionEnvironmentName(envName string) bool {
	switch strings.ToLower(strings.TrimSpace(envName)) {
	case "", "production", "prod":
		return true
	default:
		return false
	}
}
