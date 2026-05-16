package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

const defaultProductionEnvironmentName = "production"

func (h *Handler) ensureDefaultProductionEnvironment(ctx context.Context, project *types.Project) (*types.Environment, error) {
	if h == nil || h.repos == nil || h.repos.Environments == nil || project == nil {
		return nil, nil
	}

	if existing, err := h.repos.Environments.GetByProjectAndName(project.ID, defaultProductionEnvironmentName); err == nil && existing != nil {
		return existing, nil
	}

	env := &types.Environment{
		ProjectID:     project.ID,
		Name:          defaultProductionEnvironmentName,
		KubeNamespace: defaultProductionNamespace(project),
	}
	if err := h.repos.Environments.Create(env); err != nil {
		if existing, lookupErr := h.repos.Environments.GetByProjectAndName(project.ID, defaultProductionEnvironmentName); lookupErr == nil && existing != nil {
			return existing, nil
		}
		return nil, err
	}

	h.logger.Info(ctx, "Default production environment created",
		logging.String("project", project.Slug),
		logging.String("namespace", env.KubeNamespace))

	return env, nil
}

func defaultProductionNamespace(project *types.Project) string {
	if project == nil {
		return ""
	}
	if strings.TrimSpace(project.Slug) != "" {
		return strings.TrimSpace(project.Slug)
	}
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(project.Name), "_", "-"))
}

// CreateEnvironment creates a new environment for a project
func (h *Handler) CreateEnvironment(c *gin.Context) {
	ctx := c.Request.Context()
	projectSlug := c.Param("slug")

	var req struct {
		Name          string `json:"name" binding:"required"`
		KubeNamespace string `json:"kube_namespace"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// Get project by slug
	project, err := h.repos.Projects.GetBySlug(projectSlug)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
		return
	}

	// Check if environment already exists
	existing, _ := h.repos.Environments.GetByProjectAndName(project.ID, req.Name)
	if existing != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Environment already exists"})
		return
	}

	// Generate kube_namespace if not provided
	// Use consistent pattern: enclii-{project_slug}-{env_name}
	kubeNamespace := req.KubeNamespace
	if kubeNamespace == "" {
		envNameNormalized := strings.ToLower(strings.ReplaceAll(req.Name, "_", "-"))
		kubeNamespace = fmt.Sprintf("enclii-%s-%s", projectSlug, envNameNormalized)
	}

	env := &types.Environment{
		ProjectID:     project.ID,
		Name:          req.Name,
		KubeNamespace: kubeNamespace,
	}

	if err := h.repos.Environments.Create(env); err != nil {
		h.logger.Error(ctx, "Failed to create environment",
			logging.Error("error", err),
			logging.String("project", projectSlug),
			logging.String("environment", req.Name),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create environment"})
		return
	}

	c.JSON(http.StatusCreated, env)
}

// ListEnvironments returns all environments for a project
func (h *Handler) ListEnvironments(c *gin.Context) {
	projectSlug := c.Param("slug")

	// Get project by slug
	project, err := h.repos.Projects.GetBySlug(projectSlug)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
		return
	}

	if _, err := h.ensureDefaultProductionEnvironment(c.Request.Context(), project); err != nil {
		h.logger.Warn(c.Request.Context(), "Failed to ensure default production environment before listing",
			logging.String("project", projectSlug),
			logging.Error("error", err))
	}

	environments, err := h.repos.Environments.ListByProject(project.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list environments"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"environments": environments})
}

// GetEnvironment returns a specific environment
func (h *Handler) GetEnvironment(c *gin.Context) {
	ctx := c.Request.Context()
	projectSlug := c.Param("slug")
	envName := c.Param("env_name")

	// Get project by slug
	project, err := h.repos.Projects.GetBySlug(projectSlug)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
		return
	}

	env, err := h.repos.Environments.GetByProjectAndName(project.ID, envName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Environment not found"})
		return
	}

	// Optionally get ID-based lookup
	if envName == "" {
		envIDStr := c.Param("env_id")
		envID, err := uuid.Parse(envIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid environment ID"})
			return
		}
		env, err = h.repos.Environments.GetByID(ctx, envID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Environment not found"})
			return
		}
	}

	c.JSON(http.StatusOK, env)
}
