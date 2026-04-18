package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/export"
)

// Tenant data export API (P3.6).
//
// Endpoints:
//   POST   /v1/projects/:slug/exports           initiate an export
//   GET    /v1/projects/:slug/exports           list history (90d)
//   GET    /v1/exports/:export_id               status + fresh pre-signed URL
//   POST   /v1/exports/:export_id/approve       prod HITL approval
//   DELETE /v1/exports/:export_id               soft-delete + R2 purge
//
// When h.tenantExportService is nil (local-dev without a BundleProvider
// wired), the routes return 503 Service Unavailable so we don't
// fabricate failures that confuse operators.

// SetTenantExportService wires the tenant export service. Optional: when
// nil, the tenant export endpoints return 503.
func (h *Handler) SetTenantExportService(svc *export.Service) {
	h.tenantExportService = svc
}

// CreateTenantExport — POST /v1/projects/:slug/exports
//
// Response: 202 Accepted with the TenantExport row. In prod the row comes
// back with status=pending (HITL); in non-prod it's status=running and
// the pipeline is already churning.
func (h *Handler) CreateTenantExport(c *gin.Context) {
	if h.tenantExportService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "tenant export service not configured on this deployment",
		})
		return
	}

	slug := c.Param("slug")
	if slug == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project slug required"})
		return
	}

	req, err := buildInitiateRequest(c, slug)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	row, err := h.tenantExportService.Initiate(c.Request.Context(), req)
	if err != nil {
		if errors.Is(err, export.ErrUnauthorizedExport) {
			c.JSON(http.StatusForbidden, gin.H{"error": "project admin required"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, row)
}

// ListTenantExports — GET /v1/projects/:slug/exports
func (h *Handler) ListTenantExports(c *gin.Context) {
	if h.tenantExportService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "tenant export service not configured"})
		return
	}

	slug := c.Param("slug")
	req, err := buildInitiateRequest(c, slug)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	rows, err := h.tenantExportService.List(c.Request.Context(), req, slug)
	if err != nil {
		if errors.Is(err, export.ErrUnauthorizedExport) {
			c.JSON(http.StatusForbidden, gin.H{"error": "no access to project"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"exports": rows})
}

// GetTenantExport — GET /v1/exports/:export_id
//
// Returns the row plus a freshly pre-signed download URL when
// status=ready. Each call regenerates the URL (15-minute TTL).
func (h *Handler) GetTenantExport(c *gin.Context) {
	if h.tenantExportService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "tenant export service not configured"})
		return
	}

	id, err := uuid.Parse(c.Param("export_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid export id"})
		return
	}

	req, err := buildInitiateRequest(c, "")
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.tenantExportService.Get(c.Request.Context(), req, id)
	if err != nil {
		if errors.Is(err, export.ErrExportNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "export not found"})
			return
		}
		if errors.Is(err, export.ErrUnauthorizedExport) {
			c.JSON(http.StatusForbidden, gin.H{"error": "project admin required"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// ApproveTenantExport — POST /v1/exports/:export_id/approve
//
// Production-only HITL approval. A second project admin (distinct from
// the requester) approves, flipping status=pending -> running. Non-prod
// installations can still call this on manually-pending rows if they
// wish.
func (h *Handler) ApproveTenantExport(c *gin.Context) {
	if h.tenantExportService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "tenant export service not configured"})
		return
	}

	id, err := uuid.Parse(c.Param("export_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid export id"})
		return
	}

	req, err := buildInitiateRequest(c, "")
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	row, err := h.tenantExportService.Approve(c.Request.Context(), req, id)
	if err != nil {
		if errors.Is(err, export.ErrExportNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "export not found"})
			return
		}
		if errors.Is(err, export.ErrUnauthorizedExport) {
			c.JSON(http.StatusForbidden, gin.H{"error": "project admin required"})
			return
		}
		if errors.Is(err, export.ErrSelfApproval) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "approver must differ from requester in production"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, row)
}

// DeleteTenantExport — DELETE /v1/exports/:export_id
//
// Soft-deletes the row and purges the R2 tarball immediately. The row
// itself retains for the usual 90 days for audit.
func (h *Handler) DeleteTenantExport(c *gin.Context) {
	if h.tenantExportService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "tenant export service not configured"})
		return
	}

	id, err := uuid.Parse(c.Param("export_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid export id"})
		return
	}

	req, err := buildInitiateRequest(c, "")
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	if err := h.tenantExportService.Delete(c.Request.Context(), req, id); err != nil {
		if errors.Is(err, export.ErrExportNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "export not found"})
			return
		}
		if errors.Is(err, export.ErrUnauthorizedExport) {
			c.JSON(http.StatusForbidden, gin.H{"error": "project admin required"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "export deleted"})
}

// buildInitiateRequest constructs the service-layer auth/context struct
// from the gin context. Used by every tenant-export handler so auth
// extraction stays uniform.
func buildInitiateRequest(c *gin.Context, projectSlug string) (export.InitiateRequest, error) {
	userID, err := auth.GetUserIDFromContext(c)
	if err != nil {
		return export.InitiateRequest{}, err
	}
	email, _ := auth.GetUserEmailFromContext(c)
	role := c.GetString("role")
	if role == "" {
		role = c.GetString("user_role")
	}

	return export.InitiateRequest{
		ProjectSlug: projectSlug,
		UserID:      userID,
		UserEmail:   email,
		UserRole:    role,
	}, nil
}
