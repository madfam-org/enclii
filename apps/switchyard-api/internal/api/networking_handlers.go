package api

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// GetServiceNetworking returns combined networking info for a service
// GET /api/v1/services/:id/networking
func (h *Handler) GetServiceNetworking(c *gin.Context) {
	serviceID := c.Param("id")
	if serviceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "service_id is required"})
		return
	}

	ctx := c.Request.Context()

	// Parse and validate service ID
	serviceUUID, err := uuid.Parse(serviceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service_id"})
		return
	}

	// Get service
	service, err := h.repos.Services.GetByID(serviceUUID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service not found"})
		return
	}

	// Get all domains for this service
	domains, err := h.repos.CustomDomains.GetByServiceID(ctx, serviceID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get custom domains", logging.Error("error", err))
		domains = []types.CustomDomain{}
	}

	// Build domain info list with environment names
	domainInfos := make([]types.DomainInfo, 0, len(domains))

	requestedEnv := c.Query("env")
	var filteredDomains []types.CustomDomain

	for _, domain := range domains {
		// Get environment name
		env, err := h.repos.Environments.GetByID(ctx, domain.EnvironmentID)
		envName := "unknown"
		if err == nil && env != nil {
			envName = env.Name
		}

		if requestedEnv != "" && envName != requestedEnv {
			continue
		}

		filteredDomains = append(filteredDomains, domain)

		// Determine TLS status based on verification
		tlsStatus := "pending"
		if domain.Verified && domain.TLSEnabled {
			tlsStatus = "active"
		} else if domain.TLSEnabled {
			tlsStatus = "provisioning"
		}

		// Generate verification TXT record value
		verificationTXT := fmt.Sprintf("enclii-verification=%s", domain.ID.String())

		domainInfo := types.DomainInfo{
			ID:               domain.ID,
			Domain:           domain.Domain,
			Environment:      envName,
			EnvironmentID:    domain.EnvironmentID,
			IsPlatformDomain: domain.IsPlatformDomain,
			Status:           domain.Status,
			TLSStatus:        tlsStatus,
			TLSProvider:      domain.TLSProvider,
			ZeroTrustEnabled: domain.ZeroTrustEnabled,
			DNSVerifiedAt:    domain.VerifiedAt,
			VerificationTXT:  verificationTXT,
			DNSCNAME:         domain.DNSCNAME,
			CreatedAt:        domain.CreatedAt,

			// Surface the provisioning diagnosis: a domain that cannot be
			// provisioned at all must not read as merely "pending".
			ProvisioningError:     domain.ProvisioningError,
			ProvisioningCheckedAt: domain.ProvisioningCheckedAt,
			PendingDNSRecords:     domain.PendingDNSRecords,
		}

		// For unverified custom domains, always include verification info
		if !domain.IsPlatformDomain && !domain.Verified {
			domainInfo.VerificationTXT = verificationTXT
		}

		domainInfos = append(domainInfos, domainInfo)
	}

	// Build internal routes from routes table
	// Note: Routes require environment context
	routes := []types.Route{}
	if len(filteredDomains) > 0 {
		// Use the first domain's environment for routes lookup
		envRoutes, err := h.repos.Routes.GetByServiceAndEnvironment(ctx, serviceID, filteredDomains[0].EnvironmentID.String())
		if err != nil {
			h.logger.Error(ctx, "Failed to get routes", logging.Error("error", err))
		} else {
			routes = envRoutes
		}
	}

	internalRoutes := make([]types.InternalRoute, 0, len(routes))
	for _, route := range routes {
		internalRoutes = append(internalRoutes, types.InternalRoute{
			Path:          route.Path,
			TargetService: fmt.Sprintf("%s.%s.svc.cluster.local", service.Name, service.ProjectID.String()[:8]),
			TargetPort:    route.Port,
		})
	}

	// If no routes defined, provide default
	if len(internalRoutes) == 0 {
		internalRoutes = append(internalRoutes, types.InternalRoute{
			Path:          "/*",
			TargetService: fmt.Sprintf("%s.enclii.svc.cluster.local", service.Name),
			TargetPort:    8080,
		})
	}

	// Build tunnel status from Cloudflare if available
	var tunnelStatus *types.TunnelStatusInfo
	platformDomain := os.Getenv("ENCLII_PLATFORM_DOMAIN")
	if platformDomain == "" {
		platformDomain = "enclii.dev"
	}

	// Query real tunnel status from Cloudflare if service is configured
	if h.domainSyncService != nil {
		cfTunnelStatus, err := h.domainSyncService.GetTunnelStatus(ctx)
		if err == nil && cfTunnelStatus != nil {
			// Map Cloudflare status to types.TunnelStatus
			status := types.TunnelStatusOffline
			switch cfTunnelStatus.Status {
			case "active":
				status = types.TunnelStatusActive
			case "degraded":
				status = types.TunnelStatusDegraded
			case "inactive":
				status = types.TunnelStatusOffline
			}

			tunnelStatus = &types.TunnelStatusInfo{
				TunnelID:   cfTunnelStatus.TunnelID,
				TunnelName: cfTunnelStatus.TunnelName,
				Status:     status,
				CNAME:      fmt.Sprintf("tunnel.%s", platformDomain),
				Connectors: cfTunnelStatus.ActiveConnectors,
			}
		} else {
			// Log error but don't fail the request
			h.logger.Warn(ctx, "Failed to get tunnel status from Cloudflare, using fallback",
				logging.Error("error", err))
		}
	}

	// Fallback to static status if Cloudflare integration not available
	if tunnelStatus == nil {
		tunnelStatus = &types.TunnelStatusInfo{
			TunnelID:   "production-tunnel",
			TunnelName: "enclii-prod",
			Status:     types.TunnelStatusActive,
			CNAME:      fmt.Sprintf("tunnel.%s", platformDomain),
			Connectors: 3,
		}
	}

	networking := types.ServiceNetworking{
		ServiceID:      serviceUUID,
		ServiceName:    service.Name,
		Domains:        domainInfos,
		InternalRoutes: internalRoutes,
		TunnelStatus:   tunnelStatus,
	}

	c.JSON(http.StatusOK, networking)
}

