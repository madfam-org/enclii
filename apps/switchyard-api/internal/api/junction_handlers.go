package api

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// CreateJunctionRequest defines the request body for creating a junction
type CreateJunctionRequest struct {
	ServiceID string           `json:"service_id" binding:"required"`
	Domain    string           `json:"domain" binding:"required"`
	Path      string           `json:"path,omitempty"`
	Protocol  string           `json:"protocol,omitempty"`
	TLS       *types.TLSConfig `json:"tls,omitempty"`
}

// validProtocols are the allowed junction protocols
var validProtocols = map[string]bool{
	"http":  true,
	"https": true,
	"grpc":  true,
}

// validTLSIssuers are the allowed TLS issuers
var validTLSIssuers = map[string]bool{
	"letsencrypt-prod":    true,
	"letsencrypt-staging": true,
	"custom":              true,
	"selfsigned":          true,
}

// CreateJunction creates a new junction (routing rule) for a project
// POST /v1/projects/:slug/junctions
func (h *Handler) CreateJunction(c *gin.Context) {
	ctx := c.Request.Context()
	slug := c.Param("slug")

	var req CreateJunctionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate domain
	if err := validateDomain(req.Domain, false); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate protocol
	protocol := "https"
	if req.Protocol != "" {
		if !validProtocols[req.Protocol] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid protocol: must be 'http', 'https', or 'grpc'"})
			return
		}
		protocol = req.Protocol
	}

	// Validate TLS config
	if req.TLS != nil && req.TLS.Issuer != "" {
		if !validTLSIssuers[req.TLS.Issuer] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid TLS issuer"})
			return
		}
	}

	// Default path
	path := "/"
	if req.Path != "" {
		path = req.Path
	}

	// Get project
	project, err := h.repos.Projects.GetBySlug(slug)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
			return
		}
		h.logger.Error(ctx, "Failed to get project", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get project"})
		return
	}

	if _, err := h.ensureDefaultProductionEnvironment(ctx, project); err != nil {
		h.logger.Error(ctx, "Failed to ensure default environment for junction provisioning",
			logging.String("project", slug),
			logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to ensure default environment"})
		return
	}

	// Parse service ID
	serviceID, err := uuid.Parse(req.ServiceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service_id"})
		return
	}

	svc, err := h.repos.Services.GetByID(serviceID)
	if err != nil || svc == nil || svc.ProjectID != project.ID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "service does not belong to this project"})
		return
	}

	// Check uniqueness
	exists, err := h.repos.Junctions.ExistsByDomainPath(ctx, req.Domain, path)
	if err != nil {
		h.logger.Error(ctx, "Failed to check junction existence", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check junction existence"})
		return
	}
	if exists {
		c.JSON(http.StatusConflict, gin.H{"error": "junction for this domain+path already exists"})
		return
	}

	junction := &types.Junction{
		ProjectID: project.ID,
		ServiceID: serviceID,
		Domain:    req.Domain,
		Path:      path,
		Protocol:  protocol,
		TLS:       req.TLS,
	}

	// Set default TLS config
	if junction.TLS == nil {
		junction.TLS = &types.TLSConfig{
			Enabled:       true,
			Issuer:        "letsencrypt-prod",
			MinVersion:    "1.2",
			ForceRedirect: true,
		}
	}

	if err := h.repos.Junctions.Create(ctx, junction); err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			c.JSON(http.StatusConflict, gin.H{"error": "junction for this domain+path already exists"})
			return
		}
		h.logger.Error(ctx, "Failed to create junction", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create junction"})
		return
	}

	// Provision infrastructure: tunnel route + DNS CNAME
	// Get the service for namespace resolution
	var provisioning junctionProvisioningSummary
	var reconcile junctionRouteReconcileSummary
	service, svcErr := h.repos.Services.GetByID(serviceID)
	if svcErr == nil {
		provisioning = h.ensureJunctionInfrastructure(ctx, req.Domain, service)
		reconcile = h.reconcileJunctionTunnelRoutesForProject(ctx, project)
		h.scheduleJunctionTunnelRouteReconcile(project)
	} else {
		h.logger.Warn(ctx, "Service lookup failed during junction provisioning, skipping infra setup",
			logging.String("service_id", serviceID.String()),
			logging.Error("error", svcErr))
	}

	h.logger.Info(ctx, "Junction created",
		logging.String("domain", junction.Domain),
		logging.String("project", slug))

	c.JSON(http.StatusCreated, gin.H{
		"junction":     junction,
		"message":      "Junction created successfully",
		"provisioning": provisioning,
		"reconcile":    reconcile,
	})
}

