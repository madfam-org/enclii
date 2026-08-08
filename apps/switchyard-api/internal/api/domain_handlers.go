package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/reconciler"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// AddCustomDomain adds a custom domain to a service
// POST /api/v1/services/:id/domains
func (h *Handler) AddCustomDomain(c *gin.Context) {
	serviceID := c.Param("id")
	if serviceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "service_id is required"})
		return
	}

	var req struct {
		Domain      string `json:"domain" binding:"required"`
		Environment string `json:"environment" binding:"required"`
		TLSEnabled  bool   `json:"tls_enabled"`
		TLSIssuer   string `json:"tls_issuer"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Canonicalise before validating, so the value that is validated is the one
	// that is stored, looked up and compared against Cloudflare's zone names.
	req.Domain = canonicalDomain(req.Domain)

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

	// Get environment
	env, err := h.repos.Environments.GetByProjectAndName(service.ProjectID, req.Environment)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "environment not found"})
		return
	}

	// Validate domain format
	if err := validateDomain(req.Domain, false); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check if domain is already in use
	exists, err := h.repos.CustomDomains.Exists(ctx, req.Domain)
	if err != nil {
		h.logger.Error(ctx, "Failed to check domain existence", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	if exists {
		domains, listErr := h.repos.CustomDomains.GetByServiceID(ctx, serviceID)
		if listErr != nil {
			h.logger.Error(ctx, "Failed to list service domains for existing-domain reconciliation",
				logging.String("domain", req.Domain),
				logging.Error("error", listErr))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to inspect existing domain"})
			return
		}
		for _, existing := range domains {
			if !strings.EqualFold(existing.Domain, req.Domain) {
				continue
			}
			if existing.EnvironmentID != env.ID {
				c.JSON(http.StatusConflict, gin.H{"error": "domain already registered for a different environment"})
				return
			}

			h.ensureTunnelRoute(ctx, req.Domain, service, req.Environment, 80)
			ready := false
			if h.tunnelRoutesService != nil {
				ready, _ = h.tunnelRoutesService.RouteExists(ctx, req.Domain)
			}
			go h.triggerDomainReconciliation(ctx, serviceUUID, env.ID)

			c.JSON(http.StatusOK, gin.H{
				"domain":             existing,
				"message":            fmt.Sprintf("Custom domain %s already exists. Tunnel route reconciliation requested.", req.Domain),
				"tunnel_route_added": ready,
				"reconciled":         true,
			})
			return
		}

		c.JSON(http.StatusConflict, gin.H{"error": "domain already in use"})
		return
	}

	// Default TLS issuer
	tlsIssuer := req.TLSIssuer
	if tlsIssuer == "" {
		if req.Environment == "production" {
			tlsIssuer = "letsencrypt-prod"
		} else {
			tlsIssuer = "letsencrypt-staging"
		}
	}

	// Create custom domain
	domain := &types.CustomDomain{
		ServiceID:     serviceUUID,
		EnvironmentID: env.ID,
		Domain:        req.Domain,
		Verified:      false,
		TLSEnabled:    req.TLSEnabled,
		TLSIssuer:     tlsIssuer,
	}

	// Checked and claimed in one transaction, under the cross-project hostname
	// lock. The Exists check above only sees custom_domains and only saw it a
	// few statements ago: a hostname served through a junction has no row
	// there at all, and a concurrent request for another project can create one
	// in between. Both of those end with two projects holding one hostname,
	// which overwrites the other's routing and leaves the hostname permanently
	// contested for whichever of them is the rightful owner.
	if err := h.claimHostname(ctx, domain, &domainOwner{
		ProjectID: service.ProjectID,
		ServiceID: serviceUUID,
	}); err != nil {
		if isHostnameHeld(err) {
			h.logger.Warn(ctx, "Refusing a custom domain for a hostname another project holds",
				logging.String("domain", req.Domain),
				logging.Error("error", err))
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		h.logger.Error(ctx, "Failed to create custom domain", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create custom domain"})
		return
	}

	// Add tunnel route if tunnel routes service is configured
	tunnelRouteAdded := false
	if h.tunnelRoutesService != nil {
		// Connect/keepAlive timeouts intentionally omitted — see
		// domain_provisioner.go for the Cloudflare API quoted-string
		// rejection that motivated dropping these fields.
		routeSpec := &services.RouteSpec{
			Hostname:         req.Domain,
			ServiceName:      service.Name,
			ServiceNamespace: h.resolveServiceNamespace(ctx, service, req.Environment),
			ServicePort:      80, // K8s Service port (not container port)
		}

		if err := h.tunnelRoutesService.AddRoute(ctx, routeSpec); err != nil {
			h.logger.Warn(ctx, "Failed to add tunnel route (domain created, manual tunnel config may be needed)",
				logging.String("domain", req.Domain),
				logging.Error("error", err))
			// Don't fail the request - domain is created, tunnel route is optional
		} else {
			tunnelRouteAdded = true
			h.logger.Info(ctx, "Tunnel route added automatically",
				logging.String("domain", req.Domain),
				logging.String("service", service.Name))
		}
	}

	// Trigger reconciliation to create Ingress
	go h.triggerDomainReconciliation(ctx, serviceUUID, env.ID)

	responseMessage := fmt.Sprintf("Custom domain %s added.", req.Domain)
	if tunnelRouteAdded {
		responseMessage += " Tunnel route configured automatically."
	} else {
		responseMessage += " Configure your DNS to point to the tunnel."
	}

	c.JSON(http.StatusCreated, gin.H{
		"domain":             domain,
		"message":            responseMessage,
		"tunnel_route_added": tunnelRouteAdded,
	})
}

// ListCustomDomains lists all custom domains for a service
// GET /api/v1/services/:id/domains
func (h *Handler) ListCustomDomains(c *gin.Context) {
	serviceID := c.Param("id")
	if serviceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "service_id is required"})
		return
	}

	ctx := c.Request.Context()

	// XC-2 Round 5: 403 (rendered as 404) cross-tenant reads when acting-as.
	svcUUID, parseErr := uuid.Parse(serviceID)
	if parseErr == nil {
		if svc, sErr := h.repos.Services.GetByID(svcUUID); sErr == nil && svc != nil {
			if !h.enforceUserProjectAccess(c, svc.ProjectID) {
				return
			}
		}
	}

	domains, err := h.repos.CustomDomains.GetByServiceID(ctx, serviceID)
	if err != nil {
		h.logger.Error(ctx, "Failed to list custom domains", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list custom domains"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"domains": domains})
}

// GetCustomDomain gets a specific custom domain
// GET /api/v1/services/:id/domains/:domain_id
func (h *Handler) GetCustomDomain(c *gin.Context) {
	domainID := c.Param("domain_id")
	if domainID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "domain_id is required"})
		return
	}

	ctx := c.Request.Context()

	domain, err := h.repos.CustomDomains.GetByID(ctx, domainID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "custom domain not found"})
		return
	}

	// XC-2 Round 5: 404 cross-tenant detail reads when acting-as.
	if svc, sErr := h.repos.Services.GetByID(domain.ServiceID); sErr == nil && svc != nil {
		if !h.enforceUserProjectAccess(c, svc.ProjectID) {
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"domain": domain})
}

// UpdateCustomDomain updates a custom domain
// PATCH /api/v1/services/:id/domains/:domain_id
func (h *Handler) UpdateCustomDomain(c *gin.Context) {
	domainID := c.Param("domain_id")
	if domainID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "domain_id is required"})
		return
	}

	var req struct {
		TLSEnabled *bool   `json:"tls_enabled,omitempty"`
		TLSIssuer  *string `json:"tls_issuer,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()

	// Get existing domain
	domain, err := h.repos.CustomDomains.GetByID(ctx, domainID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "custom domain not found"})
		return
	}

	// Update fields
	if req.TLSEnabled != nil {
		domain.TLSEnabled = *req.TLSEnabled
	}
	if req.TLSIssuer != nil {
		domain.TLSIssuer = *req.TLSIssuer
	}

	if err := h.repos.CustomDomains.Update(ctx, domain); err != nil {
		h.logger.Error(ctx, "Failed to update custom domain", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update custom domain"})
		return
	}

	// Trigger reconciliation to update Ingress
	go h.triggerDomainReconciliation(ctx, domain.ServiceID, domain.EnvironmentID)

	c.JSON(http.StatusOK, gin.H{"domain": domain})
}