// AddDomainRequest is the request body for adding a domain
type AddDomainRequest struct {
	Domain           string `json:"domain"`
	EnvironmentID    string `json:"environment_id" binding:"required"`
	IsPlatformDomain bool   `json:"is_platform_domain"`
	TLSProvider      string `json:"tls_provider"`
	ZeroTrustEnabled bool   `json:"zero_trust_enabled"`
}

// AddServiceDomain adds a domain to a service (enhanced version)
// POST /api/v1/services/:id/domains
func (h *Handler) AddServiceDomain(c *gin.Context) {
	serviceID := c.Param("id")
	if serviceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "service_id is required"})
		return
	}

	var req AddDomainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()

	// Validate service exists
	serviceUUID, err := uuid.Parse(serviceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service_id"})
		return
	}

	service, err := h.repos.Services.GetByID(serviceUUID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service not found"})
		return
	}

	// Parse environment ID
	envUUID, err := uuid.Parse(req.EnvironmentID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid environment_id"})
		return
	}

	// Get environment
	env, err := h.repos.Environments.GetByID(ctx, envUUID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "environment not found"})
		return
	}

	// Generate domain name for platform domains
	domainName := req.Domain
	if req.IsPlatformDomain {
		platformDomain := os.Getenv("ENCLII_PLATFORM_DOMAIN")
		if platformDomain == "" {
			platformDomain = "enclii.dev"
		}
		// Generate subdomain: {service}-{env}.{platform}
		if domainName == "" {
			domainName = fmt.Sprintf("%s-%s.%s", service.Name, env.Name, platformDomain)
		} else if !strings.HasSuffix(domainName, platformDomain) {
			domainName = fmt.Sprintf("%s.%s", domainName, platformDomain)
		}
	}

	// Canonicalise before validating: the validated value is the stored value.
	domainName = canonicalDomain(domainName)

	// Validate domain format
	if !isValidDomain(domainName) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid domain format"})
		return
	}

	// Check if domain is already in use
	exists, err := h.repos.CustomDomains.Exists(ctx, domainName)
	if err != nil {
		h.logger.Error(ctx, "Failed to check domain existence", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	if exists {
		domains, listErr := h.repos.CustomDomains.GetByServiceID(ctx, serviceID)
		if listErr != nil {
			h.logger.Error(ctx, "Failed to list service domains for existing-domain reconciliation",
				logging.String("domain", domainName),
				logging.Error("error", listErr))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to inspect existing domain"})
			return
		}
		for _, existing := range domains {
			if !strings.EqualFold(existing.Domain, domainName) {
				continue
			}
			if existing.EnvironmentID != envUUID {
				c.JSON(http.StatusConflict, gin.H{"error": "domain already registered for a different environment"})
				return
			}

			// BOTH halves, not just the tunnel: `domains add` used to register
			// the row and print manual DNS instructions while creating neither
			// the record nor the route (issue #386 — bit eido on 2026-07-10 and
			// nauta on 2026-08-16, both hand-fixed under break-glass). The
			// deploy path already had provisionDomainEdge; the CLI path now
			// takes the same door.
			edge := h.provisionDomainEdge(ctx, domainName, service, env.Name, 80, nil)
			ready := false
			if h.tunnelRoutesService != nil {
				ready, _ = h.tunnelRoutesService.RouteExists(ctx, domainName)
			}

			c.JSON(http.StatusOK, gin.H{
				"domain":             existing,
				"message":            fmt.Sprintf("Domain %s already exists. Edge reconciliation requested.", domainName),
				"tunnel_route_added": ready,
				"reconciled":         true,
				"provisioning":       edge,
			})
			return
		}

		c.JSON(http.StatusConflict, gin.H{"error": "domain already in use"})
		return
	}

	// Set defaults
	tlsProvider := req.TLSProvider
	if tlsProvider == "" {
		tlsProvider = types.TLSProviderCertManager
	}

	tlsIssuer := "letsencrypt-staging"
	if env.Name == "production" {
		tlsIssuer = "letsencrypt-prod"
	}

	// Determine initial status
	status := types.DomainStatusPending
	verified := false
	if req.IsPlatformDomain {
		// Platform domains are auto-verified
		status = types.DomainStatusActive
		verified = true
	}

	// Get tunnel CNAME for DNS instructions
	platformDomain := os.Getenv("ENCLII_PLATFORM_DOMAIN")
	if platformDomain == "" {
		platformDomain = "enclii.dev"
	}
	dnsCNAME := fmt.Sprintf("tunnel.%s", platformDomain)

	// Create custom domain
	domain := &types.CustomDomain{
		ServiceID:        serviceUUID,
		EnvironmentID:    envUUID,
		Domain:           domainName,
		Verified:         verified,
		TLSEnabled:       true,
		TLSIssuer:        tlsIssuer,
		IsPlatformDomain: req.IsPlatformDomain,
		ZeroTrustEnabled: req.ZeroTrustEnabled,
		TLSProvider:      tlsProvider,
		Status:           status,
		DNSCNAME:         dnsCNAME,
	}

	// Checked and claimed in one transaction, under the cross-project hostname
	// lock. The Exists check above only sees custom_domains; a junction-served
	// hostname has no row there, and a concurrent claim for another project can
	// land between that check and this insert. See AddCustomDomain for why the
	// gate inside also fails closed rather than reading an unresolvable lookup
	// as "free".
	if err := h.claimHostname(ctx, domain, &domainOwner{
		ProjectID: service.ProjectID,
		ServiceID: serviceUUID,
	}); err != nil {
		if isHostnameHeld(err) {
			h.logger.Warn(ctx, "Refusing a service domain for a hostname another project holds",
				logging.String("domain", domainName),
				logging.Error("error", err))
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		h.logger.Error(ctx, "Failed to create custom domain", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create custom domain"})
		return
	}

	// Trigger reconciliation for platform domains
	if req.IsPlatformDomain {
		go h.triggerDomainReconciliation(ctx, serviceUUID, envUUID)
	}

	// Establish the edge NOW — tunnel ingress rule plus the DNS record (zone
	// CNAME for hostnames in zones this account controls, custom hostname for
	// client-owned domains). Before this call, `domains add` wrote the row and
	// answered with manual instructions while zone records stayed at zero and
	// the tunnel config never mentioned the host (issue #386). The manifest
	// deploy path has taken this exact door all along; the mechanism decision
	// (zone vs SaaS) and the zone RESOLUTION-BY-HOSTNAME both live inside it,
	// so a madfam.io host can never be written into the enclii.dev zone.
	edge := h.provisionDomainEdge(ctx, domainName, service, env.Name, 80, nil)

	// Build response
	response := gin.H{
		"domain":       domain,
		"message":      "Domain added successfully",
		"provisioning": edge,
	}

	// Manual DNS instructions remain ONLY for the case that still needs a
	// human: a client-owned domain whose records we cannot write. A hostname
	// the edge just provisioned gets no homework.
	if !req.IsPlatformDomain && edge.WaitingOnClient() {
		response["dns_instructions"] = gin.H{
			"verification": gin.H{
				"type":  "TXT",
				"name":  fmt.Sprintf("_enclii-verification.%s", strings.Split(domainName, ".")[0]),
				"value": fmt.Sprintf("enclii-verification=%s", domain.ID.String()),
			},
			"cname": gin.H{
				"type":  "CNAME",
				"name":  strings.Split(domainName, ".")[0],
				"value": dnsCNAME,
			},
		}
	}

	c.JSON(http.StatusCreated, response)
}

// ToggleZeroTrust enables or disables Zero Trust protection for a domain
// PUT /api/v1/domains/:domain_id/protection
func (h *Handler) ToggleZeroTrust(c *gin.Context) {
	domainIDStr := c.Param("domain_id")
	if domainIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "domain_id is required"})
		return
	}

	domainUUID, err := uuid.Parse(domainIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid domain_id"})
		return
	}

	var req struct {
		ZeroTrustEnabled bool `json:"zero_trust_enabled"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()

	domain, ok := h.loadCustomDomainWithAccess(c, domainUUID)
	if !ok {
		return
	}

	// Update Zero Trust setting
	domain.ZeroTrustEnabled = req.ZeroTrustEnabled

	// Create or delete Cloudflare Access application for this domain
	if h.domainSyncService != nil {
		cfClient := h.domainSyncService.GetCloudflareClient()
		if cfClient != nil {
			if req.ZeroTrustEnabled {
				app, err := cfClient.CreateAccessApplication(ctx,
					fmt.Sprintf("enclii-%s", domain.Domain), domain.Domain)
				if err != nil {
					h.logger.Error(ctx, "Failed to create Access application",
						logging.Error("error", err))
					c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create Access application"})
					return
				}
				domain.AccessPolicyID = app.ID
			} else if domain.AccessPolicyID != "" {
				if err := cfClient.DeleteAccessApplication(ctx, domain.AccessPolicyID); err != nil {
					h.logger.Warn(ctx, "Failed to delete Access application (may already be deleted)",
						logging.Error("error", err))
				}
				domain.AccessPolicyID = ""
			}
		} else {
			h.logger.Warn(ctx, "Cloudflare client not available, skipping Access policy management")
		}
	} else {
		h.logger.Warn(ctx, "Domain sync service not configured, skipping Access policy management")
	}

	if err := h.repos.CustomDomains.Update(ctx, domain); err != nil {
		h.logger.Error(ctx, "Failed to update domain protection", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update domain"})
		return
	}

	// Trigger reconciliation
	go h.triggerDomainReconciliation(ctx, domain.ServiceID, domain.EnvironmentID)

	c.JSON(http.StatusOK, gin.H{
		"domain":  domain,
		"message": fmt.Sprintf("Zero Trust protection %s", map[bool]string{true: "enabled", false: "disabled"}[req.ZeroTrustEnabled]),
	})
}

// GetEnvironments returns all environments for domain selection
// GET /api/v1/environments
func (h *Handler) GetEnvironments(c *gin.Context) {
	ctx := c.Request.Context()

	// Get project ID from query if provided
	projectID := c.Query("project_id")

	var environments []*types.Environment
	var err error

	if projectID != "" {
		projectUUID, parseErr := uuid.Parse(projectID)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project_id"})
			return
		}
		environments, err = h.repos.Environments.ListByProject(projectUUID)
	} else {
		// List environments across all projects - requires listing all projects first
		// For now, return empty if no project_id specified (API requires project context)
		c.JSON(http.StatusBadRequest, gin.H{"error": "project_id query parameter is required"})
		return
	}

	if err != nil {
		h.logger.Error(ctx, "Failed to list environments", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list environments"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"environments": environments})
}
