package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// A persisted staging environment and the project's completed repository
// registration jointly prove ownership. Neither labels nor request flags do.
func stagingNamespaceProof(project *types.Project, environment *types.Environment, registration *types.OnboardingRegistration, repo string) bool {
	return project != nil && project.ID != uuid.Nil && project.Slug != "" &&
		environment != nil && environment.ID != uuid.Nil && environment.ProjectID == project.ID &&
		environment.Name == "staging" && environment.KubeNamespace == project.Slug+"-staging" &&
		registration != nil && registration.ID != uuid.Nil && registration.ProjectID == project.ID &&
		registration.RepoFullName == repo && repo != "" && registration.OnboardStatus == "completed"
}

// BootstrapStagingNamespace prepares namespace/default-deny only. It never
// retargets production Argo, copies credentials or creates workloads/domains.
func (h *Handler) BootstrapStagingNamespace(c *gin.Context) {
	var request struct {
		RepoFullName string `json:"repo_full_name" binding:"required"`
		DryRun       *bool  `json:"dry_run"`
		Reason       string `json:"reason"`
	}
	if c.ShouldBindJSON(&request) != nil || c.Param("env_name") != "staging" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a persisted staging environment and repository are required"})
		return
	}
	dryRun := request.DryRun == nil || *request.DryRun
	if !dryRun && strings.TrimSpace(request.Reason) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "apply requires a reason"})
		return
	}
	if h.repos == nil || h.repos.Projects == nil || h.repos.Environments == nil || h.repos.Onboardings == nil || h.k8sClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "staging bootstrap adapter unavailable"})
		return
	}
	project, err := h.repos.Projects.GetBySlug(c.Param("slug"))
	if err != nil || project == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project unavailable"})
		return
	}
	if !h.enforceUserProjectAccess(c, project.ID) {
		return
	}
	environment, envErr := h.repos.Environments.GetByProjectAndName(project.ID, "staging")
	registration, repoErr := h.repos.Onboardings.GetByRepo(c.Request.Context(), request.RepoFullName)
	if envErr != nil || repoErr != nil || !stagingNamespaceProof(project, environment, registration, request.RepoFullName) {
		c.JSON(http.StatusConflict, gin.H{"error": "persisted environment and repository ownership do not match"})
		return
	}
	if err = h.k8sClient.PrepareOwnedStagingNamespace(c.Request.Context(), environment.KubeNamespace, project.ID, dryRun); err != nil {
		status := http.StatusServiceUnavailable
		if k8s.IsNamespaceOwnershipError(err) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": "staging namespace/default-deny preparation refused", "ownership_refused": status == http.StatusConflict})
		return
	}
	c.JSON(http.StatusOK, gin.H{"dry_run": dryRun, "namespace": environment.KubeNamespace, "project_id": project.ID, "environment_id": environment.ID, "prepared": !dryRun, "scope": "namespace/default-deny only; no workload, custody or route activation"})
}
