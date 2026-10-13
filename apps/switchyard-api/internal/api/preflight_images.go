package api

import (
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/checks"
	yamlv3 "gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/util/yaml"
)

type admissionObject struct {
	File   string
	Object map[string]interface{}
}

// Admission supports a flat directory and its images transformer. It refuses
// overlays/patches/generators it cannot render rather than certifying raw YAML.
func resolveAdmissionObjects(manifests []checks.ManifestFile, namespace string) ([]admissionObject, error) {
	if len(manifests) == 0 {
		return nil, fmt.Errorf("preflight resolved no manifest files")
	}
	resolver, err := checks.NewKustomizeImageResolver(manifests)
	if err != nil {
		return nil, err
	}
	if err = validateAdmissionKustomization(manifests, namespace); err != nil {
		return nil, err
	}
	var objects []admissionObject
	for _, file := range manifests {
		if checks.IsKustomizationFile(file.Path) {
			continue
		}
		decoder := yaml.NewYAMLOrJSONDecoder(strings.NewReader(file.Content), 4096)
		for {
			var obj map[string]interface{}
			if err := decoder.Decode(&obj); err != nil {
				if err == io.EOF {
					break
				}
				return nil, fmt.Errorf("parse %s: %w", file.Path, err)
			}
			if obj == nil {
				continue
			}
			checks.ResolveObjectImages(obj, resolver)
			objects = append(objects, admissionObject{File: file.Path, Object: obj})
		}
	}
	if len(objects) == 0 {
		return nil, fmt.Errorf("preflight resolved no admission objects")
	}
	return objects, nil
}

func validateAdmissionKustomization(manifests []checks.ManifestFile, namespace string) error {
	files := make(map[string]bool)
	for _, f := range manifests {
		if path.Base(f.Path) != f.Path {
			return fmt.Errorf("preflight requires one manifest directory")
		}
		files[f.Path] = true
	}
	for _, f := range manifests {
		if !checks.IsKustomizationFile(f.Path) {
			continue
		}
		var doc map[string]interface{}
		if err := yamlv3.Unmarshal([]byte(f.Content), &doc); err != nil {
			return fmt.Errorf("parse kustomization %s: %w", f.Path, err)
		}
		apiVersion, _ := doc["apiVersion"].(string)
		if doc["kind"] != "Kustomization" || !strings.HasPrefix(apiVersion, "kustomize.config.k8s.io/") {
			return fmt.Errorf("invalid kustomization identity in %s", f.Path)
		}
		for key := range doc {
			switch key {
			case "apiVersion", "kind", "resources", "images", "namespace":
			default:
				return fmt.Errorf("kustomization %s uses unsupported transform %s", f.Path, key)
			}
		}
		if ns, ok := doc["namespace"]; ok && ns != namespace {
			return fmt.Errorf("kustomization namespace differs from the requested project namespace")
		}
		resources, ok := doc["resources"].([]interface{})
		if !ok || len(resources) == 0 {
			return fmt.Errorf("kustomization %s requires flat local resources", f.Path)
		}
		seen := make(map[string]bool)
		for _, r := range resources {
			name, ok := r.(string)
			if !ok || path.Base(name) != name || name == "." || name == ".." || !files[name] || checks.IsKustomizationFile(name) || seen[name] {
				return fmt.Errorf("kustomization %s has an unavailable, duplicate or nonlocal resource", f.Path)
			}
			seen[name] = true
		}
		for _, other := range manifests {
			if !checks.IsKustomizationFile(other.Path) && !seen[other.Path] {
				return fmt.Errorf("kustomization %s leaves an unreferenced manifest %s", f.Path, other.Path)
			}
		}
	}
	return nil
}