// DeleteCustomDomain removes a custom domain
// DELETE /api/v1/services/:id/domains/:domain_id
func (h *Handler) DeleteCustomDomain(c *gin.Context) {
	domainID := c.Param("domain_id")
	if domainID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "domain_id is required"})
		return
	}

	ctx := c.Request.Context()

	// Get domain to check service ownership
	domain, err := h.repos.CustomDomains.GetByID(ctx, domainID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "custom domain not found"})
		return
	}

	// Remove tunnel route if tunnel routes service is configured
	tunnelRouteRemoved := false
	if h.tunnelRoutesService != nil {
		if err := h.tunnelRoutesService.RemoveRoute(ctx, domain.Domain); err != nil {
			h.logger.Warn(ctx, "Failed to remove tunnel route (continuing with domain deletion)",
				logging.String("domain", domain.Domain),
				logging.Error("error", err))
			// Don't fail the request - continue with domain deletion
		} else {
			tunnelRouteRemoved = true
			h.logger.Info(ctx, "Tunnel route removed automatically",
				logging.String("domain", domain.Domain))
		}
	}

	// Release the Cloudflare for SaaS custom hostname, if any. Leaving it
	// behind would keep Cloudflare serving the client's domain and keep
	// consuming a custom hostname slot.
	customHostnameRemoved := false
	if domain.CustomHostnameID != "" {
		if err := h.deleteCustomHostname(ctx, domain.Domain, domain.CustomHostnameID); err != nil {
			h.logger.Warn(ctx, "Failed to delete custom hostname (continuing with domain deletion)",
				logging.String("domain", domain.Domain),
				logging.String("custom_hostname_id", domain.CustomHostnameID),
				logging.Error("error", err))
		} else {
			customHostnameRemoved = true
			h.logger.Info(ctx, "Custom hostname removed from Cloudflare",
				logging.String("domain", domain.Domain))
		}
	}

	// Delete domain
	if err := h.repos.CustomDomains.Delete(ctx, domainID); err != nil {
		h.logger.Error(ctx, "Failed to delete custom domain", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete custom domain"})
		return
	}

	// Trigger reconciliation to remove Ingress
	go h.triggerDomainReconciliation(ctx, domain.ServiceID, domain.EnvironmentID)

	response := gin.H{
		"message":              "custom domain deleted",
		"tunnel_route_removed": tunnelRouteRemoved,
	}
	if domain.CustomHostnameID != "" {
		response["custom_hostname_removed"] = customHostnameRemoved
	}
	c.JSON(http.StatusOK, response)
}

