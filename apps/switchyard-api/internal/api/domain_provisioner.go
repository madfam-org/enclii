package api

import (
	"context"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

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
	envConfig *EncliiYAML,
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
	domainCfg EncliiYAMLDomain,
	runtimePort int,
) {
	domainName := domainCfg.Name
	envName := domainCfg.Environment
	servicePort := domainCfg.GetPort(runtimePort)

	// Validate domain format
	if !isValidDomain(domainName) {
		h.logger.Warn(ctx, "Skipping invalid domain from enclii.yaml",
			logging.String("domain", domainName),
			logging.String("service", service.Name))
		return
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
		h.ensureDNSRecord(ctx, domainName)
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

	// Create DNS CNAME record
	h.ensureDNSRecord(ctx, domainName)
}

// ensureTunnelRoute adds a Cloudflare tunnel route for a domain
func (h *Handler) ensureTunnelRoute(ctx context.Context, domain string, service *types.Service, envName string, servicePort int) {
	if h.tunnelRoutesService == nil {
		return
	}

	// Check if route already exists
	exists, err := h.tunnelRoutesService.RouteExists(ctx, domain)
	if err != nil {
		h.logger.Warn(ctx, "Failed to check tunnel route existence",
			logging.String("domain", domain),
			logging.Error("error", err))
		// Continue to try adding — AddRoute handles duplicates gracefully
	}

	if exists {
		h.logger.Debug(ctx, "Tunnel route already exists",
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

// resolveServiceNamespace determines the Kubernetes namespace for a service.
// It looks up the project's slug to use as the namespace. Falls back to the
// project slug derived from the service name if the project lookup fails.
func (h *Handler) resolveServiceNamespace(ctx context.Context, service *types.Service, envName string) string {
	// Try to get the namespace from the environment record
	env, err := h.repos.Environments.GetByProjectAndName(service.ProjectID, envName)
	if err == nil && env.KubeNamespace != "" {
		return env.KubeNamespace
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

// ensureDNSRecord creates a CNAME DNS record in Cloudflare for the domain.
// Uses the DomainSyncService's tunnel CNAME target.
func (h *Handler) ensureDNSRecord(ctx context.Context, domain string) {
	if h.domainSyncService == nil {
		return
	}

	// Get the Cloudflare client from the domain sync service
	cfClient := h.domainSyncService.GetCloudflareClient()
	if cfClient == nil {
		h.logger.Warn(ctx, "Cloudflare client not available for DNS record creation",
			logging.String("domain", domain))
		return
	}

	// Ensure the Cloudflare zone exists for this domain (creates if missing)
	if _, err := cfClient.EnsureZoneForDomain(ctx, domain); err != nil {
		h.logger.Warn(ctx, "Failed to ensure Cloudflare zone for domain",
			logging.String("domain", domain),
			logging.Error("error", err))
		// Continue — EnsureDNSRecord will fail with a clearer error if zone is truly missing
	}

	// The tunnel CNAME target — domains are CNAME'd to the tunnel endpoint
	tunnelCNAME := services.DefaultTunnelCNAME

	record, created, err := cfClient.EnsureDNSRecord(ctx, domain, tunnelCNAME)
	if err != nil {
		h.logger.Warn(ctx, "Failed to create DNS record for domain",
			logging.String("domain", domain),
			logging.String("cname_target", tunnelCNAME),
			logging.Error("error", err))
		return
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
