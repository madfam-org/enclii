package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// CreateDeploymentGroupRequest represents the request body for creating a deployment group
type CreateDeploymentGroupRequest struct {
	ServiceIDs []string `json:"service_ids,omitempty"` // If empty, deploys all project services
	Strategy   string   `json:"strategy,omitempty"`    // "parallel", "sequential", "dependency_ordered" (default)
	GitSHA     string   `json:"git_sha,omitempty"`
	PRURL      string   `json:"pr_url,omitempty"`
}

// CreateDeploymentGroup creates a new deployment group for coordinated multi-service deployment
// POST /v1/projects/:slug/environments/:env_name/deployment-groups
func (h *Handler) CreateDeploymentGroup(c *gin.Context) {
	ctx := c.Request.Context()
	projectSlug := c.Param("slug")
	envName := c.Param("env_name")

	// Get user from context
	user, ok := c.Get("user")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found in context"})
		return
	}
	userObj := user.(*types.User)

	// Get project by slug
	project, err := h.repos.Projects.GetBySlug(projectSlug)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Project not found",
			"slug":  projectSlug,
		})
		return
	}

	// Get environment by name
	env, err := h.repos.Environments.GetByProjectAndName(project.ID, envName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":       "Environment not found",
			"environment": envName,
		})
		return
	}

	var req CreateDeploymentGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Create deployment group via service
	result, err := h.deploymentGroupService.CreateGroupDeployment(ctx, &services.CreateGroupDeploymentRequest{
		ProjectID:     project.ID.String(),
		EnvironmentID: env.ID.String(),
		ServiceIDs:    req.ServiceIDs,
		Strategy:      req.Strategy,
		GitSHA:        req.GitSHA,
		PRURL:         req.PRURL,
		TriggeredBy:   userObj.Email,
		UserID:        userObj.ID.String(),
		UserEmail:     userObj.Email,
		UserRole:      string(userObj.Role),
	})
	if err != nil {
		h.logger.Error(ctx, "Failed to create deployment group",
			logging.Error("error", err),
			logging.String("project_slug", projectSlug))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to create deployment group",
			"details": err.Error(),
		})
		return
	}

	h.logger.Info(ctx, "Deployment group created",
		logging.String("group_id", result.Group.ID.String()),
		logging.String("project_slug", projectSlug),
		logging.String("strategy", string(result.Group.Strategy)))

	c.JSON(http.StatusCreated, gin.H{
		"group":            result.Group,
		"deployment_order": result.DeploymentOrder,
		"layers_count":     len(result.DeploymentOrder),
	})
}

// ListDeploymentGroups lists deployment groups for a project
// GET /v1/projects/:slug/deployment-groups
func (h *Handler) ListDeploymentGroups(c *gin.Context) {
	ctx := c.Request.Context()
	projectSlug := c.Param("slug")

	// Parse pagination
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	groups, err := h.deploymentGroupService.ListGroupDeployments(ctx, projectSlug, limit, offset)
	if err != nil {
		h.logger.Error(ctx, "Failed to list deployment groups",
			logging.Error("error", err),
			logging.String("project_slug", projectSlug))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to list deployment groups",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"groups": groups,
		"count":  len(groups),
		"limit":  limit,
		"offset": offset,
	})
}

// GetDeploymentGroup retrieves a deployment group by ID
// GET /v1/projects/:slug/deployment-groups/:group_id
func (h *Handler) GetDeploymentGroup(c *gin.Context) {
	ctx := c.Request.Context()
	groupID := c.Param("group_id")

	groupUUID, err := uuid.Parse(groupID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid deployment group ID"})
		return
	}
	if _, ok := h.loadDeploymentGroupWithSlugAccess(c, groupUUID); !ok {
		return
	}

	group, err := h.deploymentGroupService.GetGroupDeployment(ctx, groupID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get deployment group",
			logging.Error("error", err),
			logging.String("group_id", groupID))
		c.JSON(http.StatusNotFound, gin.H{
			"error":    "Deployment group not found",
			"group_id": groupID,
		})
		return
	}

	// Get deployments in this group
	deployments, err := h.repos.Deployments.ListByGroup(ctx, group.ID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get group deployments",
			logging.Error("error", err),
			logging.String("group_id", groupID))
	}

	c.JSON(http.StatusOK, gin.H{
		"group":       group,
		"deployments": deployments,
	})
}

