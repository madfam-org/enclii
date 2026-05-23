package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/middleware"
)

// callerIsPlatformAdmin returns true when the authenticated principal has the admin role.
func callerIsPlatformAdmin(c *gin.Context) bool {
	for _, role := range c.GetStringSlice("user_roles") {
		if role == "admin" {
			return true
		}
	}
	return false
}

func authenticatedUserID(c *gin.Context) (uuid.UUID, bool) {
	raw := c.GetString("user_id")
	if raw == "" {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// enforceUserProjectAccess ensures the caller may access projectID. Platform admins
// bypass membership checks unless they are acting-as a tenant (XC-2). Returns false
// when a response has already been written (404/403/500).
func (h *Handler) enforceUserProjectAccess(c *gin.Context, projectID uuid.UUID) bool {
	if projectID == uuid.Nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "resource not found"})
		return false
	}

	if _, isActing := middleware.ActingTeamID(c); isActing {
		return h.enforceActingTeamForProject(c, projectID)
	}

	if callerIsPlatformAdmin(c) {
		return true
	}

	userID, ok := authenticatedUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return false
	}

	if h.repos == nil || h.repos.ProjectAccess == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "authorization unavailable"})
		return false
	}

	hasAccess, err := h.repos.ProjectAccess.UserHasAccess(c.Request.Context(), userID, projectID)
	if err != nil {
		h.logger.Error(c.Request.Context(), "Failed to verify project access", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify project access"})
		return false
	}
	if !hasAccess {
		c.JSON(http.StatusNotFound, gin.H{"error": "resource not found"})
		return false
	}
	return true
}

// RequireProjectAccessBySlug is middleware for routes carrying :slug. Resolves the project,
// enforces membership (or admin / acting-as rules), and stores project_id in context.
func (h *Handler) RequireProjectAccessBySlug() gin.HandlerFunc {
	return func(c *gin.Context) {
		slug := c.Param("slug")
		if slug == "" {
			c.Next()
			return
		}

		project, err := h.repos.Projects.GetBySlug(slug)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
			c.Abort()
			return
		}

		if !h.enforceUserProjectAccess(c, project.ID) {
			c.Abort()
			return
		}

		c.Set("project_id", project.ID)
		c.Next()
	}
}

// enforceServiceAccess loads a service and applies project-level access control.
func (h *Handler) enforceServiceAccess(c *gin.Context, serviceID uuid.UUID) bool {
	svc, err := h.repos.Services.GetByID(serviceID)
	if err != nil || svc == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service not found"})
		return false
	}
	return h.enforceUserProjectAccess(c, svc.ProjectID)
}
