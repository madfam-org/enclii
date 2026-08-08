package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/manifest"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/cloudflare"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

type junctionProvisioningSummary struct {
	Domain           string   `json:"domain"`
	TunnelRouteReady bool     `json:"tunnel_route_ready"`
	DNSRequested     bool     `json:"dns_requested"`
	Warnings         []string `json:"warnings,omitempty"`

	// Mechanism records whether the domain was provisioned as a zone+CNAME
	// (we control the nameservers) or as a Cloudflare for SaaS custom
	// hostname (the client does). PendingClientDNSRecords is what the domain
	// owner still has to create; while it is non-empty the junction is
	// waiting on them, not on us.
	Mechanism              string                   `json:"mechanism,omitempty"`
	PendingClientDNSAction string                   `json:"pending_client_dns_action,omitempty"`
	PendingClientDNSRecord []types.PendingDNSRecord `json:"pending_client_dns_records,omitempty"`
}

type junctionRouteReconcileSummary struct {
	Total  int      `json:"total"`
	Ready  int      `json:"ready"`
	Failed []string `json:"failed,omitempty"`
}

// provisionDomainsFromYAML auto-provisions custom domains declared in enclii.yaml.
// For each domain it:
//  1. Creates a CustomDomain record in the database (if not exists)
//  2. Adds a tunnel route via TunnelRoutesManager
//  3. Creates a DNS CNAME record in Cloudflare
//
// This is called from triggerAutoDeploy after a successful build.
// Errors are logged but don't block the deployment.
func (h *Handler) provisionDomainsFromYAML(
	ctx context.Context,
	service *types.Service,
	envConfig *manifest.EncliiYAML,
) {
	if envConfig == nil || len(envConfig.Spec.Domains) == 0 {
		return
	}

	runtimePort := envConfig.Spec.Runtime.Port
	for _, domainCfg := range envConfig.Spec.Domains {
		h.provisionSingleDomain(ctx, service, domainCfg, runtimePort)
	}
}

