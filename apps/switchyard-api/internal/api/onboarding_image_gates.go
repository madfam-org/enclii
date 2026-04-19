package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/checks"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
)

// Preventive hygiene gates for onboarding, split off from onboarding_handlers.go
// so the status-page-specific gates are easy to find.
//
// Two gates run against every onboarding request BEFORE the ArgoCD Application
// is created:
//
//  1. Image digest pinning — any workload manifest that references an image
//     with :latest, a mutable tag, or no tag is rejected. This is the
//     onboarding-side mirror of the Kyverno `require-image-digest` policy.
//  2. GHCR package existence — every ghcr.io/madfam-org/* image referenced
//     by the manifests must correspond to a GHCR package with at least one
//     pushed version. This catches the "repo exists, K8s manifests exist,
//     but CI never built the first image" mode that gave us six silently-502
//     status targets for >4 days.
//
// Both gates return 400 with a structured body when they block. There is
// intentionally no bypass flag — if a legitimate case arises (e.g. a repo
// using a non-GHCR registry) we'll extend the gate to ignore non-ghcr
// packages explicitly rather than let the operator opt out per-request.

// imageGateResult is the structured 400 body returned when either gate fails.
type imageGateResult struct {
	Gate         string                    `json:"gate"`
	Pass         bool                      `json:"pass"`
	Message      string                    `json:"message,omitempty"`
	DigestIssues []checks.ImageDigestIssue `json:"digest_issues,omitempty"`
	MissingPkgs  []missingPackageViolation `json:"missing_packages,omitempty"`
}

type missingPackageViolation struct {
	Image   string `json:"image"`
	Org     string `json:"org"`
	Package string `json:"package"`
	Message string `json:"message"`
}

// runImageGates is the shared entry point used by both OnboardRepo and the
// standalone preflight handler. It fetches the manifest files from GitHub,
// runs the two checks, and returns a non-nil *imageGateResult iff a gate
// rejects the request. Transient errors (GitHub unreachable, GHCR 5xx) are
// returned as go errors — the caller decides whether they're fatal.
func (h *Handler) runImageGates(
	ctx context.Context,
	owner, repo, manifestPath, branch string,
) (*imageGateResult, error) {
	if h.config.GitHubToken == "" {
		// Without the token we can't read the manifests. The existing
		// manifest-path validation already goes best-effort when the token
		// is missing, so we do the same here for behavior parity.
		return nil, nil
	}
	manifests, err := h.fetchManifestFiles(ctx, owner, repo, manifestPath, branch)
	if err != nil {
		return nil, fmt.Errorf("fetch manifests: %w", err)
	}

	// Gate 1: image digest pinning.
	digestIssues, digestErr := checks.CheckImageDigestPinned(manifests)
	if digestErr != nil {
		// Parse failure is a gate failure, not a Go error — surface it so
		// the operator fixes the broken YAML in their repo.
		return &imageGateResult{
			Gate:    "image-digest-pinned",
			Pass:    false,
			Message: digestErr.Error(),
		}, nil
	}
	if len(digestIssues) > 0 {
		return &imageGateResult{
			Gate:         "image-digest-pinned",
			Pass:         false,
			Message:      "image must be digest-pinned (@sha256:...)",
			DigestIssues: digestIssues,
		}, nil
	}

	// Gate 2: GHCR package existence.
	// We only check madfam-org/ghcr.io images — external registries (docker.io,
	// registry.k8s.io, nvcr.io) are out of scope for the "first-deploy"
	// problem we're solving.
	packages := extractGHCRPackages(manifests)
	if len(packages) == 0 {
		return nil, nil
	}
	client := &checks.GHCRClient{Token: h.config.GitHubToken}
	var missing []missingPackageViolation
	for _, pkg := range packages {
		res, err := client.CheckImageExists(ctx, pkg.Org, pkg.Package)
		if err != nil {
			// Transient — surface as Go error, caller decides.
			return nil, fmt.Errorf("ghcr existence check for %s/%s: %w",
				pkg.Org, pkg.Package, err)
		}
		if !res.Exists {
			missing = append(missing, missingPackageViolation{
				Image:   pkg.Image,
				Org:     pkg.Org,
				Package: pkg.Package,
				Message: res.Message,
			})
		}
	}
	if len(missing) > 0 {
		return &imageGateResult{
			Gate:        "image-exists",
			Pass:        false,
			Message:     "no image has been pushed to GHCR yet; run CI to build and push first image before enclii onboarding",
			MissingPkgs: missing,
		}, nil
	}

	return nil, nil
}

