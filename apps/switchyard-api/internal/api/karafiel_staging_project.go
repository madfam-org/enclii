package api

import (
	"context"
	"errors"
	"reflect"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var karafielStageProjectGVR = schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "appprojects"}

// This is the closed platform/application boundary, not a caller ACL. It must
// match the separately reviewed infrastructure AppProject before any sync.
func validateKarafielStageProject(project *unstructured.Unstructured) error {
	fail := errors.New("release_stage_project_refused")
	if project == nil || project.GetName() != "karafiel-staging" || project.GetNamespace() != "argocd" ||
		project.GetUID() == "" || project.GetResourceVersion() == "" || project.GetDeletionTimestamp() != nil {
		return fail
	}
	spec, found, err := unstructured.NestedMap(project.Object, "spec")
	if err != nil || !found {
		return fail
	}
	for key := range spec {
		switch key {
		case "description", "sourceRepos", "destinations", "clusterResourceWhitelist", "clusterResourceBlacklist", "namespaceResourceWhitelist":
		default:
			return fail
		}
	}
	if !reflect.DeepEqual(spec["sourceRepos"], []any{"https://github.com/madfam-org/karafiel.git"}) ||
		!reflect.DeepEqual(spec["destinations"], []any{map[string]any{"server": "https://kubernetes.default.svc", "namespace": "enclii-karafiel-staging"}}) ||
		!reflect.DeepEqual(spec["clusterResourceBlacklist"], []any{map[string]any{"group": "*", "kind": "*"}}) {
		return fail
	}
	if whitelist, ok := spec["clusterResourceWhitelist"].([]any); !ok || len(whitelist) != 0 {
		return fail
	}
	expected := map[string]bool{"apps/Deployment": true, "/Service": true, "/ConfigMap": true, "/LimitRange": true, "/ResourceQuota": true,
		"networking.k8s.io/NetworkPolicy": true, "external-secrets.io/ExternalSecret": true, "autoscaling/HorizontalPodAutoscaler": true, // pragma: allowlist secret -- public Kubernetes API kind names; not credentials
		"policy/PodDisruptionBudget": true, "monitoring.coreos.com/ServiceMonitor": true}
	list, ok := spec["namespaceResourceWhitelist"].([]any)
	if !ok || len(list) != len(expected) {
		return fail
	}
	for _, raw := range list {
		item, ok := raw.(map[string]any)
		group, groupOK := item["group"].(string)
		kind, kindOK := item["kind"].(string)
		key := group + "/" + kind
		if !ok || !groupOK || !kindOK || len(item) != 2 || !expected[key] {
			return fail
		}
		delete(expected, key)
	}
	return nil
}

func (h *Handler) readKarafielStageProject(ctx context.Context) (*unstructured.Unstructured, error) {
	fail := errors.New("release_stage_project_refused")
	if h.k8sClient == nil || h.k8sClient.DynamicClient == nil || h.k8sClient.Kube() == nil {
		return nil, fail
	}
	project, err := h.k8sClient.DynamicClient.Resource(karafielStageProjectGVR).Namespace("argocd").Get(ctx, "karafiel-staging", metav1.GetOptions{})
	if err != nil || validateKarafielStageProject(project) != nil {
		return nil, fail
	}
	config, err := h.k8sClient.Kube().CoreV1().ConfigMaps("argocd").Get(ctx, "argocd-cm", metav1.GetOptions{})
	if err != nil || config.UID == "" || config.ResourceVersion == "" {
		return nil, fail
	}
	// A global project can silently add permissions to the local allowlist.
	if inherited := strings.TrimSpace(config.Data["globalProjects"]); inherited != "" && inherited != "[]" {
		return nil, fail
	}
	for _, owner := range project.GetOwnerReferences() {
		if owner.Controller != nil && *owner.Controller {
			return nil, fail
		}
	}
	switch config.Data["application.resourceTrackingMethod"] {
	case "annotation", "annotation+label":
		if project.GetAnnotations()["argocd.argoproj.io/tracking-id"] != "enclii-infrastructure:argoproj.io/AppProject:argocd/karafiel-staging" ||
			project.GetAnnotations()["argocd.argoproj.io/installation-id"] != config.Data["installationID"] {
			return nil, fail
		}
	case "label":
		key := config.Data["application.instanceLabelKey"]
		if key == "" {
			key = "app.kubernetes.io/instance"
		}
		if project.GetLabels()[key] != "enclii-infrastructure" {
			return nil, fail
		}
	default:
		return nil, fail
	}
	return project, nil
}