// ListJunctions lists all junctions for a project
// GET /v1/projects/:slug/junctions
func (h *Handler) ListJunctions(c *gin.Context) {
	ctx := c.Request.Context()
	slug := c.Param("slug")

	project, err := h.repos.Projects.GetBySlug(slug)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
			return
		}
		h.logger.Error(ctx, "Failed to get project", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get project"})
		return
	}

	junctions, err := h.repos.Junctions.ListByProject(ctx, project.ID)
	if err != nil {
		h.logger.Error(ctx, "Failed to list junctions", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list junctions"})
		return
	}

	if junctions == nil {
		junctions = []*types.Junction{}
	}

	c.JSON(http.StatusOK, gin.H{
		"junctions": junctions,
		"total":     len(junctions),
	})
}

// GetJunction retrieves a single junction by ID
// GET /v1/junctions/:id
func (h *Handler) GetJunction(c *gin.Context) {
	ctx := c.Request.Context()

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid junction ID"})
		return
	}

	junction, err := h.repos.Junctions.GetByID(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "junction not found"})
			return
		}
		h.logger.Error(ctx, "Failed to get junction", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get junction"})
		return
	}

	if !h.enforceUserProjectAccess(c, junction.ProjectID) {
		return
	}

	c.JSON(http.StatusOK, junction)
}

// DeleteJunction deletes a junction and removes infrastructure
// DELETE /v1/junctions/:id
func (h *Handler) DeleteJunction(c *gin.Context) {
	ctx := c.Request.Context()

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid junction ID"})
		return
	}

	// Get junction first for cleanup
	junction, err := h.repos.Junctions.GetByID(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "junction not found"})
			return
		}
		h.logger.Error(ctx, "Failed to get junction", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get junction"})
		return
	}

	// Remove tunnel route (non-blocking)
	if h.tunnelRoutesService != nil {
		if rmErr := h.tunnelRoutesService.RemoveRoute(ctx, junction.Domain); rmErr != nil {
			h.logger.Warn(ctx, "Failed to remove tunnel route during junction deletion",
				logging.String("domain", junction.Domain),
				logging.Error("error", rmErr))
		}
	}

	// Release the Cloudflare for SaaS custom hostname, if the domain was
	// provisioned that way (non-blocking). Junctions store no hostname id, so
	// this looks it up by hostname on the fallback-origin zone.
	if delErr := h.deleteCustomHostnameByDomain(ctx, junction.Domain); delErr != nil {
		h.logger.Warn(ctx, "Failed to delete custom hostname during junction deletion",
			logging.String("domain", junction.Domain),
			logging.Error("error", delErr))
	}

	// Remove DNS record (non-blocking)
	if h.domainSyncService != nil {
		cfClient := h.domainSyncService.GetCloudflareClient()
		if cfClient != nil {
			record, lookupErr := cfClient.GetDNSRecord(ctx, junction.Domain)
			if lookupErr == nil && record != nil {
				zone, zoneErr := cfClient.FindZoneForDomain(ctx, junction.Domain)
				if zoneErr == nil {
					if delErr := cfClient.DeleteDNSRecordInZone(ctx, zone.ID, record.ID); delErr != nil {
						h.logger.Warn(ctx, "Failed to delete DNS record during junction deletion",
							logging.String("domain", junction.Domain),
							logging.Error("error", delErr))
					}
				}
			}
		}
	}

	// Delete from database
	if err := h.repos.Junctions.Delete(ctx, id); err != nil {
		h.logger.Error(ctx, "Failed to delete junction", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete junction"})
		return
	}

	h.logger.Info(ctx, "Junction deleted",
		logging.String("domain", junction.Domain),
		logging.String("id", id.String()))

	c.JSON(http.StatusOK, gin.H{"message": "junction deleted"})
}