// ExecuteDeploymentGroup triggers the execution of a pending deployment group
// POST /v1/projects/:slug/deployment-groups/:group_id/execute
func (h *Handler) ExecuteDeploymentGroup(c *gin.Context) {
	ctx := c.Request.Context()
	groupID := c.Param("group_id")

	groupUUID, err := uuid.Parse(groupID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid deployment group ID"})
		return
	}
	if _, ok := h.loadDeploymentGroupWithSlugAccess(c, groupUUID); !ok {
		return
	}

	// Get user from context
	user, ok := c.Get("user")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found in context"})
		return
	}
	userObj := user.(*types.User)

	result, err := h.deploymentGroupService.ExecuteGroupDeployment(ctx, &services.ExecuteGroupDeploymentRequest{
		GroupID:   groupID,
		UserID:    userObj.ID.String(),
		UserEmail: userObj.Email,
		UserRole:  string(userObj.Role),
	})
	if err != nil {
		h.logger.Error(ctx, "Failed to execute deployment group",
			logging.Error("error", err),
			logging.String("group_id", groupID))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to execute deployment group",
			"details": err.Error(),
		})
		return
	}

	h.logger.Info(ctx, "Deployment group execution completed",
		logging.String("group_id", groupID),
		logging.String("status", string(result.Group.Status)))

	// Convert errors to strings for JSON response
	var errorMessages []string
	for _, err := range result.Errors {
		errorMessages = append(errorMessages, err.Error())
	}

	c.JSON(http.StatusOK, gin.H{
		"group":             result.Group,
		"deployments":       result.Deployments,
		"deployments_count": len(result.Deployments),
		"errors":            errorMessages,
		"errors_count":      len(result.Errors),
	})
}

// RollbackDeploymentGroup rolls back all deployments in a group
// POST /v1/projects/:slug/deployment-groups/:group_id/rollback
func (h *Handler) RollbackDeploymentGroup(c *gin.Context) {
	ctx := c.Request.Context()
	groupID := c.Param("group_id")

	groupUUID, err := uuid.Parse(groupID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid deployment group ID"})
		return
	}
	if _, ok := h.loadDeploymentGroupWithSlugAccess(c, groupUUID); !ok {
		return
	}

	// Get user from context
	user, ok := c.Get("user")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found in context"})
		return
	}
	userObj := user.(*types.User)

	result, err := h.deploymentGroupService.RollbackGroup(ctx, &services.RollbackGroupRequest{
		GroupID:   groupID,
		UserID:    userObj.ID.String(),
		UserEmail: userObj.Email,
		UserRole:  string(userObj.Role),
	})
	if err != nil {
		h.logger.Error(ctx, "Failed to rollback deployment group",
			logging.Error("error", err),
			logging.String("group_id", groupID))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to rollback deployment group",
			"details": err.Error(),
		})
		return
	}

	h.logger.Info(ctx, "Deployment group rollback completed",
		logging.String("group_id", groupID),
		logging.String("status", string(result.Group.Status)),
		logging.Int("rolled_back", result.RolledBack),
		logging.Int("failed", result.FailedToRoll))

	// Convert errors to strings for JSON response
	var errorMessages []string
	for _, err := range result.Errors {
		errorMessages = append(errorMessages, err.Error())
	}

	c.JSON(http.StatusOK, gin.H{
		"group":          result.Group,
		"rolled_back":    result.RolledBack,
		"failed_to_roll": result.FailedToRoll,
		"errors":         errorMessages,
	})
}

// --- Service Dependencies API ---

// AddServiceDependencyRequest represents the request body for adding a service dependency
type AddServiceDependencyRequest struct {
	DependsOnServiceID string `json:"depends_on_service_id" binding:"required"`
	DependencyType     string `json:"dependency_type,omitempty"` // "runtime" (default), "build", "data"
}

