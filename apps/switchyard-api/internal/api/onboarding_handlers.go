package api

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/manifest"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	k8scorev1 "k8s.io/api/core/v1"
	k8smetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/netpolicy"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// stepResult tracks the outcome of an onboarding step.
type stepResult struct {
	Name     string `json:"name"`
	Critical bool   `json:"-"`
	Err      error  `json:"-"`
	Status   string `json:"status"` // "ok", "failed", "skipped"
	Detail   string `json:"detail,omitempty"`
}

// computeOnboardStatus determines overall status from step results.
func computeOnboardStatus(steps []stepResult) string {
	for _, s := range steps {
		if s.Err != nil && s.Critical {
			return "failed"
		}
	}
	for _, s := range steps {
		if s.Err != nil {
			return "partial"
		}
	}
	return "completed"
}

// recordStep appends a step result and logs appropriately.
func (h *Handler) recordStep(ctx context.Context, steps *[]stepResult, name string, critical bool, err error) {
	s := stepResult{Name: name, Critical: critical, Err: err, Status: "ok"}
	if err != nil {
		s.Status = "failed"
		s.Detail = err.Error()
		if critical {
			h.logger.Error(ctx, "Onboarding step failed (critical)",
				logging.String("step", name), logging.Error("error", err))
		} else {
			h.logger.Warn(ctx, "Onboarding step failed (non-critical)",
				logging.String("step", name), logging.Error("error", err))
		}
	}
	*steps = append(*steps, s)
}

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

	// Validate repo format
	parts := strings.SplitN(req.RepoFullName, "/", 2)
	if len(parts) != 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repo_full_name must be in owner/repo format"})
		return
	}

	// Resolve defaults
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

	// Validate manifest path exists in target repo before proceeding
	if h.config.GitHubToken != "" {
		files, pathErr := listGitHubDirectory(ctx, h.config.GitHubToken, parts[0], parts[1], manifestPath, branch)
		if pathErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":         fmt.Sprintf("manifest_path %q not found in %s (branch: %s)", manifestPath, req.RepoFullName, branch),
				"detail":        pathErr.Error(),
				"manifest_path": manifestPath,
			})
			return
		}
		hasYAML := false
		for _, f := range files {
			if strings.HasSuffix(f, ".yaml") || strings.HasSuffix(f, ".yml") {
				hasYAML = true
				break
			}
		}
		if !hasYAML {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":         fmt.Sprintf("manifest_path %q exists but contains no .yaml/.yml files", manifestPath),
				"files_found":   files,
				"manifest_path": manifestPath,
			})
			return
		}
	}

	// Preventive hygiene gates: reject onboarding up-front if either
	//   (a) the manifests reference a `:latest` / mutable / unpinned image, or
	//   (b) the image has never been pushed to GHCR yet.
	// See onboarding_image_gates.go for rationale and scope.
	if gateResult, gateErr := h.runImageGates(ctx, parts[0], parts[1], manifestPath, branch); gateErr != nil {
		h.logger.Warn(ctx, "Image gate transient failure (treating as soft block)",
			logging.String("repo", req.RepoFullName),
			logging.Error("error", gateErr))
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":  "failed to run image preflight checks",
			"detail": gateErr.Error(),
		})
		return
	} else if gateResult != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  gateResult.Message,
			"gate":   gateResult.Gate,
			"result": gateResult,
		})
		return
	}

	var steps []stepResult

	encliiConfig := manifest.FetchAndParse(ctx, h.logger, h.config.GitHubToken, req.RepoFullName, "HEAD")

	// Step: Find or create project
	project, err := h.repos.Projects.GetBySlug(req.ProjectName)
	if err != nil {
		if err == sql.ErrNoRows {
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

	// Step: Create service from enclii.yaml metadata if available
	var serviceNames []string
	if encliiConfig != nil && encliiConfig.Metadata.Name != "" {
		svcName := encliiConfig.Metadata.Name
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

	// Generate config.json for the ApplicationSet generator
	projectConfig := generateProjectConfig(req.ProjectName, repoURL, branch, manifestPath, namespace)
	argocdYAML := generateArgocdApp(repoURL, manifestPath, namespace, appName, branch)

	// Step: Ensure namespace (critical)
	var nsErr error
	if h.k8sClient != nil && h.k8sClient.IsValid() {
		nsErr = h.k8sClient.EnsureNamespace(ctx, namespace)
	} else {
		nsErr = fmt.Errorf("k8s client not available")
	}
	h.recordStep(ctx, &steps, "namespace", true, nsErr)

	// Step: ArgoCD registration (critical). Default mode preserves the legacy
	// Enclii repo ApplicationSet config write; runtime mode reconciles the
	// Application CR directly from the client repo declaration.
	argocdRegistration, argocdRegistrationErr := h.registerArgoCDApplication(ctx, argoCDRegistrationRequest{
		ProjectName:   req.ProjectName,
		RepoFullName:  req.RepoFullName,
		RepoURL:       repoURL,
		Branch:        branch,
		ManifestPath:  manifestPath,
		Namespace:     namespace,
		AppName:       appName,
		ProjectConfig: projectConfig,
	})
	argocdCommitSHA := argocdRegistration.CommitSHA
	h.recordStep(ctx, &steps, "argocd_config", true, argocdRegistrationErr)

	// Step: Generate and apply NetworkPolicies via K8s API (non-critical)
	if encliiConfig != nil && encliiConfig.Spec.Network != nil {
		npSpec := convertNetworkSpec(encliiConfig.Spec.Network)
		npYAML, npGenErr := netpolicy.GeneratePolicies(namespace, req.ProjectName, npSpec)
		if npGenErr != nil {
			h.recordStep(ctx, &steps, "network_policies", false, npGenErr)
		} else if h.k8sClient != nil && h.k8sClient.IsValid() {
			npCount, npErr := h.k8sClient.ApplyNetworkPolicies(ctx, namespace, npYAML)
			if npErr != nil {
				h.recordStep(ctx, &steps, "network_policies", false, fmt.Errorf("applied %d policies before error: %w", npCount, npErr))
			} else {
				h.recordStep(ctx, &steps, "network_policies", false, nil)
				h.logger.Info(ctx, "Applied NetworkPolicies via K8s API",
					logging.String("namespace", namespace),
					logging.Int("policy_count", npCount))
			}
		} else {
			h.recordStep(ctx, &steps, "network_policies", false, fmt.Errorf("k8s client not available for NetworkPolicy apply"))
		}
	}

	// Step: Register status page entries (non-critical)
	if encliiConfig != nil && encliiConfig.Spec.Status != nil {
		statusErr := h.registerStatusEntries(ctx, req.ProjectName, encliiConfig)
		h.recordStep(ctx, &steps, "status_registration", false, statusErr)
	}

	// Step: Copy registry credentials (important, not critical)
	h.copyRegistryCredentials(ctx, namespace)
	// copyRegistryCredentials logs internally; record as non-critical ok
	h.recordStep(ctx, &steps, "registry_credentials", false, nil)

	// Step: Provision domains from enclii.yaml (if available)
	var domainResults []string
	if encliiConfig != nil && len(encliiConfig.Spec.Domains) > 0 {
		svcList, _ := h.repos.Services.ListByProject(project.ID)
		if len(svcList) > 0 {
			go h.provisionDomainsFromYAML(context.Background(), svcList[0], encliiConfig)
			for _, d := range encliiConfig.Spec.Domains {
				domainResults = append(domainResults, d.Name+" (provisioning)")
			}
		}
		h.recordStep(ctx, &steps, "domain_provisioning", false, nil)
	}

	// Step: Provision Postgres database + PgBouncer (optional, important)
	if req.ProvisionPostgres != nil {
		var pgErr error
		if h.postgresProvisioner != nil {
			pgErr = h.postgresProvisioner.Provision(ctx, req.ProvisionPostgres)
			if pgErr == nil {
				roleName := req.ProvisionPostgres.RoleName
				if roleName == "" {
					roleName = req.ProvisionPostgres.DatabaseName
				}
				if h.pgbouncerUpdater != nil {
					if pbErr := h.pgbouncerUpdater.AddDatabase(ctx, req.ProvisionPostgres.DatabaseName, roleName, req.ProvisionPostgres.RolePassword); pbErr != nil {
						h.recordStep(ctx, &steps, "pgbouncer", false, pbErr)
					}
				}
			}
		} else {
			pgErr = fmt.Errorf("not configured (POSTGRES_ADMIN_URL not set)")
		}
		h.recordStep(ctx, &steps, "postgres", false, pgErr)
	}

	// Step: Create K8s secrets (optional, important)
	if len(req.ProvisionSecrets) > 0 {
		var secErr error
		if h.secretsProvisioner != nil {
			secErr = h.secretsProvisioner.Create(ctx, namespace, req.ProjectName, req.SecretName, req.ProvisionSecrets)
		} else {
			secErr = fmt.Errorf("not configured (K8s client unavailable)")
		}
		h.recordStep(ctx, &steps, "secrets", false, secErr)
	}

	// Step: Create R2 bucket (optional, important)
	if req.ProvisionR2 != nil {
		var r2Err error
		if h.r2Provisioner != nil {
			r2Entries, bucketErr := h.r2Provisioner.CreateBucket(ctx, req.ProvisionR2.BucketName)
			if bucketErr != nil {
				r2Err = bucketErr
			} else if h.secretsProvisioner != nil {
				r2Err = h.secretsProvisioner.AppendEntries(ctx, namespace, req.ProjectName, req.SecretName, r2Entries)
			}
		} else {
			r2Err = fmt.Errorf("not configured (Cloudflare credentials not set)")
		}
		h.recordStep(ctx, &steps, "r2", false, r2Err)
	}

	// Compute overall status
	onboardStatus := computeOnboardStatus(steps)

	// Register onboarding
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
	configSnapshot["argocd_registration_mode"] = argocdRegistration.Mode
	if argocdRegistration.Action != "" {
		configSnapshot["argocd_registration_action"] = argocdRegistration.Action
	}
	if argocdCommitSHA != "" {
		configSnapshot["argocd_commit"] = argocdCommitSHA
	}

	reg := &types.OnboardingRegistration{
		ProjectID:      project.ID,
		RepoFullName:   req.RepoFullName,
		ArgocdAppName:  &appName,
		OnboardStatus:  onboardStatus,
		ConfigSnapshot: configSnapshot,
	}
	if err := h.repos.Onboardings.Create(ctx, reg); err != nil {
		h.logger.Error(ctx, "Failed to register onboarding",
			logging.String("repo", req.RepoFullName),
			logging.Error("db_error", err))
	}

	h.logger.Info(ctx, "Repo onboarding finished",
		logging.String("repo", req.RepoFullName),
		logging.String("project", req.ProjectName),
		logging.String("status", onboardStatus))

	// Build next_steps
	nextSteps := []string{}
	if argocdRegistration.Mode == "runtime" && argocdRegistrationErr == nil {
		nextSteps = append(nextSteps, fmt.Sprintf("1. ArgoCD Application reconciled via runtime mode (%s)", argocdRegistration.Action))
	} else if argocdCommitSHA != "" {
		nextSteps = append(nextSteps, fmt.Sprintf("1. Legacy project config committed (%s) — ApplicationSet generates ArgoCD app", argocdCommitSHA[:8]))
	} else {
		nextSteps = append(nextSteps, fmt.Sprintf("1. ArgoCD registration requires remediation for %s", req.ProjectName))
	}
	nextSteps = append(nextSteps,
		"2. Ensure GitHub webhook is configured to POST to https://api.enclii.dev/v1/webhooks/github",
		"3. Push to main branch to trigger first auto-deploy",
		"4. Check lifecycle events: GET /v1/lifecycle/timeline/"+req.RepoFullName,
	)

	// Build step_results for response
	stepResults := make([]gin.H, len(steps))
	for i, s := range steps {
		sr := gin.H{"name": s.Name, "status": s.Status}
		if s.Detail != "" {
			sr["detail"] = s.Detail
		}
		stepResults[i] = sr
	}

	response := gin.H{
		"status":         onboardStatus,
		"step_results":   stepResults,
		"repo":           req.RepoFullName,
		"project":        req.ProjectName,
		"next_steps":     nextSteps,
		"argocd_yaml":    argocdYAML,
		"project_config": projectConfig,
		"argocd_app":     appName,
		"argocd_commit":  argocdCommitSHA,
		"argocd_mode":    argocdRegistration.Mode,
		"argocd_action":  argocdRegistration.Action,
		"namespace":      namespace,
		"manifest_path":  manifestPath,
		"services":       serviceNames,
		"domains":        domainResults,
	}
	if req.ProvisionPostgres != nil {
		response["postgres_database"] = req.ProvisionPostgres.DatabaseName
	}
	if len(req.ProvisionSecrets) > 0 {
		response["secrets_count"] = len(req.ProvisionSecrets)
	}
	if req.ProvisionR2 != nil {
		response["r2_bucket"] = req.ProvisionR2.BucketName
	}

	httpStatus := http.StatusOK
	if onboardStatus == "failed" {
		httpStatus = http.StatusInternalServerError
	}
	c.JSON(httpStatus, response)
}

// copyRegistryCredentials copies ghcr-credentials from the enclii namespace to a target namespace.
// This allows pods in the target namespace to pull images from GHCR.
func (h *Handler) copyRegistryCredentials(ctx context.Context, targetNamespace string) {
	if h.serviceReconciler == nil {
		h.logger.Warn(ctx, "Service reconciler not available, skipping credential copy",
			logging.String("namespace", targetNamespace))
		return
	}

	// The reconciler already has ensureRegistryCredentials — leverage it indirectly
	// by calling EnsureNamespace on the k8s client (which the reconciler's ensureNamespace does)
	// Since the namespace is already created, and the reconciler's
	// ensureNamespace handles credential copying, we can use the reconciler directly
	if h.k8sClient == nil || !h.k8sClient.IsValid() {
		return
	}

	const secretName = "ghcr-credentials" // #nosec G101 -- secret reference name, not a credential
	const sourceNamespace = "enclii"

	secretClient := h.k8sClient.Clientset.CoreV1().Secrets(targetNamespace)

	// Check if secret already exists
	_, err := secretClient.Get(ctx, secretName, k8smetav1.GetOptions{})
	if err == nil {
		return // Already exists
	}

	// Get source secret
	sourceClient := h.k8sClient.Clientset.CoreV1().Secrets(sourceNamespace)
	sourceSecret, err := sourceClient.Get(ctx, secretName, k8smetav1.GetOptions{})
	if err != nil {
		h.logger.Warn(ctx, "Source registry credentials not found, skipping copy",
			logging.String("namespace", targetNamespace),
			logging.Error("error", err))
		return
	}

	// Create copy in target namespace
	newSecret := &k8scorev1.Secret{
		ObjectMeta: k8smetav1.ObjectMeta{
			Name:      secretName,
			Namespace: targetNamespace,
			Labels: map[string]string{
				"enclii.dev/managed-by":  "onboarding-api",
				"enclii.dev/copied-from": sourceNamespace,
			},
		},
		Type: sourceSecret.Type,
		Data: sourceSecret.Data,
	}

	if _, err := secretClient.Create(ctx, newSecret, k8smetav1.CreateOptions{}); err != nil {
		h.logger.Warn(ctx, "Failed to copy registry credentials (non-fatal)",
			logging.String("namespace", targetNamespace),
			logging.Error("error", err))
	} else {
		h.logger.Info(ctx, "Copied registry credentials to namespace",
			logging.String("namespace", targetNamespace))
	}
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

// convertNetworkSpec converts the parsed enclii.yaml network section into the
// netpolicy package's NetworkSpec type (avoids circular import).
func convertNetworkSpec(n *manifest.EncliiYAMLNetwork) netpolicy.NetworkSpec {
	spec := netpolicy.NetworkSpec{}
	for _, s := range n.Services {
		spec.Services = append(spec.Services, netpolicy.ServiceSpec{
			Name:    s.Name,
			Label:   s.Label,
			Port:    s.Port,
			Ingress: s.Ingress,
			Egress:  s.Egress,
		})
	}
	for _, c := range n.Custom {
		spec.Custom = append(spec.Custom, netpolicy.CustomRule{
			Name:      c.Name,
			From:      c.From,
			To:        c.To,
			Port:      c.Port,
			Direction: c.Direction,
		})
	}
	return spec
}

// registerStatusEntries auto-registers status page entries from enclii.yaml.
// It reads the current configmap from GitHub, appends new entries idempotently,
// and commits the update back. ArgoCD syncs the updated configmap.
func (h *Handler) registerStatusEntries(ctx context.Context, projectName string, config *manifest.EncliiYAML) error {
	if config.Spec.Status == nil {
		return nil
	}

	status := config.Spec.Status
	// Default enabled to true
	if !status.Enabled && len(status.Entries) == 0 {
		return nil
	}

	// If no explicit entries but domains exist, auto-derive entries
	entries := status.Entries
	if len(entries) == 0 && len(config.Spec.Domains) > 0 {
		for _, d := range config.Spec.Domains {
			entries = append(entries, manifest.EncliiYAMLStatusEntry{
				Name:  d.Name,
				URL:   "https://" + d.Name,
				Group: projectName,
			})
		}
	}

	if len(entries) == 0 {
		return nil
	}

	if h.config.GitHubToken == "" || h.config.EncliiRepoOwner == "" {
		return fmt.Errorf("GitHub token or enclii repo not configured for status registration")
	}

	// Read current configmap from GitHub
	configmapPath := "apps/status/k8s/madfam/configmap.yaml"
	content, sha, err := getGitHubFileContent(ctx, h.config.GitHubToken, h.config.EncliiRepoOwner, h.config.EncliiRepoName, configmapPath, "main")
	if err != nil {
		return fmt.Errorf("failed to read status configmap: %w", err)
	}

	// Check if entries for this project's group already exist (idempotent)
	for _, entry := range entries {
		if strings.Contains(content, entry.URL) {
			h.logger.Info(ctx, "Status entry already exists, skipping",
				logging.String("url", entry.URL))
			continue
		}

		// Find the services-config section and append the entry
		// The configmap has a JSON array in the services-config key
		marker := `"group": "` + entry.Group + `"`
		if !strings.Contains(content, marker) {
			// New group — append before the closing bracket of the JSON array
			newEntry := fmt.Sprintf(`    {"name": "%s", "url": "%s", "group": "%s"}`, entry.Name, entry.URL, entry.Group)
			// Find the last ] in the services-config value
			lastBracket := strings.LastIndex(content, "]")
			if lastBracket > 0 {
				// Check if the array is empty
				beforeBracket := strings.TrimRight(content[:lastBracket], " \n\r\t")
				if strings.HasSuffix(beforeBracket, "[") {
					content = beforeBracket + "\n" + newEntry + "\n  " + content[lastBracket:]
				} else {
					content = beforeBracket + ",\n" + newEntry + "\n  " + content[lastBracket:]
				}
			}
		}
	}

	// Commit the updated configmap
	commitMsg := fmt.Sprintf("feat(status): auto-register %s status entries\n\nGenerated by POST /v1/admin/onboard from enclii.yaml status section.", projectName)
	_, err = createOrUpdateGitHubFileWithSHA(ctx, h.config.GitHubToken, h.config.EncliiRepoOwner, h.config.EncliiRepoName, configmapPath, []byte(content), commitMsg, "main", sha)
	if err != nil {
		return fmt.Errorf("failed to commit status configmap: %w", err)
	}

	h.logger.Info(ctx, "Registered status page entries",
		logging.String("project", projectName),
		logging.Int("entry_count", len(entries)))

	return nil
}