// provisionSingleDomain provisions a single domain: DB record + tunnel route + DNS CNAME
func (h *Handler) provisionSingleDomain(
	ctx context.Context,
	service *types.Service,
	domainCfg manifest.EncliiYAMLDomain,
	runtimePort int,
) {
	domainName := domainCfg.Name
	envName := domainCfg.Environment
	servicePort := domainCfg.GetPort(runtimePort)
	external, externalDeclared := domainCfg.ExternalOverride()

	// Validate domain format. Nested subdomains are allowed only on the
	// Cloudflare for SaaS path, which issues a certificate for the exact
	// hostname; Universal SSL on the zone path covers one level only.
	if err := validateDomain(domainName, externalDeclared && external); err != nil {
		h.logger.Warn(ctx, "Skipping invalid domain from enclii.yaml",
			logging.String("domain", domainName),
			logging.String("service", service.Name),
			logging.Error("error", err))
		return
	}

	var externalOverride *bool
	if externalDeclared {
		externalOverride = &external
	}

	// Check if domain already exists in the database
	exists, err := h.repos.CustomDomains.Exists(ctx, domainName)
	if err != nil {
		h.logger.Warn(ctx, "Failed to check domain existence",
			logging.String("domain", domainName),
			logging.Error("error", err))
		return
	}

	if exists {
		h.logger.Debug(ctx, "Domain already registered, skipping creation",
			logging.String("domain", domainName))
		// Even if already registered, ensure tunnel route + DNS exist
		h.ensureTunnelRoute(ctx, domainName, service, envName, servicePort)
		h.ensureDomainRouting(ctx, domainName, externalOverride)
		return
	}

	// Find the environment
	env, err := h.repos.Environments.GetByProjectAndName(service.ProjectID, envName)
	if err != nil {
		h.logger.Warn(ctx, "Environment not found for domain provisioning, will provision on next deploy",
			logging.String("domain", domainName),
			logging.String("environment", envName),
			logging.Error("error", err))
		return
	}

	// Determine TLS issuer
	tlsIssuer := "letsencrypt-prod"
	if envName != "production" {
		tlsIssuer = "letsencrypt-staging"
	}

	// Create custom domain in database
	domain := &types.CustomDomain{
		ServiceID:     service.ID,
		EnvironmentID: env.ID,
		Domain:        domainName,
		Verified:      false,
		TLSEnabled:    domainCfg.IsTLSEnabled(),
		TLSIssuer:     tlsIssuer,
	}

	if err := h.repos.CustomDomains.Create(ctx, domain); err != nil {
		h.logger.Warn(ctx, "Failed to create custom domain from enclii.yaml",
			logging.String("domain", domainName),
			logging.Error("error", err))
		return
	}

	h.logger.Info(ctx, "Custom domain created from enclii.yaml",
		logging.String("domain", domainName),
		logging.String("service", service.Name),
		logging.String("environment", envName))

	// Add tunnel route
	h.ensureTunnelRoute(ctx, domainName, service, envName, servicePort)

	// Provision edge routing: zone + CNAME for domains whose nameservers we
	// control, Cloudflare for SaaS custom hostname for client-owned domains.
	h.ensureDomainRouting(ctx, domainName, externalOverride)
}

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
		if route.Hostname != spec.Hostname {
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

// ensureDomainRouting provisions edge routing for a domain and persists the
// typed outcome on the domain record.
//
// Two mechanisms exist and the choice is per-domain:
//   - zone + CNAME  — for domains whose nameservers point at our Cloudflare
//     account. Unchanged from before.
//   - custom hostname — Cloudflare for SaaS, for client-owned domains that
//     keep their own registrar and nameservers.
//
// external forces the mechanism when non-nil (enclii.yaml `external:`);
// nil means auto-detect. Failures are logged and do NOT abort the deploy —
// that is deliberate — but they are also written to the domain record so the
// failure stays legible on the read path.
func (h *Handler) ensureDomainRouting(ctx context.Context, domain string, external *bool) domainProvisioningResult {
	if h.domainSyncService == nil {
		return domainProvisioningResult{Domain: domain, Mechanism: mechanismZoneCNAME}
	}

	// Get the Cloudflare client from the domain sync service
	cfClient := h.domainSyncService.GetCloudflareClient()
	if cfClient == nil {
		h.logger.Warn(ctx, "Cloudflare client not available for domain provisioning",
			logging.String("domain", domain))
		return domainProvisioningResult{Domain: domain, Mechanism: mechanismZoneCNAME}
	}

	mechanism := h.resolveDomainMechanism(ctx, cfClient, domain, external)

	var result domainProvisioningResult
	if mechanism == mechanismCustomHostname {
		result = h.ensureCustomHostname(ctx, domain)
	} else {
		result = h.ensureZoneDNSRecord(ctx, cfClient, domain)
	}

	if result.Err != nil {
		h.logger.Warn(ctx, "Domain provisioning failed",
			logging.String("domain", domain),
			logging.String("mechanism", string(result.Mechanism)),
			logging.Error("error", result.Err))
	} else if result.WaitingOnClient() {
		h.logger.Info(ctx, "Domain provisioning is waiting on the domain owner",
			logging.String("domain", domain),
			logging.String("mechanism", string(result.Mechanism)),
			logging.String("action", describePendingClientAction(result)))
	}

	h.persistDomainProvisioningResult(ctx, result)
	return result
}

// ensureZoneDNSRecord is the pre-existing zone + proxied CNAME path, used for
// domains whose nameservers are delegated to our Cloudflare account.
func (h *Handler) ensureZoneDNSRecord(ctx context.Context, cfClient *cloudflare.Client, domain string) domainProvisioningResult {
	result := domainProvisioningResult{Domain: domain, Mechanism: mechanismZoneCNAME}

	// Ensure the Cloudflare zone exists for this domain (creates if missing)
	if _, err := cfClient.EnsureZoneForDomain(ctx, domain); err != nil {
		h.logger.Warn(ctx, "Failed to ensure Cloudflare zone for domain",
			logging.String("domain", domain),
			logging.Error("error", err))
		// Continue — EnsureDNSRecord will fail with a clearer error if zone is truly missing
	}

	// The tunnel CNAME target — domains are CNAME'd to the tunnel endpoint.
	tunnelCNAME := h.domainSyncService.TunnelCNAME()

	record, created, err := cfClient.EnsureDNSRecord(ctx, domain, tunnelCNAME)
	if err != nil {
		h.logger.Warn(ctx, "Failed to create DNS record for domain",
			logging.String("domain", domain),
			logging.String("cname_target", tunnelCNAME),
			logging.Error("error", err))
		result.setErr(fmt.Errorf("failed to create DNS record for %s: %w", domain, err))
		return result
	}

	if created {
		h.logger.Info(ctx, "DNS CNAME record created in Cloudflare",
			logging.String("domain", domain),
			logging.String("cname_target", tunnelCNAME),
			logging.String("record_id", record.ID))
	} else {
		h.logger.Debug(ctx, "DNS record already exists",
			logging.String("domain", domain),
			logging.String("existing_content", record.Content))
	}

	return result
}

func (h *Handler) ensureJunctionInfrastructure(ctx context.Context, domain string, service *types.Service) junctionProvisioningSummary {
	summary := junctionProvisioningSummary{
		Domain:       domain,
		DNSRequested: h != nil && h.domainSyncService != nil,
	}
	if h == nil || service == nil {
		summary.Warnings = append(summary.Warnings, "service unavailable")
		return summary
	}

	h.ensureTunnelRoute(ctx, domain, service, defaultProductionEnvironmentName, 80)
	if h.tunnelRoutesService != nil {
		ready, err := h.tunnelRoutesService.RouteExists(ctx, domain)
		if err != nil {
			summary.Warnings = append(summary.Warnings, "tunnel route readback failed: "+err.Error())
		}
		summary.TunnelRouteReady = ready
	}

	result := h.ensureDomainRouting(ctx, domain, nil)
	summary.Mechanism = string(result.Mechanism)
	summary.PendingClientDNSRecord = result.PendingDNSRecords
	if action := describePendingClientAction(result); action != "" {
		summary.PendingClientDNSAction = action
	}
	if result.Err != nil {
		summary.Warnings = append(summary.Warnings, result.ErrorMessage)
	}
	return summary
}

func (h *Handler) reconcileJunctionTunnelRoutesForProject(ctx context.Context, project *types.Project) junctionRouteReconcileSummary {
	summary := junctionRouteReconcileSummary{}
	if h == nil || h.repos == nil || h.repos.Junctions == nil || h.repos.Services == nil || project == nil {
		return summary
	}

	if _, err := h.ensureDefaultProductionEnvironment(ctx, project); err != nil {
		h.logger.Warn(ctx, "Failed to ensure default environment before junction route reconciliation",
			logging.String("project", project.Slug),
			logging.Error("error", err))
	}

	junctions, err := h.repos.Junctions.ListByProject(ctx, project.ID)
	if err != nil {
		h.logger.Warn(ctx, "Failed to list junctions during route reconciliation",
			logging.String("project", project.Slug),
			logging.Error("error", err))
		return summary
	}

	summary.Total = len(junctions)
	for _, junction := range junctions {
		if junction == nil || junction.Domain == "" {
			continue
		}

		service, err := h.repos.Services.GetByID(junction.ServiceID)
		if err != nil {
			summary.Failed = append(summary.Failed, junction.Domain)
			h.logger.Warn(ctx, "Skipping junction route reconciliation because service lookup failed",
				logging.String("domain", junction.Domain),
				logging.String("service_id", junction.ServiceID.String()),
				logging.Error("error", err))
			continue
		}

		h.ensureTunnelRoute(ctx, junction.Domain, service, defaultProductionEnvironmentName, 80)
		h.ensureDomainRouting(ctx, junction.Domain, nil)

		if h.tunnelRoutesService == nil {
			continue
		}
		ready, err := h.tunnelRoutesService.RouteExists(ctx, junction.Domain)
		if err != nil || !ready {
			summary.Failed = append(summary.Failed, junction.Domain)
			if err != nil {
				h.logger.Warn(ctx, "Junction tunnel route readback failed after reconciliation",
					logging.String("domain", junction.Domain),
					logging.Error("error", err))
			}
			continue
		}
		summary.Ready++
	}

	return summary
}

func (h *Handler) scheduleJunctionTunnelRouteReconcile(project *types.Project) {
	if h == nil || project == nil {
		return
	}

	go func(projectCopy types.Project) {
		for _, delay := range []time.Duration{2 * time.Second, 15 * time.Second} {
			time.Sleep(delay)

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			summary := h.reconcileJunctionTunnelRoutesForProject(ctx, &projectCopy)
			cancel()

			h.logger.Info(context.Background(), "Delayed junction route reconciliation completed",
				logging.String("project", projectCopy.Slug),
				logging.Int("total", summary.Total),
				logging.Int("ready", summary.Ready))
		}
	}(*project)
}

// cleanupDomainsForService removes tunnel routes and DNS records for all domains of a service.
// Called during service deletion.
func (h *Handler) cleanupDomainsForService(ctx context.Context, serviceID uuid.UUID) {
	// Get all domains for this service (across all environments)
	domains, err := h.repos.CustomDomains.GetByServiceID(ctx, serviceID.String())
	if err != nil {
		h.logger.Warn(ctx, "Failed to get domains for cleanup",
			logging.String("service_id", serviceID.String()),
			logging.Error("error", err))
		return
	}

	for _, domain := range domains {
		// Remove tunnel route
		if h.tunnelRoutesService != nil {
			if err := h.tunnelRoutesService.RemoveRoute(ctx, domain.Domain); err != nil {
				h.logger.Warn(ctx, "Failed to remove tunnel route during cleanup",
					logging.String("domain", domain.Domain),
					logging.Error("error", err))
			} else {
				h.logger.Info(ctx, "Tunnel route removed during cleanup",
					logging.String("domain", domain.Domain))
			}
		}

		// Remove the Cloudflare for SaaS custom hostname, if this domain was
		// provisioned that way. There is no DNS record of ours to delete in
		// that case — the records live on the client's nameservers.
		if domain.CustomHostnameID != "" {
			if err := h.deleteCustomHostname(ctx, domain.Domain, domain.CustomHostnameID); err != nil {
				h.logger.Warn(ctx, "Failed to delete custom hostname during cleanup",
					logging.String("domain", domain.Domain),
					logging.String("custom_hostname_id", domain.CustomHostnameID),
					logging.Error("error", err))
			} else {
				h.logger.Info(ctx, "Custom hostname deleted during cleanup",
					logging.String("domain", domain.Domain))
			}
			continue
		}

		// Remove DNS record (zone-aware: finds correct zone for each domain)
		if h.domainSyncService != nil {
			cfClient := h.domainSyncService.GetCloudflareClient()
			if cfClient != nil {
				zone, zoneErr := cfClient.FindZoneForDomain(ctx, domain.Domain)
				if zoneErr != nil {
					h.logger.Warn(ctx, "Failed to find zone for domain during cleanup",
						logging.String("domain", domain.Domain),
						logging.Error("error", zoneErr))
					continue
				}
				record, err := cfClient.GetDNSRecord(ctx, domain.Domain)
				if err != nil {
					h.logger.Warn(ctx, "Failed to look up DNS record during cleanup",
						logging.String("domain", domain.Domain),
						logging.Error("error", err))
					continue
				}
				if record != nil {
					if err := cfClient.DeleteDNSRecordInZone(ctx, zone.ID, record.ID); err != nil {
						h.logger.Warn(ctx, "Failed to delete DNS record during cleanup",
							logging.String("domain", domain.Domain),
							logging.Error("error", err))
					} else {
						h.logger.Info(ctx, "DNS record deleted during cleanup",
							logging.String("domain", domain.Domain))
					}
				}
			}
		}
	}
}
