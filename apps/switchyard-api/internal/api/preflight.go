package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/checks"
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
	var notes []string

	var manifests []checks.ManifestFile
	for _, fileName := range files {
		if !isManifestFileName(fileName) {
			continue
		}
		content, _, fetchErr := getGitHubFileContent(ctx, h.config.GitHubToken, parts[0], parts[1], manifestPath+"/"+fileName, branch)
		if fetchErr != nil {
			violations = append(violations, types.PreflightIssue{File: fileName, Kind: "unknown", Name: "unknown", Message: fmt.Sprintf("failed to fetch: %v", fetchErr)})
			continue
		}
		manifests = append(manifests, checks.ManifestFile{Path: fileName, Content: content})
	}
	if len(violations) == 0 {
		objects, resolveErr := resolveAdmissionObjects(manifests, namespace)
		if resolveErr != nil {
			violations = append(violations, types.PreflightIssue{File: "kustomization", Kind: "unknown", Name: "unknown", Message: resolveErr.Error()})
		} else {
			admissionViolations, admissionNotes := admitPreflightObjects(ctx, h.k8sClient, namespace, objects)
			violations = append(violations, admissionViolations...)
			notes = append(notes, admissionNotes...)
		}
	}

	result := types.PreflightResult{
		Pass:       len(violations) == 0,
		Violations: violations,
		Notes:      notes,
	}

	h.logger.Info(ctx, "Preflight validation complete",
		logging.String("repo", req.RepoFullName),
		logging.String("pass", fmt.Sprintf("%v", result.Pass)),
		logging.String("violations", fmt.Sprintf("%d", len(violations))),
		logging.String("notes", fmt.Sprintf("%d", len(notes))))

	c.JSON(http.StatusOK, result)
}

// preflightCluster is the part of the Kubernetes client the admission
// preflight uses; *k8s.Client satisfies it.
type preflightCluster interface {
	NamespaceExists(ctx context.Context, namespace string) (bool, error)
	IsNamespacedKind(apiVersion, kind string) (bool, error)
	DryRunApply(ctx context.Context, namespace string, obj map[string]interface{}) error
}

const networkPolicyPreflightMessage = "NetworkPolicy resources should NOT be included in project manifests — they are centrally managed by enclii via the 'network' section in enclii.yaml. Remove this resource to avoid ArgoCD ownership conflicts."

// admitPreflightObjects runs the per-object checks: the NetworkPolicy rule and
// a server-side dry run of every object in the target namespace.
//
// A brand-new project's namespace does not exist until onboarding creates it,
// and the API server refuses any request for a namespaced object in a missing
// namespace, dry run or not (namespaces "<name>" not found). A dry-run
// create of the Namespace does not help: dry-run objects are never persisted,
// so the next request still finds no namespace. So when the namespace is
// missing, namespaced objects skip the dry run and the result carries a note;
// cluster-scoped objects are still dry-run, and every static check still runs.
// The skip is decided by an explicit NotFound lookup and the object's
// discovered scope, never by matching a dry-run error, so no admission
// rejection can be hidden by it.
func admitPreflightObjects(ctx context.Context, cluster preflightCluster, namespace string, objects []admissionObject) ([]types.PreflightIssue, []string) {
	var violations []types.PreflightIssue
	namespaceMissing := false
	exists, lookupErr := cluster.NamespaceExists(ctx, namespace)
	switch {
	case lookupErr != nil:
		// Unknown is not missing: report the failure and dry-run every object
		// as before.
		violations = append(violations, types.PreflightIssue{File: "namespace", Kind: "Namespace", Name: namespace, Message: fmt.Sprintf("could not look up the target namespace: %v", lookupErr)})
	case !exists:
		namespaceMissing = true
	}

	skipped := 0
	for _, item := range objects {
		obj := item.Object
		apiVersion, _ := obj["apiVersion"].(string)
		kind, _ := obj["kind"].(string)
		metadata, _ := obj["metadata"].(map[string]interface{})
		name, _ := metadata["name"].(string)
		if kind == "NetworkPolicy" {
			violations = append(violations, types.PreflightIssue{File: item.File, Kind: kind, Name: name, Message: networkPolicyPreflightMessage})
		}
		if namespaceMissing {
			namespaced, scopeErr := cluster.IsNamespacedKind(apiVersion, kind)
			if scopeErr != nil {
				violations = append(violations, types.PreflightIssue{File: item.File, Kind: kind, Name: name, Message: scopeErr.Error()})
				continue
			}
			if namespaced {
				// The dry run would also reject a nameless object; keep
				// that static check when the dry run cannot run.
				if name == "" {
					violations = append(violations, types.PreflightIssue{File: item.File, Kind: kind, Name: name, Message: "manifest missing metadata.name"})
				}
				skipped++
				continue
			}
		}
		if dryErr := cluster.DryRunApply(ctx, namespace, obj); dryErr != nil {
			violations = append(violations, types.PreflightIssue{File: item.File, Kind: kind, Name: name, Message: dryErr.Error()})
		}
	}

	var notes []string
	if skipped > 0 {
		notes = append(notes, fmt.Sprintf("namespace %q does not exist yet; onboarding creates it, so the admission dry run of %d namespaced object(s) was skipped. Static checks and cluster-scoped objects were still validated. Run the preflight again after onboarding for the full admission check.", namespace, skipped))
	}
	return violations, notes
}

func shouldSkipPreflightObject(apiVersion, kind string) bool {
	return kind == "Kustomization" && strings.HasPrefix(apiVersion, "kustomize.config.k8s.io/")
}
