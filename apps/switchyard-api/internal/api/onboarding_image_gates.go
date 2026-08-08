package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/checks"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
)

// Preventive hygiene gates for onboarding, split off from onboarding_handlers.go
// so the status-page-specific gates are easy to find.
//
// Two gates run against every onboarding request BEFORE the ArgoCD Application
// is created:
//
//  1. Image digest pinning — any workload manifest that would deploy an image
//     with :latest, a mutable tag, no tag, or a placeholder digest is
//     rejected. This is the onboarding-side mirror of the Kyverno
//     `require-image-digest` policy.
//  2. GHCR package existence — every ghcr.io/madfam-org/* image referenced
//     by the manifests must correspond to a GHCR package with at least one
//     pushed version. This catches the "repo exists, K8s manifests exist,
//     but CI never built the first image" mode that gave us six silently-502
//     status targets for >4 days.
//
// Both gates judge the EFFECTIVE image: the manifest set is resolved through
// the directory's kustomization `images:` transformer first (see
// checks/kustomize_images.go), because the house convention is that CI writes
// the digest into kustomization.yaml, not into the Deployment. Judging the raw
// deployment file rejected correctly-pinned repos and implied a "fix" that
// would have broken the CI pin flow.
//
// A kustomization we cannot interpret is reported as its own `kustomize-images`
// failure rather than resolved optimistically: an image whose effective value
// is unknown is an image we must not certify.
//
// Both gates return 400 with a structured body when they block. There is
// intentionally no bypass flag — if a legitimate case arises (e.g. a repo
// using a non-GHCR registry) we'll extend the gate to ignore non-ghcr
// packages explicitly rather than let the operator opt out per-request.

// Gate identifiers reported in the 400 body.
const (
	gateImageDigestPinned = "image-digest-pinned"
	gateImageExists       = "image-exists"
	gateKustomizeImages   = "kustomize-images"
)

// imageGateResult is the structured 400 body returned when either gate fails.
type imageGateResult struct {
	Gate         string                    `json:"gate"`
	Pass         bool                      `json:"pass"`
	Message      string                    `json:"message,omitempty"`
	DigestIssues []checks.ImageDigestIssue `json:"digest_issues,omitempty"`
	MissingPkgs  []missingPackageViolation `json:"missing_packages,omitempty"`
	// Resolution reports what the gate actually looked at, so a failure is
	// never mistaken for a different failure (e.g. "kustomization present but
	// its images: block is empty").
	Resolution *imageGateResolution `json:"resolution,omitempty"`
}

// imageGateResolution is the read-proof record of a gate run, returned on
// PASS as well as failure. Without it, "we fetched nothing", "the
// kustomization declared no image overrides" and "everything resolved
// cleanly" all render as a silent clean pass.
type imageGateResolution struct {
	// Ran is false when the gates were skipped entirely.
	Ran bool `json:"ran"`
	// SkipReason explains a Ran=false run.
	SkipReason string `json:"skip_reason,omitempty"`
	// Summary is the one-line human rendering of the counts below.
	Summary string `json:"summary,omitempty"`
	checks.ImageResolutionStats
}

type missingPackageViolation struct {
	Image   string `json:"image"`
	Org     string `json:"org"`
	Package string `json:"package"`
	Message string `json:"message"`
}

