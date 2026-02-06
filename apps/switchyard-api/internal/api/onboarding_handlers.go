package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// OnboardRepo handles self-service repo onboarding
// POST /v1/admin/onboard
func (h *Handler) OnboardRepo(c *gin.Context) {
	ctx := c.Request.Context()

	var req types.OnboardingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.logger.Info(ctx, "Starting repo onboarding",
		logging.String("repo", req.RepoFullName),
		logging.String("project", req.ProjectName))

	// Check if already onboarded
	existing, err := h.repos.Onboardings.GetByRepo(ctx, req.RepoFullName)
	if err == nil && existing != nil {
		c.JSON(http.StatusConflict, gin.H{
			"error":   "Repository already onboarded",
			"status":  existing.OnboardStatus,
			"repo":    existing.RepoFullName,
			"created": existing.CreatedAt,
		})
		return
	}

	// Step 1: Validate enclii.yaml from target repo
	parts := strings.SplitN(req.RepoFullName, "/", 2)
	if len(parts) != 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repo_full_name must be in owner/repo format"})
		return
	}

	encliiConfig := h.fetchAndParseEncliiYAML(ctx, req.RepoFullName, "HEAD")

	// Step 2: Find or create project
	project, err := h.repos.Projects.GetBySlug(req.ProjectName)
	if err != nil {
		if err == sql.ErrNoRows {
			// Create project
			project = &types.Project{
				Name: req.ProjectName,
				Slug: req.ProjectName,
			}
			if createErr := h.repos.Projects.Create(project); createErr != nil {
				h.logger.Error(ctx, "Failed to create project during onboarding",
					logging.String("project", req.ProjectName),
					logging.Error("db_error", createErr))
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create project"})
				return
			}
			h.logger.Info(ctx, "Created project for onboarding",
				logging.String("project_id", project.ID.String()),
				logging.String("project_name", req.ProjectName))
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to look up project"})
			return
		}
	}

	// Step 3: Create service from enclii.yaml metadata if available
	var serviceNames []string
	if encliiConfig != nil && encliiConfig.Metadata.Name != "" {
		svcName := encliiConfig.Metadata.Name
		// Check if service already exists
		_, lookupErr := h.repos.Services.GetByName(svcName)
		if lookupErr == nil {
			serviceNames = append(serviceNames, svcName+" (existing)")
		} else {
			newService := &types.Service{
				ProjectID:  project.ID,
				Name:       svcName,
				GitRepo:    "https://github.com/" + req.RepoFullName,
				AutoDeploy: true,
			}
			if createErr := h.repos.Services.Create(newService); createErr != nil {
				h.logger.Warn(ctx, "Failed to create service during onboarding (non-fatal)",
					logging.String("service", svcName),
					logging.Error("db_error", createErr))
				serviceNames = append(serviceNames, svcName+" (failed)")
			} else {
				serviceNames = append(serviceNames, svcName+" (created)")
			}
		}
	}

	// Step 4: Generate ArgoCD Application YAML
	namespace := req.Namespace
	if namespace == "" {
		namespace = req.ProjectName
	}
	manifestPath := req.ManifestPath
	if manifestPath == "" {
		manifestPath = "infra/k8s/production"
	}
	branch := "main"
	if req.Branch != nil {
		branch = *req.Branch
	}
	appName := req.ProjectName + "-services"
	repoURL := "https://github.com/" + req.RepoFullName + ".git"

	argocdYAML := generateArgocdApp(repoURL, manifestPath, namespace, appName, branch)

	// Step 5: Provision domains from enclii.yaml (if available)
	var domainResults []string
	if encliiConfig != nil && len(encliiConfig.Spec.Domains) > 0 {
		// Get the first service to associate domains with
		services, _ := h.repos.Services.ListByProject(project.ID)
		if len(services) > 0 {
			go h.provisionDomainsFromYAML(context.Background(), services[0], encliiConfig)
			for _, d := range encliiConfig.Spec.Domains {
				domainResults = append(domainResults, d.Name+" (provisioning)")
			}
		}
	}

	// Step 6: Register onboarding
	configSnapshot := map[string]interface{}{
		"manifest_path": manifestPath,
		"namespace":     namespace,
		"branch":        branch,
		"services":      serviceNames,
		"domains":       domainResults,
	}
	if encliiConfig != nil {
		configSnapshot["enclii_yaml_found"] = true
	}

	reg := &types.OnboardingRegistration{
		ProjectID:      project.ID,
		RepoFullName:   req.RepoFullName,
		ArgocdAppName:  &appName,
		OnboardStatus:  "completed",
		ConfigSnapshot: configSnapshot,
	}
	if err := h.repos.Onboardings.Create(ctx, reg); err != nil {
		h.logger.Error(ctx, "Failed to register onboarding",
			logging.String("repo", req.RepoFullName),
			logging.Error("db_error", err))
		// Non-fatal — continue with response
	}

	h.logger.Info(ctx, "Repo onboarding completed",
		logging.String("repo", req.RepoFullName),
		logging.String("project", req.ProjectName),
		logging.String("app_name", appName))

	c.JSON(http.StatusOK, gin.H{
		"status":  "completed",
		"repo":    req.RepoFullName,
		"project": req.ProjectName,
		"next_steps": []string{
			fmt.Sprintf("1. Commit ArgoCD app YAML to infra/argocd/apps/%s.yaml", appName),
			"2. Ensure GitHub webhook is configured to POST to https://api.enclii.dev/v1/webhooks/github",
			"3. Push to main branch to trigger first auto-deploy",
			"4. Check lifecycle events: GET /v1/lifecycle/timeline/" + req.RepoFullName,
		},
		"argocd_yaml":   argocdYAML,
		"argocd_app":    appName,
		"namespace":     namespace,
		"manifest_path": manifestPath,
		"services":      serviceNames,
		"domains":       domainResults,
	})
}

// ListOnboardings returns all onboarded repos
// GET /v1/admin/onboard
func (h *Handler) ListOnboardings(c *gin.Context) {
	ctx := c.Request.Context()

	regs, err := h.repos.Onboardings.List(ctx)
	if err != nil {
		h.logger.Error(ctx, "Failed to list onboardings",
			logging.Error("db_error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list onboardings"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"count":         len(regs),
		"registrations": regs,
	})
}

// GetOnboarding returns a specific repo's onboarding status
// GET /v1/admin/onboard/:owner/:repo
func (h *Handler) GetOnboarding(c *gin.Context) {
	ctx := c.Request.Context()

	owner := c.Param("owner")
	repo := c.Param("repo")
	repoFullName := owner + "/" + repo

	reg, err := h.repos.Onboardings.GetByRepo(ctx, repoFullName)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Repository not onboarded"})
			return
		}
		h.logger.Error(ctx, "Failed to get onboarding",
			logging.String("repo", repoFullName),
			logging.Error("db_error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get onboarding"})
		return
	}

	c.JSON(http.StatusOK, reg)
}
