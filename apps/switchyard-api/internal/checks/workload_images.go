package checks

import (
	"fmt"
	"io"
	"strings"

	"k8s.io/apimachinery/pkg/util/yaml"
)

// Workload image collection: the single place that turns a fetched manifest
// set into the list of images that would ACTUALLY deploy.
//
// Both onboarding image gates consume this list, so they agree on the
// effective value: the digest gate judges it, and the GHCR existence gate
// looks up the package it names. Before this existed each gate walked the raw
// YAML itself and both judged a pre-transformer value that never reaches the
// cluster.

// Image sources reported on every collected reference.
const (
	// ImageSourceManifest means the workload manifest's own value stands.
	ImageSourceManifest = "manifest"
	// ImageSourceKustomization means a kustomization images[] entry rewrote it.
	ImageSourceKustomization = "kustomization"
)

// WorkloadImage is one container image reference from a workload manifest,
// reported as the EFFECTIVE value — after the kustomization `images:`
// transformer has been applied.
type WorkloadImage struct {
	// File is the manifest file the workload was read from.
	File string `json:"file"`
	// Kind and Name identify the workload.
	Kind string `json:"kind"`
	Name string `json:"name"`
	// Container is the container name inside the pod spec.
	Container string `json:"container,omitempty"`
	// Image is the effective reference: what would actually deploy.
	Image string `json:"image"`
	// ManifestImage is the reference exactly as written in the YAML. Equal to
	// Image when no kustomization entry matched.
	ManifestImage string `json:"manifest_image"`
	// Source is ImageSourceManifest or ImageSourceKustomization.
	Source string `json:"source"`
	// KustomizeEntry names the images[] entries that matched, comma-joined.
	KustomizeEntry string `json:"kustomize_entry,omitempty"`
	// KustomizationFile is the kustomization present in the manifest set, set
	// whether or not it matched this image, so "a kustomization existed and
	// still did not rewrite this" is visible.
	KustomizationFile string `json:"kustomization_file,omitempty"`
}

// ImageResolutionStats is the read-proof record of what a gate run looked at.
// It is reported on pass as well as failure: without it, "no manifests
// fetched", "kustomization present but images: is empty" and "everything
// resolved cleanly" are indistinguishable, and the first two read as a clean
// pass.
type ImageResolutionStats struct {
	// ManifestsScanned counts the files fed to the gate.
	ManifestsScanned int `json:"manifests_scanned"`
	// KustomizationFound distinguishes "no kustomization in this directory"
	// from "kustomization present with no images: block".
	KustomizationFound bool   `json:"kustomization_found"`
	KustomizationFile  string `json:"kustomization_file,omitempty"`
	// KustomizeEntries counts the images[] entries available to match.
	KustomizeEntries int `json:"kustomize_entries"`
	// WorkloadImages counts the container images found in workload kinds.
	WorkloadImages int `json:"workload_images"`
	// ResolvedByKustomize counts how many of them an entry actually rewrote.
	ResolvedByKustomize int `json:"resolved_by_kustomize"`
}

// Summary renders the stats as one log/HTTP-friendly line.
func (s ImageResolutionStats) Summary() string {
	kustomization := "no kustomization found"
	if s.KustomizationFound {
		kustomization = fmt.Sprintf("%s with %d images[] entries",
			s.KustomizationFile, s.KustomizeEntries)
	}
	return fmt.Sprintf(
		"scanned %d manifest file(s); %s; %d workload image(s), %d resolved through a kustomization override",
		s.ManifestsScanned, kustomization, s.WorkloadImages, s.ResolvedByKustomize,
	)
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

// CollectWorkloadImages walks every workload manifest in the set and returns
// each container image as it would deploy, together with the resolution stats.
//
// A kustomization that cannot be interpreted is returned as an error — the
// caller must treat that as a gate failure, because an image we could not
// resolve is an image we cannot certify.
func CollectWorkloadImages(manifests []ManifestFile) ([]WorkloadImage, ImageResolutionStats, error) {
	stats := ImageResolutionStats{ManifestsScanned: len(manifests)}
	resolver, err := NewKustomizeImageResolver(manifests)
	if err != nil {
		return nil, stats, err
	}
	stats.KustomizationFound = resolver.Found()
	stats.KustomizationFile = resolver.File()
	stats.KustomizeEntries = resolver.EntryCount()

	var images []WorkloadImage
	for _, mf := range manifests {
		decoder := yaml.NewYAMLOrJSONDecoder(strings.NewReader(mf.Content), 4096)
		for {
			var obj map[string]interface{}
			if decodeErr := decoder.Decode(&obj); decodeErr != nil {
				if decodeErr == io.EOF {
					break
				}
				// Surface the parse error against the originating file so the
				// operator can find it. The caller treats a parse error as its
				// own blocker class rather than continuing on partial data.
				return images, stats, fmt.Errorf("parse %s: %w", mf.Path, decodeErr)
			}
			if obj == nil {
				continue
			}
			images = append(images, workloadImagesFromObject(mf.Path, obj, resolver)...)
		}
	}

	stats.WorkloadImages = len(images)
	for _, img := range images {
		if img.Source == ImageSourceKustomization {
			stats.ResolvedByKustomize++
		}
	}
	return images, stats, nil
}

// workloadImagesFromObject extracts the resolved container images of a single
// decoded manifest document. Non-workload kinds yield nothing.
func workloadImagesFromObject(
	file string,
	obj map[string]interface{},
	resolver *KustomizeImageResolver,
) []WorkloadImage {
	kind, _ := obj["kind"].(string)
	if !workloadKinds[kind] {
		return nil
	}
	name := "unknown"
	if md, ok := obj["metadata"].(map[string]interface{}); ok {
		if n, ok := md["name"].(string); ok {
			name = n
		}
	}
	var out []WorkloadImage
	for _, container := range iterContainers(kind, obj) {
		image, _ := container["image"].(string)
		if image == "" {
			continue
		}
		containerName, _ := container["name"].(string)
		resolved := resolver.Resolve(image)
		source := ImageSourceManifest
		if resolved.Overridden() {
			source = ImageSourceKustomization
		}
		out = append(out, WorkloadImage{
			File:              file,
			Kind:              kind,
			Name:              name,
			Container:         containerName,
			Image:             resolved.Image,
			ManifestImage:     resolved.Original,
			Source:            source,
			KustomizeEntry:    resolved.Entry,
			KustomizationFile: resolver.File(),
		})
	}
	return out
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