// runImageGates is the shared entry point used by both OnboardRepo and the
// standalone preflight handler. It fetches the manifest files from GitHub,
// resolves them through the kustomization `images:` transformer, runs the two
// checks, and returns a non-nil *imageGateResult iff a gate rejects the
// request. Transient errors (GitHub unreachable, GHCR 5xx) are returned as go
// errors — the caller decides whether they're fatal.
//
// The second return value is the read-proof resolution record, populated on
// pass and failure alike. Callers that only need the verdict may discard it:
// this function logs it either way, and a failing gate also carries it in
// imageGateResult.Resolution.
func (h *Handler) runImageGates(
	ctx context.Context,
	owner, repo, manifestPath, branch string,
) (*imageGateResult, imageGateResolution, error) {
	if h.config.GitHubToken == "" {
		// Without the token we can't read the manifests. The existing
		// manifest-path validation already goes best-effort when the token
		// is missing, so we do the same here for behavior parity.
		return nil, imageGateResolution{
			SkipReason: "no GitHub token configured; manifests were never fetched",
		}, nil
	}
	manifests, err := h.fetchManifestFiles(ctx, owner, repo, manifestPath, branch)
	if err != nil {
		return nil, imageGateResolution{Ran: true}, fmt.Errorf("fetch manifests: %w", err)
	}

	// Resolve once, judge twice: both gates must see the image that would
	// actually deploy, not the pre-transformer value.
	images, stats, resolveErr := checks.CollectWorkloadImages(manifests)
	resolution := imageGateResolution{
		Ran:                  true,
		Summary:              stats.Summary(),
		ImageResolutionStats: stats,
	}
	h.logResolution(ctx, owner, repo, manifestPath, resolution)
	if resolveErr != nil {
		// An unreadable manifest or kustomization is a gate failure, not a Go
		// error and never a pass-through — an image we cannot resolve is an
		// image we cannot certify. Surface it so the operator fixes the repo.
		gate := gateImageDigestPinned
		var kustomizeErr *checks.KustomizeImagesError
		if errors.As(resolveErr, &kustomizeErr) {
			gate = gateKustomizeImages
		}
		return &imageGateResult{
			Gate:       gate,
			Pass:       false,
			Message:    resolveErr.Error(),
			Resolution: &resolution,
		}, resolution, nil
	}

	// Gate 1: image digest pinning.
	if digestIssues := checks.CheckImageDigestPinnedImages(images); len(digestIssues) > 0 {
		return &imageGateResult{
			Gate:         gateImageDigestPinned,
			Pass:         false,
			Message:      "image must be digest-pinned (@sha256:...)",
			DigestIssues: digestIssues,
			Resolution:   &resolution,
		}, resolution, nil
	}

	// Gate 2: GHCR package existence.
	// We only check madfam-org/ghcr.io images — external registries (docker.io,
	// registry.k8s.io, nvcr.io) are out of scope for the "first-deploy"
	// problem we're solving.
	packages := extractGHCRPackages(images)
	if len(packages) == 0 {
		return nil, resolution, nil
	}
	client := &checks.GHCRClient{Token: h.config.GitHubToken}
	var missing []missingPackageViolation
	for _, pkg := range packages {
		res, err := client.CheckImageExists(ctx, pkg.Org, pkg.Package)
		if err != nil {
			// Transient — surface as Go error, caller decides.
			return nil, resolution, fmt.Errorf("ghcr existence check for %s/%s: %w",
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
			Gate:        gateImageExists,
			Pass:        false,
			Message:     "no image has been pushed to GHCR yet; run CI to build and push first image before enclii onboarding",
			MissingPkgs: missing,
			Resolution:  &resolution,
		}, resolution, nil
	}

	return nil, resolution, nil
}

// logResolution emits the counts behind every gate run. A gate that scanned
// zero manifests, or resolved zero images through a kustomization that does
// have an images: block, is a measurement failure that would otherwise look
// exactly like a clean pass.
func (h *Handler) logResolution(
	ctx context.Context,
	owner, repo, manifestPath string,
	resolution imageGateResolution,
) {
	h.logger.Info(ctx, "Image gates resolved manifest images",
		logging.String("repo", owner+"/"+repo),
		logging.String("manifest_path", manifestPath),
		logging.Int("manifests_scanned", resolution.ManifestsScanned),
		logging.Bool("kustomization_found", resolution.KustomizationFound),
		logging.String("kustomization_file", resolution.KustomizationFile),
		logging.Int("kustomize_entries", resolution.KustomizeEntries),
		logging.Int("workload_images", resolution.WorkloadImages),
		logging.Int("resolved_by_kustomize", resolution.ResolvedByKustomize))
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

	gateResult, resolution, err := h.runImageGates(ctx, owner, repo, manifestPath, branch)
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
	// A pass carries its resolution too: an operator must be able to tell a
	// real pass from "we scanned nothing".
	c.JSON(http.StatusOK, gin.H{"pass": true, "resolution": resolution})
}

// fetchManifestFiles pulls every manifest file from manifestPath in the
// target repo via the GitHub Contents API. Returns the list as
// checks.ManifestFile so it can be fed straight into CollectWorkloadImages.
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
		if !isManifestFileName(fileName) {
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

// isManifestFileName selects the files worth fetching: YAML documents, plus
// the extensionless `Kustomization` spelling kustomize also accepts — without
// it the transformer would be invisible for a repo using that name and the
// gate would judge unresolved images.
func isManifestFileName(fileName string) bool {
	if strings.HasSuffix(fileName, ".yaml") || strings.HasSuffix(fileName, ".yml") {
		return true
	}
	return checks.IsKustomizationFile(fileName)
}

// ghcrImageRef is an extracted image reference, split into its registry
// components so we can query the GHCR versions endpoint.
type ghcrImageRef struct {
	Image   string
	Org     string
	Package string
}

// extractGHCRPackages filters already-resolved workload images to
// ghcr.io/<org>/<package> patterns and dedupes. Non-GHCR registries are
// ignored.
//
// It takes RESOLVED images on purpose: with the house convention the
// deployment YAML carries a bare name like `web`, and only the kustomization
// knows it means ghcr.io/madfam-org/nauta/web. Reading the raw manifest here
// would silently skip the existence check for every repo following the
// convention — the check would appear to pass while measuring nothing.
func extractGHCRPackages(images []checks.WorkloadImage) []ghcrImageRef {
	seen := make(map[string]ghcrImageRef)
	for _, img := range images {
		ref := parseGHCRImage(img.Image)
		if ref.Image == "" {
			continue
		}
		key := ref.Org + "/" + ref.Package
		if _, ok := seen[key]; !ok {
			seen[key] = ref
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