// AddServiceDependency adds a dependency between two services
// POST /v1/services/:id/dependencies
func (h *Handler) AddServiceDependency(c *gin.Context) {
	ctx := c.Request.Context()
	serviceUUID, ok := h.mustServiceAccess(c)
	if !ok {
		return
	}
	serviceID := serviceUUID.String()

	// Get user from context
	user, ok := c.Get("user")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found in context"})
		return
	}
	userObj := user.(*types.User)

	var req AddServiceDependencyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	dependsOnUUID, err := uuid.Parse(req.DependsOnServiceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid depends_on_service_id"})
		return
	}
	if !h.enforceServiceAccess(c, dependsOnUUID) {
		return
	}

	dep, err := h.deploymentGroupService.AddServiceDependency(ctx, &services.AddServiceDependencyRequest{
		ServiceID:          serviceID,
		DependsOnServiceID: req.DependsOnServiceID,
		DependencyType:     req.DependencyType,
		UserEmail:          userObj.Email,
		UserRole:           string(userObj.Role),
	})
	if err != nil {
		h.logger.Error(ctx, "Failed to add service dependency",
			logging.Error("error", err),
			logging.String("service_id", serviceID))
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Failed to add service dependency",
			"details": err.Error(),
		})
		return
	}

	h.logger.Info(ctx, "Service dependency added",
		logging.String("service_id", serviceID),
		logging.String("depends_on", req.DependsOnServiceID),
		logging.String("type", string(dep.DependencyType)))

	c.JSON(http.StatusCreated, dep)
}

// ListServiceDependencies lists all dependencies for a service
// GET /v1/services/:id/dependencies
func (h *Handler) ListServiceDependencies(c *gin.Context) {
	ctx := c.Request.Context()
	serviceUUID, ok := h.mustServiceAccess(c)
	if !ok {
		return
	}
	serviceID := serviceUUID.String()

	deps, err := h.deploymentGroupService.GetServiceDependencies(ctx, serviceID)
	if err != nil {
		h.logger.Error(ctx, "Failed to list service dependencies",
			logging.Error("error", err),
			logging.String("service_id", serviceID))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to list service dependencies",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"dependencies": deps,
		"count":        len(deps),
	})
}

// ListServiceDependents lists all services that depend on a given service
// GET /v1/services/:id/dependents
func (h *Handler) ListServiceDependents(c *gin.Context) {
	ctx := c.Request.Context()
	serviceUUID, ok := h.mustServiceAccess(c)
	if !ok {
		return
	}
	serviceID := serviceUUID.String()

	deps, err := h.deploymentGroupService.GetServiceDependents(ctx, serviceID)
	if err != nil {
		h.logger.Error(ctx, "Failed to list service dependents",
			logging.Error("error", err),
			logging.String("service_id", serviceID))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to list service dependents",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"dependents": deps,
		"count":      len(deps),
	})
}

// RemoveServiceDependency removes a dependency between two services
// DELETE /v1/services/:id/dependencies/:depends_on_id
func (h *Handler) RemoveServiceDependency(c *gin.Context) {
	ctx := c.Request.Context()
	serviceUUID, ok := h.mustServiceAccess(c)
	if !ok {
		return
	}
	serviceID := serviceUUID.String()
	dependsOnID := c.Param("depends_on_id")

	dependsOnUUID, err := uuid.Parse(dependsOnID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid depends_on_service_id"})
		return
	}
	if !h.enforceServiceAccess(c, dependsOnUUID) {
		return
	}

	// Get user from context
	user, ok := c.Get("user")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found in context"})
		return
	}
	userObj := user.(*types.User)

	err = h.deploymentGroupService.RemoveServiceDependency(ctx, serviceID, dependsOnID, userObj.Email, string(userObj.Role))
	if err != nil {
		h.logger.Error(ctx, "Failed to remove service dependency",
			logging.Error("error", err),
			logging.String("service_id", serviceID),
			logging.String("depends_on_id", dependsOnID))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to remove service dependency",
			"details": err.Error(),
		})
		return
	}

	h.logger.Info(ctx, "Service dependency removed",
		logging.String("service_id", serviceID),
		logging.String("depends_on", dependsOnID))

	c.JSON(http.StatusOK, gin.H{
		"message":       "Dependency removed successfully",
		"service_id":    serviceID,
		"depends_on_id": dependsOnID,
	})
}
