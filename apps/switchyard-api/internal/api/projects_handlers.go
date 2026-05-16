package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/errors"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/middleware"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/monitoring"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
)

// CreateProject creates a new project for the authenticated user.
//
// Projects are the top-level organizational unit in Enclii. Each project
// can contain multiple services, environments, and team members.
//
// Request:
//   - Method: POST /api/v1/projects
//   - Authorization: Bearer <access_token>
//   - Content-Type: application/json
//   - Body: {name: string, slug: string, description?: string}
//
// Response:
//   - 201 Created: Project object
//   - 400 Bad Request: Invalid request body or validation error
//   - 409 Conflict: Project with slug already exists
//   - 500 Internal Server Error: Failed to create project
func (h *Handler) CreateProject(c *gin.Context) {
	ctx := c.Request.Context()
	var req struct {
		Name        string `json:"name" binding:"required"`
		Slug        string `json:"slug" binding:"required"`
		Description string `json:"description"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// Use service layer for project creation
	createReq := &services.CreateProjectRequest{
		Name:      req.Name,
		Slug:      req.Slug,
		UserID:    c.GetString("user_id"),
		UserEmail: c.GetString("user_email"),
		UserRole:  c.GetString("user_role"),
	}

	resp, err := h.projectService.CreateProject(ctx, createReq)
	if err != nil {
		// Map service errors to HTTP status codes
		if errors.Is(err, errors.ErrSlugAlreadyExists) {
			c.JSON(http.StatusConflict, gin.H{"error": "A project with this slug already exists"})
		} else if errors.Is(err, errors.ErrValidation) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
			h.logger.Error(ctx, "Failed to create project", logging.Error("error", err), logging.String("user_email", createReq.UserEmail), logging.String("project_name", createReq.Name))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create project"})
		}
		return
	}

	if _, err := h.ensureDefaultProductionEnvironment(ctx, resp.Project); err != nil {
		h.logger.Warn(ctx, "Failed to ensure default production environment after project creation",
			logging.Error("error", err),
			logging.String("project_slug", req.Slug))
	}

	// Clear project cache on creation
	if h.cache != nil {
		if err := h.cache.InvalidateTags(ctx, "projects"); err != nil {
			h.logger.Warn(ctx, "Failed to invalidate project cache", logging.Error("error", err))
		}
	}

	// Record project creation metric
	monitoring.RecordProjectCreated()

	c.JSON(http.StatusCreated, resp.Project)
}

// ListProjects returns all projects accessible to the authenticated user.
//
// This endpoint returns projects based on user's team memberships and permissions.
// Results may be cached for performance.
//
// Request:
//   - Method: GET /api/v1/projects
//   - Authorization: Bearer <access_token>
//
// Response:
//   - 200 OK: {projects: Project[]}
//   - 500 Internal Server Error: Failed to list projects
func (h *Handler) ListProjects(c *gin.Context) {
	ctx := c.Request.Context()

	// XC-2: when the master admin is acting-as a tenant, filter the
	// platform-wide list down to that tenant's projects. The middleware
	// (middleware.ActingAsMiddleware) sets acting_team_id when an open
	// session exists; absent it, behavior is unchanged.
	var teamID *uuid.UUID
	if id, ok := middleware.ActingTeamID(c); ok {
		teamID = &id
	}

	projects, err := h.projectService.ListProjectsScoped(ctx, teamID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list projects"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"projects": projects})
}

// GetProject returns a project by its unique slug.
//
// This endpoint retrieves a single project's details including its
// configuration, team members, and associated resources.
//
// Request:
//   - Method: GET /api/v1/projects/:slug
//   - Authorization: Bearer <access_token>
//   - Path Parameters: slug (string) - Project slug
//
// Response:
//   - 200 OK: Project object
//   - 404 Not Found: Project not found
//   - 500 Internal Server Error: Failed to get project
func (h *Handler) GetProject(c *gin.Context) {
	ctx := c.Request.Context()
	slug := c.Param("slug")

	// Use service layer for getting project
	project, err := h.projectService.GetProject(ctx, slug)
	if err != nil {
		if errors.Is(err, errors.ErrProjectNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get project"})
		}
		return
	}

	c.JSON(http.StatusOK, project)
}

// DeleteProject deletes a project and all associated resources.
//
// Projects can only be deleted by administrators. All services, environments,
// and other resources associated with the project are automatically deleted
// via database cascading deletes.
//
// Request:
//   - Method: DELETE /api/v1/projects/:slug
//   - Authorization: Bearer <access_token> (Admin role required)
//   - Path Parameters: slug (string) - Project slug
//
// Response:
//   - 200 OK: {message: "Project deleted successfully"}
//   - 404 Not Found: Project not found
//   - 500 Internal Server Error: Failed to delete project
func (h *Handler) DeleteProject(c *gin.Context) {
	ctx := c.Request.Context()
	slug := c.Param("slug")

	// Get project first to verify it exists and get its ID
	project, err := h.projectService.GetProject(ctx, slug)
	if err != nil {
		if errors.Is(err, errors.ErrProjectNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
		} else {
			h.logger.Error(ctx, "Failed to get project for deletion",
				logging.Error("error", err),
				logging.String("project_slug", slug))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get project"})
		}
		return
	}

	// Delete the project (CASCADE will handle related records)
	if err := h.repos.Projects.Delete(ctx, project.ID); err != nil {
		h.logger.Error(ctx, "Failed to delete project",
			logging.Error("error", err),
			logging.String("project_slug", slug),
			logging.String("project_id", project.ID.String()))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete project"})
		return
	}

	h.logger.Info(ctx, "Project deleted",
		logging.String("project_id", project.ID.String()),
		logging.String("project_slug", slug),
		logging.String("deleted_by", c.GetString("user_email")))

	c.JSON(http.StatusOK, gin.H{"message": "Project deleted successfully"})
}