// VerifyCustomDomain verifies domain ownership via DNS TXT record
// POST /api/v1/services/:id/domains/:domain_id/verify
func (h *Handler) VerifyCustomDomain(c *gin.Context) {
	domainID := c.Param("domain_id")
	if domainID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "domain_id is required"})
		return
	}

	ctx := c.Request.Context()

	// Get domain
	domain, err := h.repos.CustomDomains.GetByID(ctx, domainID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "custom domain not found"})
		return
	}

	// Platform subdomains (*.enclii.dev) are auto-verified at creation time.
	if domain.IsPlatformDomain || domain.Verified {
		c.JSON(http.StatusOK, gin.H{
			"verified": true,
			"domain":   domain,
			"message":  "domain already verified",
		})
		return
	}

	// Cloudflare for SaaS domains are verified by Cloudflare, not by us: the
	// client proves ownership to Cloudflare with the records we handed them.
	// Re-read the real state instead of looking for an Enclii TXT record the
	// client was never asked to create.
	if domain.CustomHostnameID != "" {
		result := h.refreshCustomHostnameState(ctx, domain.Domain, domain.CustomHostnameID)
		applyProvisioningResult(domain, result, time.Now())

		if updateErr := h.repos.CustomDomains.UpdateCustomHostnameState(ctx, domain); updateErr != nil {
			h.logger.Error(ctx, "Failed to persist custom hostname verification state",
				logging.String("domain", domain.Domain),
				logging.Error("error", updateErr))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update domain"})
			return
		}

		if result.Err != nil {
			c.JSON(http.StatusBadGateway, gin.H{
				"verified": false,
				"domain":   domain,
				"error":    "failed to read custom hostname state from Cloudflare",
				"details":  result.ErrorMessage,
			})
			return
		}

		if !domain.Verified {
			c.JSON(http.StatusBadRequest, gin.H{
				"verified":            false,
				"domain":              domain,
				"error":               "domain not verified",
				"message":             describePendingClientAction(result),
				"pending_dns_records": result.PendingDNSRecords,
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"verified": true,
			"domain":   domain,
			"message":  "Cloudflare reports the custom hostname and its certificate as active",
		})
		return
	}

	// Check DNS TXT record
	expectedValue := fmt.Sprintf("enclii-verification=%s", domain.ID.String())
	verified, err := verifyDNSTXTRecord(domain.Domain, expectedValue)
	if err != nil {
		h.logger.Error(ctx, "Failed to verify DNS",
			logging.Error("error", err),
			logging.String("domain", domain.Domain))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "failed to verify DNS record",
			"details": err.Error(),
		})
		return
	}

	if !verified {
		c.JSON(http.StatusBadRequest, gin.H{
			"verified":           false,
			"error":              "domain not verified",
			"message":            fmt.Sprintf("Add a TXT record to %s with value: %s", domain.Domain, expectedValue),
			"verification_value": expectedValue,
		})
		return
	}

	// Mark as verified
	domain.Verified = true
	domain.Status = services.StatusVerified
	verifiedAt := time.Now()
	domain.VerifiedAt = &verifiedAt

	if err := h.repos.CustomDomains.Update(ctx, domain); err != nil {
		h.logger.Error(ctx, "Failed to update domain verification status", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update domain"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "domain verified successfully",
		"domain":  domain,
	})
}

