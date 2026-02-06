package api

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// UpdateServiceRequest defines the request body for updating a service
type UpdateServiceRequest struct {
	Name             *string            `json:"name,omitempty"`
	GitRepo          *string            `json:"git_repo,omitempty"`
	AppPath          *string            `json:"app_path,omitempty"`
	AutoDeploy       *bool              `json:"auto_deploy,omitempty"`
	AutoDeployBranch *string            `json:"auto_deploy_branch,omitempty"`
	AutoDeployEnv    *string            `json:"auto_deploy_env,omitempty"`
	BuildConfig      *types.BuildConfig `json:"build_config,omitempty"`
}

// UpdateService updates a service's settings
// PATCH /v1/services/:id
func (h *Handler) UpdateService(c *gin.Context) {
	serviceID := c.Param("id")
	if serviceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "service_id is required"})
		return
	}

	ctx := c.Request.Context()

	// Parse service ID
	serviceUUID, err := uuid.Parse(serviceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service_id format"})
		return
	}

	// Get existing service
	service, err := h.repos.Services.GetByID(serviceUUID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "service not found"})
			return
		}
		h.logger.Error(ctx, "Failed to get service", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get service"})
		return
	}

	// Parse request body
	var req UpdateServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Apply updates
	if req.Name != nil {
		service.Name = *req.Name
	}
	if req.GitRepo != nil {
		service.GitRepo = *req.GitRepo
	}
	if req.AppPath != nil {
		service.AppPath = *req.AppPath
	}
	if req.AutoDeploy != nil {
		service.AutoDeploy = *req.AutoDeploy
	}
	if req.AutoDeployBranch != nil {
		service.AutoDeployBranch = *req.AutoDeployBranch
	}
	if req.AutoDeployEnv != nil {
		service.AutoDeployEnv = *req.AutoDeployEnv
	}
	if req.BuildConfig != nil {
		service.BuildConfig = *req.BuildConfig
	}

	// Update in database
	if err := h.repos.Services.Update(ctx, service); err != nil {
		h.logger.Error(ctx, "Failed to update service",
			logging.String("service_id", serviceID),
			logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update service"})
		return
	}

	h.logger.Info(ctx, "Service updated",
		logging.String("service_id", serviceID),
		logging.String("name", service.Name))

	c.JSON(http.StatusOK, gin.H{
		"service": service,
		"message": "Service updated successfully",
	})
}

// DeleteService deletes a service and all associated resources
// DELETE /v1/services/:id
func (h *Handler) DeleteService(c *gin.Context) {
	serviceID := c.Param("id")
	if serviceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "service_id is required"})
		return
	}

	ctx := c.Request.Context()

	// Parse service ID
	serviceUUID, err := uuid.Parse(serviceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service_id format"})
		return
	}

	// Verify service exists
	service, err := h.repos.Services.GetByID(serviceUUID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "service not found"})
			return
		}
		h.logger.Error(ctx, "Failed to get service", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get service"})
		return
	}

	// Delete env vars for this service first (due to FK constraints)
	if h.repos.EnvVars != nil {
		if err := h.repos.EnvVars.DeleteByService(ctx, serviceUUID); err != nil {
			h.logger.Warn(ctx, "Failed to delete env vars for service",
				logging.String("service_id", serviceID),
				logging.Error("error", err))
			// Continue anyway - might not have any
		}
	}

	// Cleanup tunnel routes and DNS records for all domains (before deleting from DB)
	h.cleanupDomainsForService(ctx, serviceUUID)

	// Delete custom domains for this service
	if err := h.repos.CustomDomains.DeleteByServiceID(ctx, serviceID); err != nil {
		h.logger.Warn(ctx, "Failed to delete custom domains for service",
			logging.String("service_id", serviceID),
			logging.Error("error", err))
		// Continue anyway
	}

	// Delete routes for this service
	if err := h.repos.Routes.DeleteByServiceID(ctx, serviceID); err != nil {
		h.logger.Warn(ctx, "Failed to delete routes for service",
			logging.String("service_id", serviceID),
			logging.Error("error", err))
		// Continue anyway
	}

	// Delete service dependencies
	if h.repos.ServiceDependencies != nil {
		if err := h.repos.ServiceDependencies.DeleteByServiceID(ctx, serviceUUID); err != nil {
			h.logger.Warn(ctx, "Failed to delete service dependencies",
				logging.String("service_id", serviceID),
				logging.Error("error", err))
		}
	}

	// Delete the service
	if err := h.repos.Services.Delete(ctx, serviceUUID); err != nil {
		h.logger.Error(ctx, "Failed to delete service",
			logging.String("service_id", serviceID),
			logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete service"})
		return
	}

	h.logger.Info(ctx, "Service deleted",
		logging.String("service_id", serviceID),
		logging.String("name", service.Name))

	c.JSON(http.StatusOK, gin.H{
		"message": "Service deleted successfully",
	})
}

