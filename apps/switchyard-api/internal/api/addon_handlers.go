package api

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/middleware"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ListAllAddons lists all database addons the user has access to
// GET /v1/addons or /v1/databases
func (h *Handler) ListAllAddons(c *gin.Context) {
	ctx := c.Request.Context()

	// Get user ID from context
	userIDStr, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}

	userID, err := uuid.Parse(userIDStr.(string))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user ID"})
		return
	}

	// Check if addon service is available
	if h.addonService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "addon service not available"})
		return
	}

	addons, err := h.addonService.ListAllAddonsForUser(ctx, userID)
	if err != nil {
		h.logger.Error(ctx, "Failed to list all addons", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list addons"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"addons": addons,
		"count":  len(addons),
	})
}

// CreateAddonRequest defines the request body for creating a database addon
type CreateAddonRequest struct {
	Name          string                     `json:"name" binding:"required"`
	Type          types.DatabaseAddonType    `json:"type" binding:"required"`
	EnvironmentID *string                    `json:"environment_id,omitempty"`
	Config        *types.DatabaseAddonConfig `json:"config,omitempty"`
}

// CreateAddonResponse defines the response for addon creation
type CreateAddonResponse struct {
	Addon   *types.DatabaseAddon `json:"addon"`
	Message string               `json:"message"`
}

// CreateAddon creates a new database addon for a project
// POST /v1/projects/:slug/addons
func (h *Handler) CreateAddon(c *gin.Context) {
	ctx := c.Request.Context()
	slug := c.Param("slug")

	// Get project by slug
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

	// Parse request body
	var req CreateAddonRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate addon type
	switch req.Type {
	case types.DatabaseAddonTypePostgres, types.DatabaseAddonTypeRedis, types.DatabaseAddonTypeMySQL:
		// Valid types
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid addon type, must be one of: postgres, redis, mysql"})
		return
	}

	// Parse environment ID if provided
	var environmentID *uuid.UUID
	if req.EnvironmentID != nil && *req.EnvironmentID != "" {
		envID, err := uuid.Parse(*req.EnvironmentID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid environment_id format"})
			return
		}
		environmentID = &envID
	}

	// Get user info from context
	userID, _ := c.Get("userID")
	userEmail, _ := c.Get("userEmail")

	var userUUID *uuid.UUID
	if uid, ok := userID.(string); ok && uid != "" {
		if parsed, err := uuid.Parse(uid); err == nil {
			userUUID = &parsed
		}
	}

	// Prepare config
	config := types.DatabaseAddonConfig{}
	if req.Config != nil {
		config = *req.Config
	}

	// Create the addon
	createReq := &addons.CreateAddonRequest{
		ProjectID:     project.ID,
		EnvironmentID: environmentID,
		Type:          req.Type,
		Name:          req.Name,
		Config:        config,
		UserID:        userUUID,
	}
	if email, ok := userEmail.(string); ok {
		createReq.UserEmail = email
	}

	addon, err := h.addonService.CreateAddon(ctx, createReq)
	if err != nil {
		h.logger.Error(ctx, "Failed to create addon",
			logging.String("project_slug", slug),
			logging.String("addon_name", req.Name),
			logging.Error("error", err))
		middleware.AbortInternal(c, err)
		return
	}

	h.logger.Info(ctx, "Addon created",
		logging.String("addon_id", addon.ID.String()),
		logging.String("project_slug", slug),
		logging.String("type", string(addon.Type)))

	c.JSON(http.StatusCreated, CreateAddonResponse{
		Addon:   addon,
		Message: "Database addon creation initiated",
	})
}

// ListAddons lists all database addons for a project
// GET /v1/projects/:slug/addons
func (h *Handler) ListAddons(c *gin.Context) {
	ctx := c.Request.Context()
	slug := c.Param("slug")

	// Get project by slug
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

	addons, err := h.addonService.ListAddons(ctx, project.ID)
	if err != nil {
		h.logger.Error(ctx, "Failed to list addons", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list addons"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"addons": addons,
		"count":  len(addons),
	})
}

// GetAddon retrieves a specific database addon
// GET /v1/addons/:id
func (h *Handler) GetAddon(c *gin.Context) {
	ctx := c.Request.Context()
	addonID := c.Param("id")

	// Parse addon ID
	addonUUID, err := uuid.Parse(addonID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid addon_id format"})
		return
	}

	addon, err := h.addonService.GetAddonWithBindings(ctx, addonUUID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "addon not found"})
			return
		}
		h.logger.Error(ctx, "Failed to get addon", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get addon"})
		return
	}

	c.JSON(http.StatusOK, addon)
}

// GetAddonCredentials retrieves connection credentials for an addon
// GET /v1/addons/:id/credentials
func (h *Handler) GetAddonCredentials(c *gin.Context) {
	ctx := c.Request.Context()
	addonID := c.Param("id")

	// Parse addon ID
	addonUUID, err := uuid.Parse(addonID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid addon_id format"})
		return
	}

	creds, err := h.addonService.GetCredentials(ctx, addonUUID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get addon credentials",
			logging.String("addon_id", addonID),
			logging.Error("error", err))
		middleware.AbortInternal(c, err)
		return
	}

	c.JSON(http.StatusOK, creds)
}

