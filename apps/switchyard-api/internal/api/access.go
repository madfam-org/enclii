package api

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	apperrors "github.com/madfam-org/enclii/apps/switchyard-api/internal/errors"
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
		respondAppError(c, apperrors.ErrNotFound)
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
		respondAppError(c, apperrors.ErrUnauthorized)
		return false
	}

	if h.repos == nil || h.repos.ProjectAccess == nil {
		respondAppError(c, apperrors.ErrInternal.WithDetails(map[string]string{"reason": "authorization unavailable"}))
		return false
	}

	hasAccess, err := h.repos.ProjectAccess.UserHasAccess(c.Request.Context(), userID, projectID)
	if err != nil {
		h.logger.Error(c.Request.Context(), "Failed to verify project access", logging.Error("error", err))
		respondAppError(c, apperrors.ErrInternal.WithError(err))
		return false
	}
	if !hasAccess {
		respondAppError(c, apperrors.ErrNotFound)
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
			respondAppError(c, apperrors.ErrProjectNotFound)
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
		respondAppError(c, apperrors.ErrServiceNotFound)
		return false
	}
	return h.enforceUserProjectAccess(c, svc.ProjectID)
}
