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
	"fmt"
	"strings"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ensureTunnelRoute adds a Cloudflare tunnel route for a domain
func (h *Handler) ensureTunnelRoute(ctx context.Context, domain string, service *types.Service, envName string, servicePort int) {
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

	if h.tunnelRouteMatches(ctx, routeSpec) {
		h.logger.Debug(ctx, "Tunnel route already targets desired service",
			logging.String("domain", domain),
			logging.String("namespace", namespace))
		return
	}

	if err := h.tunnelRoutesService.AddRoute(ctx, routeSpec); err != nil {
		h.logger.Warn(ctx, "Failed to add tunnel route for domain",
			logging.String("domain", domain),
			logging.Error("error", err))
	} else {
		h.logger.Info(ctx, "Tunnel route added for domain",
			logging.String("domain", domain),
			logging.String("service", service.Name),
			logging.String("namespace", namespace),
			logging.Int("port", servicePort))
	}
}

func (h *Handler) tunnelRouteMatches(ctx context.Context, spec *services.RouteSpec) bool {
	if h == nil || h.tunnelRoutesService == nil || spec == nil {
		return false
	}

	routes, err := h.tunnelRoutesService.ListRoutes(ctx)
	if err != nil {
		h.logger.Warn(ctx, "Failed to list tunnel routes before reconciliation",
			logging.String("domain", spec.Hostname),
			logging.Error("error", err))
		return false
	}

	want := tunnelRouteServiceURL(spec)
	for _, route := range routes {
		// Case-insensitive: a mixed-case rule names the same host, and reading
		// it as a different one reports "no route" and provokes a duplicate.
		if !strings.EqualFold(strings.TrimSpace(route.Hostname), strings.TrimSpace(spec.Hostname)) {
			continue
		}
		return route.Service == want
	}
	return false
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