// RefreshAddonStatus refreshes the status of a provisioning addon
// POST /v1/addons/:id/refresh
func (h *Handler) RefreshAddonStatus(c *gin.Context) {
	ctx := c.Request.Context()
	addonID := c.Param("id")

	// Parse addon ID
	addonUUID, err := uuid.Parse(addonID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid addon_id format"})
		return
	}

	addon, err := h.addonService.RefreshStatus(ctx, addonUUID)
	if err != nil {
		h.logger.Error(ctx, "Failed to refresh addon status",
			logging.String("addon_id", addonID),
			logging.Error("error", err))
		middleware.AbortInternal(c, err)
		return
	}

	c.JSON(http.StatusOK, addon)
}

// DeleteAddon deletes a database addon
// DELETE /v1/addons/:id
func (h *Handler) DeleteAddon(c *gin.Context) {
	ctx := c.Request.Context()
	addonID := c.Param("id")

	// Parse addon ID
	addonUUID, err := uuid.Parse(addonID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid addon_id format"})
		return
	}

	if err := h.addonService.DeleteAddon(ctx, addonUUID); err != nil {
		h.logger.Error(ctx, "Failed to delete addon",
			logging.String("addon_id", addonID),
			logging.Error("error", err))
		middleware.AbortInternal(c, err)
		return
	}

	h.logger.Info(ctx, "Addon deleted", logging.String("addon_id", addonID))

	c.JSON(http.StatusOK, gin.H{"message": "Addon deleted successfully"})
}

// CreateBindingRequest defines the request body for creating an addon binding
type CreateBindingRequest struct {
	ServiceID  string `json:"service_id" binding:"required"`
	EnvVarName string `json:"env_var_name,omitempty"`
}

// CreateAddonBinding creates a binding between an addon and a service
// POST /v1/addons/:id/bindings
func (h *Handler) CreateAddonBinding(c *gin.Context) {
	ctx := c.Request.Context()
	addonID := c.Param("id")

	// Parse addon ID
	addonUUID, err := uuid.Parse(addonID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid addon_id format"})
		return
	}

	// Parse request body
	var req CreateBindingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Parse service ID
	serviceUUID, err := uuid.Parse(req.ServiceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service_id format"})
		return
	}

	// Default env var name based on addon type
	envVarName := req.EnvVarName
	if envVarName == "" {
		addon, err := h.addonService.GetAddon(ctx, addonUUID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get addon"})
			return
		}
		switch addon.Type {
		case types.DatabaseAddonTypePostgres:
			envVarName = "DATABASE_URL"
		case types.DatabaseAddonTypeRedis:
			envVarName = "REDIS_URL"
		case types.DatabaseAddonTypeMySQL:
			envVarName = "MYSQL_URL"
		default:
			envVarName = "DATABASE_URL"
		}
	}

	binding, err := h.addonService.CreateBinding(ctx, addonUUID, serviceUUID, envVarName)
	if err != nil {
		h.logger.Error(ctx, "Failed to create addon binding",
			logging.String("addon_id", addonID),
			logging.String("service_id", req.ServiceID),
			logging.Error("error", err))
		middleware.AbortInternal(c, err)
		return
	}

	h.logger.Info(ctx, "Addon binding created",
		logging.String("addon_id", addonID),
		logging.String("service_id", req.ServiceID),
		logging.String("env_var", envVarName))

	c.JSON(http.StatusCreated, gin.H{
		"binding": binding,
		"message": "Binding created successfully",
	})
}

// DeleteAddonBinding removes a binding between an addon and a service
// DELETE /v1/addons/:id/bindings/:service_id
func (h *Handler) DeleteAddonBinding(c *gin.Context) {
	ctx := c.Request.Context()
	addonID := c.Param("id")
	serviceID := c.Param("service_id")

	// Parse addon ID
	addonUUID, err := uuid.Parse(addonID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid addon_id format"})
		return
	}

	// Parse service ID
	serviceUUID, err := uuid.Parse(serviceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service_id format"})
		return
	}

	if err := h.addonService.DeleteBinding(ctx, addonUUID, serviceUUID); err != nil {
		h.logger.Error(ctx, "Failed to delete addon binding",
			logging.String("addon_id", addonID),
			logging.String("service_id", serviceID),
			logging.Error("error", err))
		middleware.AbortInternal(c, err)
		return
	}

	h.logger.Info(ctx, "Addon binding deleted",
		logging.String("addon_id", addonID),
		logging.String("service_id", serviceID))

	c.JSON(http.StatusOK, gin.H{"message": "Binding deleted successfully"})
}

// GetServiceBindings retrieves all addon bindings for a service
// GET /v1/services/:id/bindings
func (h *Handler) GetServiceBindings(c *gin.Context) {
	ctx := c.Request.Context()
	serviceID := c.Param("id")

	// Parse service ID
	serviceUUID, err := uuid.Parse(serviceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service_id format"})
		return
	}

	bindings, err := h.addonService.GetBindingsForService(ctx, serviceUUID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get service bindings",
			logging.String("service_id", serviceID),
			logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get bindings"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"bindings": bindings,
		"count":    len(bindings),
	})
}