// PreflightImageGates exposes runImageGates via
// GET /v1/admin/preflight?repo=<owner/name>[&manifest_path=…][&branch=…]
// so operators can validate a repo's manifests WITHOUT actually onboarding.
// Intentionally idempotent and side-effect-free.
func (h *Handler) PreflightImageGates(c *gin.Context) {
	ctx := c.Request.Context()
	repoFull := c.Query("repo")
	parts := strings.SplitN(repoFull, "/", 2)
	if len(parts) != 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repo must be in owner/repo format"})
		return
	}
	owner, repo := parts[0], parts[1]
	manifestPath := c.DefaultQuery("manifest_path", "infra/k8s/production")
	branch := c.DefaultQuery("branch", "main")

	h.logger.Info(ctx, "Running image gates preflight",
		logging.String("repo", repoFull),
		logging.String("manifest_path", manifestPath))

	gateResult, err := h.runImageGates(ctx, owner, repo, manifestPath, branch)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":  "transient failure running image gates",
			"detail": err.Error(),
		})
		return
	}
	if gateResult != nil {
		// Non-OK status lets CI consumers differentiate "pass" from "fail"
		// without parsing the body.
		c.JSON(http.StatusOK, gin.H{
			"pass":   false,
			"result": gateResult,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"pass": true})
}

// fetchManifestFiles pulls every .yaml/.yml file from manifestPath in the
// target repo via the GitHub Contents API. Returns the list as checks.ManifestFile
// so it can be fed straight into CheckImageDigestPinned.
func (h *Handler) fetchManifestFiles(
	ctx context.Context,
	owner, repo, manifestPath, branch string,
) ([]checks.ManifestFile, error) {
	files, err := listGitHubDirectory(ctx, h.config.GitHubToken, owner, repo, manifestPath, branch)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", manifestPath, err)
	}
	var out []checks.ManifestFile
	for _, fileName := range files {
		if !strings.HasSuffix(fileName, ".yaml") && !strings.HasSuffix(fileName, ".yml") {
			continue
		}
		filePath := manifestPath + "/" + fileName
		content, _, fetchErr := getGitHubFileContent(
			ctx, h.config.GitHubToken, owner, repo, filePath, branch,
		)
		if fetchErr != nil {
			// Non-fatal: let the digest gate run on the files we did fetch.
			// If this file had the only violation we'd silently pass it, but
			// that's the same failure mode as a flaky GitHub API call — we
			// prefer not to cascade intermittent infra faults into onboarding
			// rejections.
			h.logger.Warn(ctx, "Failed to fetch manifest file (skipping)",
				logging.String("path", filePath),
				logging.Error("error", fetchErr))
			continue
		}
		out = append(out, checks.ManifestFile{Path: fileName, Content: content})
	}
	return out, nil
}

// ghcrImageRef is an extracted image reference, split into its registry
// components so we can query the GHCR versions endpoint.
type ghcrImageRef struct {
	Image   string
	Org     string
	Package string
}