// GetServiceSettings returns detailed service settings
// GET /v1/services/:id/settings
func (h *Handler) GetServiceSettings(c *gin.Context) {
	serviceID := c.Param("id")
	if serviceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "service_id is required"})
		return
	}

	ctx := c.Request.Context()

	// Parse service ID
	serviceUUID, err := uuid.Parse(serviceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service_id format"})
		return
	}

	// Get service
	service, err := h.repos.Services.GetByID(serviceUUID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "service not found"})
			return
		}
		h.logger.Error(ctx, "Failed to get service", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get service"})
		return
	}

	// Get project name
	project, err := h.repos.Projects.GetByID(ctx, service.ProjectID)
	projectName := ""
	if err == nil {
		projectName = project.Name
	}

	// Build settings response
	settings := gin.H{
		"id":                 service.ID,
		"name":               service.Name,
		"project_id":         service.ProjectID,
		"project_name":       projectName,
		"git_repo":           service.GitRepo,
		"app_path":           service.AppPath,
		"auto_deploy":        service.AutoDeploy,
		"auto_deploy_branch": service.AutoDeployBranch,
		"auto_deploy_env":    service.AutoDeployEnv,
		"build_config":       service.BuildConfig,
		"created_at":         service.CreatedAt,
		"updated_at":         service.UpdatedAt,
	}

	c.JSON(http.StatusOK, gin.H{"settings": settings})
}

// ListServicesByGitRepo returns all services that use a specific git repository URL.
// This is an internal endpoint used by Roundhouse for webhook-triggered preview environments.
//
// Request:
//   - Method: GET /v1/internal/services
//   - Query Parameters: git_repo (string) - Git repository URL to search for
//   - Authorization: API key via X-API-Key or Authorization header
//
// Response:
//   - 200 OK: {services: Service[]}
//   - 400 Bad Request: Missing git_repo parameter
//   - 500 Internal Server Error: Failed to query services
func (h *Handler) ListServicesByGitRepo(c *gin.Context) {
	ctx := c.Request.Context()

	gitRepo := c.Query("git_repo")
	if gitRepo == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "git_repo query parameter is required"})
		return
	}

	// Query services by git repo URL
	services, err := h.repos.Services.ListByGitRepo(gitRepo)
	if err != nil {
		h.logger.Error(ctx, "Failed to list services by git repo",
			logging.String("git_repo", gitRepo),
			logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query services"})
		return
	}

	// Convert to response format
	type serviceResponse struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		ProjectID string `json:"project_id"`
	}

	result := make([]serviceResponse, 0, len(services))
	for _, svc := range services {
		result = append(result, serviceResponse{
			ID:        svc.ID.String(),
			Name:      svc.Name,
			ProjectID: svc.ProjectID.String(),
		})
	}

	c.JSON(http.StatusOK, gin.H{"services": result})
}