// triggerDomainReconciliation triggers a reconciliation for a service with updated domains
func (h *Handler) triggerDomainReconciliation(ctx context.Context, serviceID, environmentID uuid.UUID) {
	// Get service
	service, err := h.repos.Services.GetByID(serviceID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get service for reconciliation", logging.Error("error", err))
		return
	}

	// Get latest deployment
	deployment, err := h.repos.Deployments.GetLatestByService(ctx, serviceID.String())
	if err != nil {
		h.logger.Warn(ctx, "No deployment found for service, skipping domain reconciliation", logging.Error("error", err))
		return
	}

	// Get release
	release, err := h.repos.Releases.GetByID(deployment.ReleaseID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get release for reconciliation", logging.Error("error", err))
		return
	}

	// Get custom domains and routes
	domains, err := h.repos.CustomDomains.GetByServiceAndEnvironment(ctx, serviceID.String(), environmentID.String())
	if err != nil {
		h.logger.Error(ctx, "Failed to get custom domains", logging.Error("error", err))
		domains = []types.CustomDomain{} // Continue with empty domains
	}

	routes, err := h.repos.Routes.GetByServiceAndEnvironment(ctx, serviceID.String(), environmentID.String())
	if err != nil {
		h.logger.Error(ctx, "Failed to get routes", logging.Error("error", err))
		routes = []types.Route{} // Continue with empty routes
	}

	// Get environment variables (decrypted)
	var envVars map[string]string
	if h.repos.EnvVars != nil {
		envVars, err = h.repos.EnvVars.GetDecrypted(ctx, serviceID, deployment.EnvironmentID)
		if err != nil {
			h.logger.Warn(ctx, "Failed to get environment variables", logging.Error("error", err))
			envVars = make(map[string]string)
		}
	} else {
		envVars = make(map[string]string)
	}

	// Reconcile
	reconcileReq := &reconciler.ReconcileRequest{
		Service:       service,
		Release:       release,
		Deployment:    deployment,
		CustomDomains: domains,
		Routes:        routes,
		EnvVars:       envVars,
	}

	result := h.serviceReconciler.Reconcile(ctx, reconcileReq)
	if !result.Success {
		h.logger.Error(ctx, "Failed to reconcile service with custom domains",
			logging.String("service", service.Name),
			logging.Error("error", result.Error))
	} else {
		h.logger.Info(ctx, "Successfully reconciled service with custom domains",
			logging.String("service", service.Name))
	}
}

// Note: triggerDomainReconciliation uses the existing reconciler.Controller
// which contains the ServiceReconciler needed for reconciling service changes
