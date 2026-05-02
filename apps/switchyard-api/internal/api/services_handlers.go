package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/errors"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/middleware"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// enforceActingTeamForProject is the shared 403 guard for per-resource detail
// endpoints in the XC-2 tenant-filter rollout (Round 5). When the caller is
// acting-as a tenant, any resource whose owning project does not belong to
// that tenant must be invisible — we return 404 (not 403) so a master admin
// scoped into "tenant A" can't fingerprint "tenant B" resources by id.
//
// Returns true when the request should proceed; false when the handler has
// already written a response (404 / 500) and must abort. ProjectID may be
// uuid.Nil for resources we couldn't resolve to a project — in that case we
// fail closed (treat as mismatch) only when the caller is acting-as.
func (h *Handler) enforceActingTeamForProject(c *gin.Context, projectID uuid.UUID) bool {
	actingTeamID, ok := middleware.ActingTeamID(c)
	if !ok {
		return true
	}
	if projectID == uuid.Nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "resource not found"})
		return false
	}
	teamID, err := h.repos.Projects.GetTeamID(c.Request.Context(), projectID)
	if err != nil {
		// Either the project disappeared or the lookup failed — either way
		// we cannot prove tenant ownership, so refuse the read. 404 keeps
		// the impersonation surface opaque.
		c.JSON(http.StatusNotFound, gin.H{"error": "resource not found"})
		return false
	}
	if teamID != actingTeamID {
		c.JSON(http.StatusNotFound, gin.H{"error": "resource not found"})
		return false
	}
	return true
}

