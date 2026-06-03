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
package checks

import (
	"fmt"
	"io"
	"strings"

	"k8s.io/apimachinery/pkg/util/yaml"
)

// ImageDigestIssue describes a single workload that would deploy with a
// non-digest-pinned image.
type ImageDigestIssue struct {
	File     string `json:"file"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Image    string `json:"image"`
	Message  string `json:"message"`
	Severity string `json:"severity"` // "blocker" always — keeps shape symmetric with preflight
}

// workloadKinds enumerates the K8s workload kinds whose containers we inspect.
// Matches the set used by selva-office/packages/tools's deploy_preflight.py
// so the onboarding gate and the pre-submit gate agree on what's a workload.
var workloadKinds = map[string]bool{
	"Deployment":  true,
	"StatefulSet": true,
	"DaemonSet":   true,
	"Job":         true,
	"CronJob":     true,
	"ReplicaSet":  true,
}

// CheckImageDigestPinned scans a batch of rendered manifest YAML documents
// (one file's worth of content per entry, each potentially multi-doc) and
// returns any workloads whose container images are NOT digest-pinned.
//
// "Digest-pinned" means the image reference contains "@sha256:". Everything
// else — :latest, :v1.2.3, :main, no tag at all — is a blocker. Kyverno's
// require-image-digest policy uses the same rule in prod; we mirror it
// here so the onboarding gate matches admission.
//
// Returns an empty slice when all containers are pinned. Callers treat a
// non-empty result as "reject onboarding with 400".
func CheckImageDigestPinned(manifests []ManifestFile) ([]ImageDigestIssue, error) {
	var issues []ImageDigestIssue
	for _, mf := range manifests {
		decoder := yaml.NewYAMLOrJSONDecoder(strings.NewReader(mf.Content), 4096)
		for {
			var obj map[string]interface{}
			if err := decoder.Decode(&obj); err != nil {
				if err == io.EOF {
					break
				}
				// Surface the parse error against the originating file so
				// the operator can find it, but don't abort the whole batch
				// — other files may still be clean and the caller can
				// treat a parse error as its own blocker class.
				return issues, fmt.Errorf("parse %s: %w", mf.Path, err)
			}
			if obj == nil {
				continue
			}
			kind, _ := obj["kind"].(string)
			if !workloadKinds[kind] {
				continue
			}
			name := "unknown"
			if md, ok := obj["metadata"].(map[string]interface{}); ok {
				if n, ok := md["name"].(string); ok {
					name = n
				}
			}
			for _, container := range iterContainers(kind, obj) {
				image, _ := container["image"].(string)
				if image == "" {
					continue
				}
				if !imageIsDigestPinned(image) {
					issues = append(issues, ImageDigestIssue{
						File:     mf.Path,
						Kind:     kind,
						Name:     name,
						Image:    image,
						Message:  digestIssueMessage(image),
						Severity: "blocker",
					})
				}
			}
		}
	}
	return issues, nil
}

// ManifestFile is a single rendered-manifest document to inspect.
// The caller is responsible for resolving kustomize — this package does
// NOT run kustomize, because the onboarding handler already reads the
// target repo via the GitHub Contents API and we don't want to shell out
// from inside the API pod.
type ManifestFile struct {
	Path    string
	Content string
}

// imageIsDigestPinned reports whether an image reference is pinned by sha256
// digest (e.g. "ghcr.io/foo/bar@sha256:abc...").
func imageIsDigestPinned(image string) bool {
	return strings.Contains(image, "@sha256:")
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

// iterContainers returns the pod-spec containers for a workload manifest.
// Handles the CronJob-wraps-jobTemplate nesting quirk.
func iterContainers(kind string, obj map[string]interface{}) []map[string]interface{} {
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
