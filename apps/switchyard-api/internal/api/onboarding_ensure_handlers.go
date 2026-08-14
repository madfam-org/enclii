package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/manifest"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/netpolicy"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func serializeStepResults(steps []stepResult) []map[string]string {
	results := make([]map[string]string, len(steps))
	for i, s := range steps {
		result := map[string]string{
			"name":   s.Name,
			"status": s.Status,
		}
		if s.Detail != "" {
			result["detail"] = s.Detail
		}
		results[i] = result
	}
	return results
}

// EnsureOnboarding is the first convergent entry point for recursive provisioning.
// POST /v1/admin/onboard/ensure
//
// For a new repository it delegates to the existing create-first onboarding flow.
// For an already-onboarded repository it re-runs the high-value reconciliation
// steps instead of returning 409, allowing Selva and operators to repair partial
// runtime state without falling back to raw provider tooling.
func (h *Handler) EnsureOnboarding(c *gin.Context) {
	ctx := c.Request.Context()

	raw, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}
	var req types.OnboardingRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.RepoFullName == "" || req.ProjectName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repo_full_name and project_name are required"})
		return
	}

	existing, lookupErr := h.repos.Onboardings.GetByRepo(ctx, req.RepoFullName)
	if lookupErr != nil || existing == nil {
		c.Request.Body = io.NopCloser(bytes.NewReader(raw))
		h.OnboardRepo(c)
		return
	}

	h.logger.Info(ctx, "Ensuring existing repo onboarding",
		logging.String("repo", req.RepoFullName),
		logging.String("project", req.ProjectName),
		logging.String("existing_status", existing.OnboardStatus))

	parts := strings.SplitN(req.RepoFullName, "/", 2)
	if len(parts) != 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repo_full_name must be in owner/repo format"})
		return
	}

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

	// The resolution record is dropped here on purpose: runImageGates logs it,
	// and a failing gate carries it in gateResult.Resolution.
	if gateResult, _, gateErr := h.runImageGates(ctx, parts[0], parts[1], manifestPath, branch); gateErr != nil {
		h.logger.Warn(ctx, "Image gate transient failure during onboarding ensure",
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

	// A nil config here is NOT benign: every domain, status entry and network
	// policy the manifest declares is silently dropped, and onboarding still
	// reports success. That is how madfam-org/angelia onboarded on 2026-08-13
	// with "Onboarding complete!" and zero of its five domains provisioned —
	// the repo is private and the server token could not read enclii.yaml.
	// Record it as a step so the operator sees the omission instead of
	// inferring it from an empty result days later.
	if encliiConfig == nil {
		h.recordStep(ctx, &steps, "manifest_fetch", false,
			fmt.Errorf("could not read enclii.yaml from %s — domains, status entries and network policies declared there are being SKIPPED (private repo without server access, missing file, or parse error; see switchyard-api logs)", req.RepoFullName))
	}

	project, err := h.repos.Projects.GetBySlug(req.ProjectName)
	if err != nil {
		if err == sql.ErrNoRows {
			project = &types.Project{
				Name: req.ProjectName,
				Slug: req.ProjectName,
			}
			if createErr := h.repos.Projects.Create(project); createErr != nil {
				h.recordStep(ctx, &steps, "project", true, createErr)
			} else {
				h.recordStep(ctx, &steps, "project", true, nil)
			}
		} else {
			h.recordStep(ctx, &steps, "project", true, err)
		}
	} else {
		h.recordStep(ctx, &steps, "project", true, nil)
	}

	var serviceNames []string
	if project != nil && encliiConfig != nil && encliiConfig.Metadata.Name != "" {
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
				h.recordStep(ctx, &steps, "service", false, createErr)
				serviceNames = append(serviceNames, svcName+" (failed)")
			} else {
				h.recordStep(ctx, &steps, "service", false, nil)
				serviceNames = append(serviceNames, svcName+" (created)")
			}
		}
	}

	var nsErr error
	if h.k8sClient != nil && h.k8sClient.IsValid() {
		nsErr = h.k8sClient.EnsureNamespace(ctx, namespace)
	} else {
		nsErr = fmt.Errorf("k8s client not available")
	}
	h.recordStep(ctx, &steps, "namespace", true, nsErr)

	projectConfig := generateProjectConfig(req.ProjectName, repoURL, branch, manifestPath, namespace)
	argocdYAML := generateArgocdApp(repoURL, manifestPath, namespace, appName, branch)
	argocdRegistration, argocdRegistrationErr := h.registerArgoCDApplication(ctx, argoCDRegistrationRequest{
		ProjectName:   req.ProjectName,
		RepoFullName:  req.RepoFullName,
		RepoURL:       repoURL,
		Branch:        branch,
		ManifestPath:  manifestPath,
		Namespace:     namespace,
		AppName:       appName,
		ProjectConfig: projectConfig,
		EnsureMode:    true,
	})
	argocdCommitSHA := argocdRegistration.CommitSHA
	h.recordStep(ctx, &steps, "argocd_config", true, argocdRegistrationErr)

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
			}
		} else {
			h.recordStep(ctx, &steps, "network_policies", false, fmt.Errorf("k8s client not available for NetworkPolicy apply"))
		}
	}

	var statusEntries []statusServiceEntry
	if encliiConfig != nil && encliiConfig.Spec.Status != nil {
		var statusErr error
		statusEntries, statusErr = h.registerStatusEntries(ctx, req.ProjectName, encliiConfig)
		h.recordStep(ctx, &steps, "status_registration", false, statusErr)
	}

	h.copyRegistryCredentials(ctx, namespace)
	h.recordStep(ctx, &steps, "registry_credentials", false, nil)

	var domainResults []string
	if project != nil && encliiConfig != nil && len(encliiConfig.Spec.Domains) > 0 {
		svcList, _ := h.repos.Services.ListByProject(project.ID)
		if len(svcList) > 0 {
			go h.provisionDomainsFromYAML(context.Background(), svcList[0], encliiConfig)
			for _, d := range encliiConfig.Spec.Domains {
				domainResults = append(domainResults, d.Name+" (provisioning)")
			}
		}
		h.recordStep(ctx, &steps, "domain_provisioning", false, nil)
	}

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

	if len(req.ProvisionSecrets) > 0 {
		var secErr error
		if h.secretsProvisioner != nil {
			secErr = h.secretsProvisioner.Create(ctx, namespace, req.ProjectName, req.SecretName, req.ProvisionSecrets)
		} else {
			secErr = fmt.Errorf("not configured (K8s client unavailable)")
		}
		h.recordStep(ctx, &steps, "secrets", false, secErr)
	}

	if req.ProvisionR2 != nil {
		// Same code path as `enclii storage create`: complete credentials or a
		// failed step. Never STORAGE_BACKEND=r2 with no access keys.
		_, r2Err := h.ensureProjectR2Bucket(ctx, r2ProvisionOptions{
			Project:    req.ProjectName,
			Namespace:  namespace,
			SecretName: req.SecretName,
			Bucket:     req.ProvisionR2.BucketName,
		})
		h.recordStep(ctx, &steps, "r2", false, r2Err)
	}

	ensureStatus := computeOnboardStatus(steps)
	var statusErrMsg *string
	if ensureStatus == "failed" {
		for _, s := range steps {
			if s.Err != nil && s.Critical {
				msg := fmt.Sprintf("%s: %s", s.Name, s.Err.Error())
				statusErrMsg = &msg
				break
			}
		}
	}
	serializedSteps := serializeStepResults(steps)
	ensureSnapshot := map[string]interface{}{
		"manifest_path": manifestPath,
		"namespace":     namespace,
		"branch":        branch,
		"services":      serviceNames,
		"domains":       domainResults,
		"desired_state": map[string]interface{}{
			"repo_full_name": req.RepoFullName,
			"repo_url":       repoURL,
			"project":        req.ProjectName,
			"namespace":      namespace,
			"manifest_path":  manifestPath,
			"branch":         branch,
			"argocd_app":     appName,
			"secret_name":    req.SecretName,
		},
		"last_ensure": map[string]interface{}{
			"status":        ensureStatus,
			"error":         statusErrMsg,
			"step_results":  serializedSteps,
			"reconciled_at": time.Now().UTC().Format(time.RFC3339),
		},
	}
	if encliiConfig != nil {
		ensureSnapshot["enclii_yaml_found"] = true
	}
	if len(statusEntries) > 0 {
		ensureSnapshot["status_entries"] = statusEntries
		ensureSnapshot["desired_state"].(map[string]interface{})["status_entries_count"] = len(statusEntries)
	}
	ensureSnapshot["argocd_registration_mode"] = argocdRegistration.Mode
	if argocdRegistration.Action != "" {
		ensureSnapshot["argocd_registration_action"] = argocdRegistration.Action
	}
	ensureSnapshot["desired_state"].(map[string]interface{})["argocd_registration_mode"] = argocdRegistration.Mode
	if argocdCommitSHA != "" {
		ensureSnapshot["argocd_commit"] = argocdCommitSHA
	}
	if req.ProvisionPostgres != nil {
		ensureSnapshot["desired_state"].(map[string]interface{})["postgres_database"] = req.ProvisionPostgres.DatabaseName
	}
	if len(req.ProvisionSecrets) > 0 {
		ensureSnapshot["desired_state"].(map[string]interface{})["secrets_count"] = len(req.ProvisionSecrets)
	}
	if req.ProvisionR2 != nil {
		ensureSnapshot["desired_state"].(map[string]interface{})["r2_bucket"] = req.ProvisionR2.BucketName
	}
	if err := h.repos.Onboardings.UpdateStatusAndMergeSnapshot(ctx, existing.ID, ensureStatus, statusErrMsg, ensureSnapshot); err != nil {
		h.logger.Error(ctx, "Failed to update onboarding status after ensure",
			logging.String("repo", req.RepoFullName),
			logging.Error("db_error", err))
	}

	response := gin.H{
		"status":                 ensureStatus,
		"mode":                   "repair",
		"reconciled":             true,
		"existing_onboarding_id": existing.ID,
		"previous_status":        existing.OnboardStatus,
		"step_results":           serializedSteps,
		"repo":                   req.RepoFullName,
		"project":                req.ProjectName,
		"argocd_yaml":            argocdYAML,
		"project_config":         projectConfig,
		"argocd_app":             appName,
		"argocd_commit":          argocdCommitSHA,
		"argocd_mode":            argocdRegistration.Mode,
		"argocd_action":          argocdRegistration.Action,
		"namespace":              namespace,
		"manifest_path":          manifestPath,
		"services":               serviceNames,
		"domains":                domainResults,
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
	if ensureStatus == "failed" {
		httpStatus = http.StatusInternalServerError
	}
	c.JSON(httpStatus, response)
}