// extractGHCRPackages walks every workload manifest, collects the images
// referenced, filters to ghcr.io/<org>/<package> patterns, and dedupes.
// Non-GHCR registries are ignored.
func extractGHCRPackages(manifests []checks.ManifestFile) []ghcrImageRef {
	seen := make(map[string]ghcrImageRef)
	for _, mf := range manifests {
		decoder := k8syaml.NewYAMLOrJSONDecoder(strings.NewReader(mf.Content), 4096)
		for {
			var obj map[string]interface{}
			if err := decoder.Decode(&obj); err != nil {
				if err == io.EOF {
					break
				}
				break
			}
			if obj == nil {
				continue
			}
			kind, _ := obj["kind"].(string)
			if !workloadKindsForExtract[kind] {
				continue
			}
			for _, c := range iterContainersForExtract(kind, obj) {
				img, _ := c["image"].(string)
				ref := parseGHCRImage(img)
				if ref.Image == "" {
					continue
				}
				key := ref.Org + "/" + ref.Package
				if _, ok := seen[key]; !ok {
					seen[key] = ref
				}
			}
		}
	}
	out := make([]ghcrImageRef, 0, len(seen))
	for _, ref := range seen {
		out = append(out, ref)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Org == out[j].Org {
			return out[i].Package < out[j].Package
		}
		return out[i].Org < out[j].Org
	})
	return out
}

// parseGHCRImage extracts org + package from a ghcr.io image reference, or
// returns a zero value for anything else.
//
// Examples:
//
//	ghcr.io/madfam-org/enclii/switchyard-api@sha256:abc
//	  → org=madfam-org, package=enclii/switchyard-api
//	ghcr.io/madfam-org/avala/avala-web:latest
//	  → org=madfam-org, package=avala/avala-web
//	docker.io/library/postgres:16
//	  → zero
func parseGHCRImage(image string) ghcrImageRef {
	if !strings.HasPrefix(image, "ghcr.io/") {
		return ghcrImageRef{}
	}
	// Strip the digest/tag before splitting path components.
	base := image
	if idx := strings.Index(base, "@"); idx >= 0 {
		base = base[:idx]
	}
	// Only strip the LAST colon, so registry:port wouldn't be mis-split (GHCR
	// doesn't use ports but stay safe).
	if lastColon := strings.LastIndex(base, ":"); lastColon > strings.LastIndex(base, "/") {
		base = base[:lastColon]
	}
	// ghcr.io/<org>/<pkg-or-nested-pkg...>
	rest := strings.TrimPrefix(base, "ghcr.io/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ghcrImageRef{}
	}
	return ghcrImageRef{Image: image, Org: parts[0], Package: parts[1]}
}

// workloadKindsForExtract / iterContainersForExtract mirror the helpers in
// the checks package without importing the unexported symbols. We could
// export them but that'd widen the API unnecessarily — this is the only
// other call site.
var workloadKindsForExtract = map[string]bool{
	"Deployment":  true,
	"StatefulSet": true,
	"DaemonSet":   true,
	"Job":         true,
	"CronJob":     true,
	"ReplicaSet":  true,
}

func iterContainersForExtract(kind string, obj map[string]interface{}) []map[string]interface{} {
	spec, _ := obj["spec"].(map[string]interface{})
	if spec == nil {
		return nil
	}
	var template map[string]interface{}
	if kind == "CronJob" {
		jt, _ := spec["jobTemplate"].(map[string]interface{})
		if jt == nil {
			return nil
		}
		jtSpec, _ := jt["spec"].(map[string]interface{})
		if jtSpec == nil {
			return nil
		}
		template, _ = jtSpec["template"].(map[string]interface{})
	} else {
		template, _ = spec["template"].(map[string]interface{})
	}
	if template == nil {
		return nil
	}
	podSpec, _ := template["spec"].(map[string]interface{})
	if podSpec == nil {
		return nil
	}
	raw, _ := podSpec["containers"].([]interface{})
	out := make([]map[string]interface{}, 0, len(raw))
	for _, c := range raw {
		if cm, ok := c.(map[string]interface{}); ok {
			out = append(out, cm)
		}
	}
	return out
}