// CreateService creates a new service in a project.
//
// Services are deployable units within a project. Each service is linked to
// a git repository and can be deployed to multiple environments.
//
// Request:
//   - Method: POST /api/v1/projects/:slug/services
//   - Authorization: Bearer <access_token>
//   - Path Parameters: slug (string) - Project slug
//   - Body: {name: string, git_repo: string, build_config?: BuildConfig}
//
// Response:
//   - 201 Created: Service object
//   - 400 Bad Request: Invalid request body
//   - 404 Not Found: Project not found
//   - 500 Internal Server Error: Failed to create service
func (h *Handler) CreateService(c *gin.Context) {
	ctx := c.Request.Context()
	slug := c.Param("slug")

	// Get project first to get project ID
	project, err := h.projectService.GetProject(ctx, slug)
	if err != nil {
		if errors.Is(err, errors.ErrProjectNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get project"})
		}
		return
	}

	var req struct {
		Name        string            `json:"name" binding:"required"`
		GitRepo     string            `json:"git_repo" binding:"required"`
		Type        types.ServiceType `json:"type"`
		Region      string            `json:"region"`
		BuildConfig types.BuildConfig `json:"build_config"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Use service layer for service creation
	createReq := &services.CreateServiceRequest{
		ProjectID:   project.ID.String(),
		Name:        req.Name,
		GitRepo:     req.GitRepo,
		Type:        req.Type,
		Region:      req.Region,
		BuildConfig: req.BuildConfig,
		UserID:      c.GetString("user_id"),
		UserEmail:   c.GetString("user_email"),
		UserRole:    c.GetString("user_role"),
	}

	resp, err := h.projectService.CreateService(ctx, createReq)
	if err != nil {
		// Map service errors to HTTP status codes
		if errors.Is(err, errors.ErrProjectNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
		} else if errors.Is(err, errors.ErrValidation) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create service"})
		}
		return
	}

	c.JSON(http.StatusCreated, resp.Service)
}

// ListServices returns all services in a project.
//
// This endpoint lists all services within a project, including their
// current deployment status and configuration.
//
// Request:
//   - Method: GET /api/v1/projects/:slug/services
//   - Authorization: Bearer <access_token>
//   - Path Parameters: slug (string) - Project slug
//
// Response:
//   - 200 OK: {services: Service[]}
//   - 404 Not Found: Project not found
//   - 500 Internal Server Error: Failed to list services
func (h *Handler) ListServices(c *gin.Context) {
	ctx := c.Request.Context()
	slug := c.Param("slug")

	// Use service layer for listing services
	svcList, err := h.projectService.ListServices(ctx, slug)
	if err != nil {
		if errors.Is(err, errors.ErrProjectNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list services"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"services": svcList})
}

// GetService returns a service by its unique ID.
//
// This endpoint retrieves detailed information about a specific service,
// including its configuration, build settings, and deployment history.
//
// Request:
//   - Method: GET /api/v1/services/:id
//   - Authorization: Bearer <access_token>
//   - Path Parameters: id (string) - Service ID (UUID)
//
// Response:
//   - 200 OK: Service object
//   - 404 Not Found: Service not found
//   - 500 Internal Server Error: Failed to get service
func (h *Handler) GetService(c *gin.Context) {
	ctx := c.Request.Context()
	serviceID := c.Param("id")

	// Use service layer for getting service
	service, err := h.projectService.GetService(ctx, serviceID)
	if err != nil {
		if errors.Is(err, errors.ErrServiceNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get service"})
		}
		return
	}

	// XC-2 Round 5: when the master admin is acting-as a tenant, a service
	// whose project belongs to a different team must be invisible. 404
	// rather than 403 so the impersonation surface doesn't leak that the
	// id exists in some other tenant.
	if !h.enforceActingTeamForProject(c, service.ProjectID) {
		return
	}

	c.JSON(http.StatusOK, service)
}

// BulkServiceRequest represents a single service in a bulk import request
type BulkServiceRequest struct {
	Name             string            `json:"name" binding:"required"`
	AppPath          string            `json:"app_path" binding:"required"`
	Port             int               `json:"port"`
	BuildCommand     string            `json:"build_command"`
	StartCommand     string            `json:"start_command"`
	Type             types.ServiceType `json:"type"`
	Region           string            `json:"region"`
	AutoDeploy       *bool             `json:"auto_deploy"`        // Enable auto-deploy (defaults to true)
	AutoDeployBranch string            `json:"auto_deploy_branch"` // Override branch for this service
	AutoDeployEnv    string            `json:"auto_deploy_env"`    // Target environment (e.g., "production")
}

// BulkCreateServicesRequest represents a request to create multiple services at once
type BulkCreateServicesRequest struct {
	GitRepo   string               `json:"git_repo" binding:"required"`
	GitBranch string               `json:"git_branch"`
	Services  []BulkServiceRequest `json:"services" binding:"required,min=1"`
}

// BulkCreateServicesResponse represents the response from bulk service creation
type BulkCreateServicesResponse struct {
	Services []types.Service `json:"services"`
	Errors   []string        `json:"errors,omitempty"`
}

// BulkCreateServices creates multiple services in a project from a monorepo import
// POST /v1/projects/:slug/services/bulk
func (h *Handler) BulkCreateServices(c *gin.Context) {
	ctx := c.Request.Context()
	slug := c.Param("slug")

	// Get project first
	project, err := h.projectService.GetProject(ctx, slug)
	if err != nil {
		if errors.Is(err, errors.ErrProjectNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get project"})
		}
		return
	}

	var req BulkCreateServicesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate request
	if len(req.Services) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "At least one service is required"})
		return
	}

	if len(req.Services) > 20 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Maximum 20 services can be created at once"})
		return
	}

	// Create services one by one (could be optimized with batch insert)
	createdServices := make([]types.Service, 0, len(req.Services))
	var createErrors []string

	for _, svc := range req.Services {
		// Normalize app path
		appPath := svc.AppPath
		if appPath == "." {
			appPath = ""
		}

		// Determine auto-deploy branch (service-level overrides request-level)
		autoDeployBranch := req.GitBranch
		if svc.AutoDeployBranch != "" {
			autoDeployBranch = svc.AutoDeployBranch
		}

		createReq := &services.CreateServiceRequest{
			ProjectID:        project.ID.String(),
			Name:             svc.Name,
			GitRepo:          req.GitRepo,
			AppPath:          appPath,
			Type:             svc.Type,
			Region:           svc.Region,
			AutoDeploy:       svc.AutoDeploy,
			AutoDeployBranch: autoDeployBranch,
			AutoDeployEnv:    svc.AutoDeployEnv,
			BuildConfig: types.BuildConfig{
				Type: types.BuildTypeBuildpack, // Default to buildpack
			},
			UserID:    c.GetString("user_id"),
			UserEmail: c.GetString("user_email"),
			UserRole:  c.GetString("user_role"),
		}

		resp, err := h.projectService.CreateService(ctx, createReq)
		if err != nil {
			createErrors = append(createErrors, svc.Name+": "+err.Error())
			continue
		}

		createdServices = append(createdServices, *resp.Service)
	}

	// Return partial success if some services were created
	response := BulkCreateServicesResponse{
		Services: createdServices,
		Errors:   createErrors,
	}

	if len(createdServices) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Failed to create any services",
			"details": createErrors,
		})
		return
	}

	if len(createErrors) > 0 {
		c.JSON(http.StatusMultiStatus, response)
		return
	}

	c.JSON(http.StatusCreated, response)
}
