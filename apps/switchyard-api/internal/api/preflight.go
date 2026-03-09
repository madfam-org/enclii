package api

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// PreflightOnboard validates manifests from a repo against the cluster's admission policies.
// POST /v1/admin/onboard/preflight
func (h *Handler) PreflightOnboard(c *gin.Context) {
	ctx := c.Request.Context()

	var req types.OnboardingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if h.config.GitHubToken == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "GitHub token not configured"})
		return
	}

	if h.k8sClient == nil || !h.k8sClient.IsValid() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "K8s client not available for dry-run validation"})
		return
	}

	parts := strings.SplitN(req.RepoFullName, "/", 2)
	if len(parts) != 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repo_full_name must be in owner/repo format"})
		return
	}

	manifestPath := req.ManifestPath
	if manifestPath == "" {
		manifestPath = "infra/k8s/production"
	}
	branch := "main"
	if req.Branch != nil {
		branch = *req.Branch
	}
	namespace := req.Namespace
	if namespace == "" {
		namespace = req.ProjectName
	}

	h.logger.Info(ctx, "Starting preflight validation",
		logging.String("repo", req.RepoFullName),
		logging.String("path", manifestPath))

	// List YAML files in manifest path
	files, err := listGitHubDirectory(ctx, h.config.GitHubToken, parts[0], parts[1], manifestPath, branch)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  fmt.Sprintf("manifest_path %q not found in %s", manifestPath, req.RepoFullName),
			"detail": err.Error(),
		})
		return
	}

	var violations []types.PreflightIssue

	for _, fileName := range files {
		if !strings.HasSuffix(fileName, ".yaml") && !strings.HasSuffix(fileName, ".yml") {
			continue
		}

		filePath := manifestPath + "/" + fileName

		// Fetch file content
		content, _, fetchErr := getGitHubFileContent(ctx, h.config.GitHubToken, parts[0], parts[1], filePath, branch)
		if fetchErr != nil {
			violations = append(violations, types.PreflightIssue{
				File:    fileName,
				Kind:    "unknown",
				Name:    "unknown",
				Message: fmt.Sprintf("failed to fetch: %v", fetchErr),
			})
			continue
		}

		// Parse YAML — may contain multiple documents
		decoder := yaml.NewYAMLOrJSONDecoder(strings.NewReader(content), 4096)
		for {
			var obj map[string]interface{}
			if decErr := decoder.Decode(&obj); decErr != nil {
				if decErr == io.EOF {
					break
				}
				violations = append(violations, types.PreflightIssue{
					File:    fileName,
					Kind:    "unknown",
					Name:    "unknown",
					Message: fmt.Sprintf("YAML parse error: %v", decErr),
				})
				break
			}
			if obj == nil {
				continue
			}

			kind, _ := obj["kind"].(string)
			metadata, _ := obj["metadata"].(map[string]interface{})
			name := "unknown"
			if metadata != nil {
				if n, ok := metadata["name"].(string); ok {
					name = n
				}
			}

			// Warn if project manifests contain NetworkPolicy resources
			// (these should be managed centrally by enclii, not by individual projects)
			if kind == "NetworkPolicy" {
				violations = append(violations, types.PreflightIssue{
					File:    fileName,
					Kind:    kind,
					Name:    name,
					Message: "NetworkPolicy resources should NOT be included in project manifests — they are centrally managed by enclii via the 'network' section in enclii.yaml. Remove this resource to avoid ArgoCD ownership conflicts.",
				})
			}

			// Server-side dry-run
			if dryErr := h.k8sClient.DryRunApply(ctx, namespace, obj); dryErr != nil {
				violations = append(violations, types.PreflightIssue{
					File:    fileName,
					Kind:    kind,
					Name:    name,
					Message: dryErr.Error(),
				})
			}
		}
	}

	result := types.PreflightResult{
		Pass:       len(violations) == 0,
		Violations: violations,
	}

	h.logger.Info(ctx, "Preflight validation complete",
		logging.String("repo", req.RepoFullName),
		logging.String("pass", fmt.Sprintf("%v", result.Pass)),
		logging.String("violations", fmt.Sprintf("%d", len(violations))))

	c.JSON(http.StatusOK, result)
}
