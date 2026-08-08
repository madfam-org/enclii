// Package checks implements onboarding-side preventive gates that stop
// degenerate targets from being registered in the platform (status page,
// ArgoCD, etc.) in the first place.
//
// Each check is a pure function over manifest bytes — no I/O, no cluster,
// no network. That makes them cheap to run before ArgoCD Application
// creation, and trivially testable from Go unit tests with fixtures.
//
// The checks here are the onboarding-side half of a two-layer defense:
//
//  1. Onboarding gate (this package) — rejects bad configurations before
//     they ever reach the cluster. Failure mode: 400 with an actionable
//     error so the operator fixes the repo, not the cluster state.
//  2. Kyverno `require-image-digest` ClusterPolicy (infra) — rejects the
//     same bad configurations at admission time if they sneak through.
//
// The two halves are intentional: we could rely on Kyverno alone, but
// by the time Kyverno speaks up, the status page already has a broken
// target on its timeline. Catching it at onboarding is cheaper.
//
// The digest rule is applied to the EFFECTIVE image — the value after the
// kustomization `images:` transformer has run (see kustomize_images.go),
// because that is what Kyverno will see at admission. The rule itself is
// unchanged: unresolved, :latest, mutable tag, no tag, or a placeholder
// digest all still fail.
package checks

import (
	"fmt"
	"strings"
)

// ImageDigestIssue describes a single workload that would deploy with a
// non-digest-pinned image.
type ImageDigestIssue struct {
	File      string `json:"file"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Container string `json:"container,omitempty"`
	// Image is the EFFECTIVE reference — post-kustomization — which is the
	// value that was judged.
	Image string `json:"image"`
	// ManifestImage is the reference as written in the workload YAML. When it
	// differs from Image, a kustomization override produced the judged value.
	ManifestImage string `json:"manifest_image,omitempty"`
	// Source is "manifest" or "kustomization": where the judged value came
	// from, so an operator is not left diffing two files by hand.
	Source string `json:"source,omitempty"`
	// KustomizeEntry is the images[] entry name that matched, when any did.
	KustomizeEntry string `json:"kustomize_entry,omitempty"`
	// KustomizationFile is the kustomization present in the manifest set,
	// whether or not it matched this image.
	KustomizationFile string `json:"kustomization_file,omitempty"`
	Message           string `json:"message"`
	Severity          string `json:"severity"` // "blocker" always — keeps shape symmetric with preflight
}

// CheckImageDigestPinned scans a batch of manifest YAML documents (one file's
// worth of content per entry, each potentially multi-doc) and returns any
// workloads whose container images are NOT digest-pinned once the directory's
// kustomization `images:` transformer has been applied.
//
// "Digest-pinned" means the effective image reference contains "@sha256:"
// followed by a digest that could identify a real image. Everything else —
// :latest, :v1.2.3, :main, no tag at all, the all-zero placeholder digest —
// is a blocker. Kyverno's require-image-digest policy uses the same rule in
// prod against the rendered output; we mirror it here so the onboarding gate
// matches admission.
//
// Returns an empty slice when all containers are pinned. Callers treat a
// non-empty result as "reject onboarding with 400". An unreadable manifest or
// kustomization is returned as an error, which callers treat as its own
// blocker class — never as a pass.
func CheckImageDigestPinned(manifests []ManifestFile) ([]ImageDigestIssue, error) {
	images, _, err := CollectWorkloadImages(manifests)
	if err != nil {
		return nil, err
	}
	return CheckImageDigestPinnedImages(images), nil
}

// CheckImageDigestPinnedImages applies the digest rule to images already
// resolved by CollectWorkloadImages. Callers that also run the GHCR existence
// gate use this form so both gates judge one shared, already-resolved list.
func CheckImageDigestPinnedImages(images []WorkloadImage) []ImageDigestIssue {
	var issues []ImageDigestIssue
	for _, img := range images {
		message := digestRuleViolation(img.Image)
		if message == "" {
			continue
		}
		issues = append(issues, ImageDigestIssue{
			File:              img.File,
			Kind:              img.Kind,
			Name:              img.Name,
			Container:         img.Container,
			Image:             img.Image,
			ManifestImage:     img.ManifestImage,
			Source:            img.Source,
			KustomizeEntry:    img.KustomizeEntry,
			KustomizationFile: img.KustomizationFile,
			Message:           message + imageOriginSuffix(img),
			Severity:          "blocker",
		})
	}
	return issues
}

// ManifestFile is a single manifest document to inspect, as fetched from the
// repo. The caller fetches; this package resolves the kustomization `images:`
// transformer itself (kustomize_images.go) rather than shelling out to
// `kustomize`, which the API pod has no checkout for.
type ManifestFile struct {
	Path    string
	Content string
}

// digestRuleViolation returns why an effective image reference fails the
// digest rule, or "" when it passes.
func digestRuleViolation(image string) string {
	if !imageIsDigestPinned(image) {
		return digestIssueMessage(image)
	}
	return placeholderDigestMessage(image)
}

// imageIsDigestPinned reports whether an image reference is pinned by sha256
// digest (e.g. "ghcr.io/foo/bar@sha256:abc...").
func imageIsDigestPinned(image string) bool {
	return strings.Contains(image, "@sha256:")
}

// placeholderDigestMessage rejects digests that are syntactically pinned but
// cannot identify a real image. The all-zero digest is the house convention
// for "CI has not pinned this yet" — onboarding a project whose image was
// never built is exactly the failure this gate exists to catch, so it must
// not pass merely because it parses.
func placeholderDigestMessage(image string) string {
	const marker = "@sha256:"
	idx := strings.Index(image, marker)
	if idx < 0 {
		return ""
	}
	hex := image[idx+len(marker):]
	if hex == "" {
		return fmt.Sprintf(
			"image %q has an empty @sha256: digest — it identifies no image; let CI commit a real digest",
			image,
		)
	}
	if strings.Trim(hex, "0") != "" {
		return ""
	}
	return fmt.Sprintf(
		"image %q is pinned to the all-zero placeholder digest — CI has not pushed a real image yet; run the build workflow and let it commit the digest",
		image,
	)
}

// digestIssueMessage renders a human-readable explanation.
func digestIssueMessage(image string) string {
	lastSlash := strings.LastIndex(image, "/")
	lastPart := image
	if lastSlash >= 0 {
		lastPart = image[lastSlash+1:]
	}
	if strings.HasSuffix(image, ":latest") || !strings.Contains(lastPart, ":") {
		return fmt.Sprintf(
			"image %q is not digest-pinned (uses :latest or no tag) — pin by @sha256: digest",
			image,
		)
	}
	return fmt.Sprintf(
		"image %q uses a mutable tag — pin by @sha256: digest instead",
		image,
	)
}

// imageOriginSuffix explains where the judged value came from, so a failing
// gate does not leave the operator comparing a Deployment against a
// kustomization by hand.
func imageOriginSuffix(img WorkloadImage) string {
	if img.Source == ImageSourceKustomization {
		return fmt.Sprintf(
			" (resolved from manifest image %q via %s images[] entry %q)",
			img.ManifestImage, img.KustomizationFile, img.KustomizeEntry,
		)
	}
	if img.KustomizationFile != "" {
		return fmt.Sprintf(
			" (%s did not rewrite it: no images[] entry matched %q — entry names must match the image name exactly as written in the manifest)",
			img.KustomizationFile, img.ManifestImage,
		)
	}
	return ""
}
