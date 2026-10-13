package api

import (
	"path"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/checks"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	yamlv3 "gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// declaredTarget filters only explicitly listed flat resources. It does not
// turn deferred, unreferenced manifests into mutation candidates or apply them.
func declaredTarget(files []checks.ManifestFile, namespace, name string) (*appsv1.Deployment, error) {
	byName := map[string]checks.ManifestFile{}
	var kustomization *checks.ManifestFile
	for _, file := range files {
		if path.Base(file.Path) != file.Path || byName[file.Path].Path != "" {
			return nil, workloadtargets.ErrOwnership
		}
		byName[file.Path] = file
		if checks.IsKustomizationFile(file.Path) {
			if kustomization != nil {
				return nil, workloadtargets.ErrOwnership
			}
			copy := file
			kustomization = &copy
		}
	}
	if kustomization == nil {
		return nil, workloadtargets.ErrOwnership
	}
	var doc map[string]interface{}
	if yamlv3.Unmarshal([]byte(kustomization.Content), &doc) != nil {
		return nil, workloadtargets.ErrOwnership
	}
	resources, ok := doc["resources"].([]interface{})
	if !ok || len(resources) == 0 {
		return nil, workloadtargets.ErrOwnership
	}
	selected := []checks.ManifestFile{*kustomization}
	seen := map[string]bool{}
	for _, item := range resources {
		filename, ok := item.(string)
		file, exists := byName[filename]
		if !ok || !exists || path.Base(filename) != filename || seen[filename] || checks.IsKustomizationFile(filename) {
			return nil, workloadtargets.ErrOwnership
		}
		seen[filename] = true
		selected = append(selected, file)
	}
	objects, err := resolveAdmissionObjects(selected, namespace)
	if err != nil {
		return nil, workloadtargets.ErrOwnership
	}
	var target *appsv1.Deployment
	for _, item := range objects {
		obj := item.Object
		metadata, _ := obj["metadata"].(map[string]interface{})
		if obj["kind"] != "Deployment" || metadata["name"] != name {
			continue
		}
		if obj["apiVersion"] != "apps/v1" || metadata["namespace"] != namespace || target != nil {
			return nil, workloadtargets.ErrOwnership
		}
		candidate := &appsv1.Deployment{}
		if runtime.DefaultUnstructuredConverter.FromUnstructured(obj, candidate) != nil {
			return nil, workloadtargets.ErrOwnership
		}
		target = candidate
	}
	if target == nil {
		return nil, workloadtargets.ErrOwnership
	}
	return target, nil
}
