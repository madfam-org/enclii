package workloadtargets

import "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

// The production Application is generated, not a direct root child. Prove
// its exact native controller and fixed generator/template/config chain.
func validateKarafielProductionParent(s Snapshot) (string, error) {
	a, p := s.Application, s.SourceParent
	if a == nil || p == nil || p.GetKind() != "ApplicationSet" || p.GetAPIVersion() != "argoproj.io/v1alpha1" ||
		p.GetName() != "project-applications" || p.GetNamespace() != "argocd" || p.GetUID() == "" || p.GetResourceVersion() == "" || p.GetDeletionTimestamp() != nil {
		return "", ErrOwnership
	}
	controllers := 0
	for _, o := range a.GetOwnerReferences() {
		if o.Controller != nil && *o.Controller {
			controllers++
			if o.APIVersion != "argoproj.io/v1alpha1" || o.Kind != "ApplicationSet" || o.Name != p.GetName() || o.UID != p.GetUID() {
				return "", ErrOwnership
			}
		}
	}
	if controllers != 1 || validateKarafielRootChild(s, p, "ApplicationSet", "project-applications") != nil {
		return "", ErrOwnership
	}
	template, found, err := unstructured.NestedBool(p.Object, "spec", "goTemplate")
	if err != nil || !found || !template {
		return "", ErrOwnership
	}
	options, found, err := unstructured.NestedStringSlice(p.Object, "spec", "goTemplateOptions")
	if err != nil || !found || len(options) != 1 || options[0] != "missingkey=zero" {
		return "", ErrOwnership
	}
	if _, found, err := unstructured.NestedFieldNoCopy(p.Object, "spec", "templatePatch"); err != nil || found {
		return "", ErrOwnership
	}
	gen, found, err := unstructured.NestedSlice(p.Object, "spec", "generators")
	if err != nil || !found || len(gen) != 1 {
		return "", ErrOwnership
	}
	g, ok := gen[0].(map[string]any)
	if !ok || len(g) != 1 {
		return "", ErrOwnership
	}
	git, ok := g["git"].(map[string]any)
	if !ok || len(git) != 3 || git["repoURL"] != "https://github.com/madfam-org/enclii.git" || git["revision"] != "main" {
		return "", ErrOwnership
	}
	files, ok := git["files"].([]any)
	if !ok || len(files) != 1 {
		return "", ErrOwnership
	}
	file, ok := files[0].(map[string]any)
	if !ok || len(file) != 1 || file["path"] != "infra/argocd/projects/*/config.json" {
		return "", ErrOwnership
	}
	for _, f := range []struct {
		path     []string
		expected string
	}{
		{[]string{"spec", "template", "metadata", "name"}, "{{ .name }}-services"},
		{[]string{"spec", "template", "spec", "project"}, "default"},
		{[]string{"spec", "template", "spec", "source", "repoURL"}, "{{ .repoURL }}"},
		{[]string{"spec", "template", "spec", "source", "targetRevision"}, "{{ .branch }}"},
		{[]string{"spec", "template", "spec", "source", "path"}, "{{ .manifestPath }}"},
		{[]string{"spec", "template", "spec", "destination", "server"}, "https://kubernetes.default.svc"},
		{[]string{"spec", "template", "spec", "destination", "namespace"}, "{{ .namespace }}"},
	} {
		v, found, e := unstructured.NestedString(p.Object, f.path...)
		if e != nil || !found || v != f.expected {
			return "", ErrOwnership
		}
	}
	for _, path := range [][]string{{"spec", "template", "spec", "sources"}} {
		if _, found, err := unstructured.NestedFieldNoCopy(p.Object, path...); err != nil || found {
			return "", ErrOwnership
		}
	}
	source, found, err := unstructured.NestedMap(p.Object, "spec", "template", "spec", "source")
	if err != nil || !found || len(source) != 3 {
		return "", ErrOwnership
	}
	dest, found, err := unstructured.NestedMap(p.Object, "spec", "template", "spec", "destination")
	if err != nil || !found || len(dest) != 2 {
		return "", ErrOwnership
	}
	expected := map[string]string{"name": "karafiel", "partOf": "karafiel", "repoURL": "https://github.com/madfam-org/karafiel.git", "manifestPath": "infra/k8s/production", "namespace": "karafiel", "branch": "main"}
	if len(s.SourceConfiguration) != len(expected) {
		return "", ErrOwnership
	}
	for k, v := range expected {
		if s.SourceConfiguration[k] != v {
			return "", ErrOwnership
		}
	}
	rootRevision, found, err := unstructured.NestedString(s.SourceRoot.Object, "status", "sync", "revision")
	if err != nil || !found || !sha40.MatchString(rootRevision) || s.SourceConfigurationRevision != rootRevision {
		return "", ErrOwnership
	}
	project, found, err := unstructured.NestedString(a.Object, "spec", "project")
	if err != nil || !found || project != "default" {
		return "", ErrOwnership
	}
	return hash(s.SourceConfiguration)
}
