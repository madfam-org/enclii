package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func releaseRouteProject() *unstructured.Unstructured {
	list := []any{}
	for _, pair := range [][2]string{{"apps", "Deployment"}, {"", "Service"}, {"", "ConfigMap"}, {"", "LimitRange"}, {"", "ResourceQuota"},
		{"networking.k8s.io", "NetworkPolicy"}, {"external-secrets.io", "ExternalSecret"}, {"autoscaling", "HorizontalPodAutoscaler"},
		{"policy", "PodDisruptionBudget"}, {"monitoring.coreos.com", "ServiceMonitor"}} {
		list = append(list, map[string]any{"group": pair[0], "kind": pair[1]})
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1", "kind": "AppProject",
		"metadata": map[string]any{"name": "karafiel-staging", "namespace": "argocd", "uid": "fixture-project-uid", "resourceVersion": "10",
			"annotations": map[string]any{"argocd.argoproj.io/tracking-id": "enclii-infrastructure:argoproj.io/AppProject:argocd/karafiel-staging"}},
		"spec": map[string]any{"sourceRepos": []any{"https://github.com/madfam-org/karafiel.git"},
			"destinations":             []any{map[string]any{"server": "https://kubernetes.default.svc", "namespace": "enclii-karafiel-staging"}},
			"clusterResourceWhitelist": []any{}, "clusterResourceBlacklist": []any{map[string]any{"group": "*", "kind": "*"}},
			"namespaceResourceWhitelist": list},
	}}
}

func TestKarafielStageProjectRejectsBroadenedAuthority(t *testing.T) {
	for _, tc := range []struct {
		name  string
		path  string
		value any
	}{
		{"foreign-repository", "sourceRepos", []any{"*"}},
		{"foreign-namespace", "destinations", []any{map[string]any{"server": "https://kubernetes.default.svc", "namespace": "karafiel"}}},
		{"cluster-writes", "clusterResourceWhitelist", []any{map[string]any{"group": "*", "kind": "*"}}},
		{"no-cluster-deny", "clusterResourceBlacklist", []any{}},
		{"namespace-wildcard", "namespaceResourceWhitelist", []any{map[string]any{"group": "*", "kind": "*"}}},
		{"project-admin", "roles", []any{map[string]any{"name": "admin"}}},
		{"application-source-namespaces", "sourceNamespaces", []any{"*"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			project := releaseRouteProject()
			require.NoError(t, unstructured.SetNestedField(project.Object, tc.value, "spec", tc.path))
			require.Error(t, validateKarafielStageProject(project))
		})
	}
	require.NoError(t, validateKarafielStageProject(releaseRouteProject()))
	project := releaseRouteProject()
	list, _, _ := unstructured.NestedSlice(project.Object, "spec", "namespaceResourceWhitelist")
	list[0] = list[1]
	require.NoError(t, unstructured.SetNestedSlice(project.Object, list, "spec", "namespaceResourceWhitelist"))
	require.Error(t, validateKarafielStageProject(project), "duplicate entries must not substitute for the exact allowlist")
}

func TestKarafielStageProjectRequiresActualRootTrackingAndNoInheritedPermissions(t *testing.T) {
	for _, mode := range []string{"valid", "untracked", "unknown-tracking", "global-inheritance"} {
		t.Run(mode, func(t *testing.T) {
			h, _, _, dyn, kube := releaseRouteFixture(t)
			if mode == "untracked" {
				project, err := dyn.Resource(karafielStageProjectGVR).Namespace("argocd").Get(context.Background(), "karafiel-staging", metav1.GetOptions{})
				require.NoError(t, err)
				project.SetAnnotations(nil)
				_, err = dyn.Resource(karafielStageProjectGVR).Namespace("argocd").Update(context.Background(), project, metav1.UpdateOptions{})
				require.NoError(t, err)
			}
			if mode == "unknown-tracking" || mode == "global-inheritance" {
				config, err := kube.CoreV1().ConfigMaps("argocd").Get(context.Background(), "argocd-cm", metav1.GetOptions{})
				require.NoError(t, err)
				if mode == "unknown-tracking" {
					delete(config.Data, "application.resourceTrackingMethod")
				} else {
					config.Data["globalProjects"] = "- projectName: broad"
				}
				_, err = kube.CoreV1().ConfigMaps("argocd").Update(context.Background(), config, metav1.UpdateOptions{})
				require.NoError(t, err)
			}
			_, err := h.readKarafielStageProject(context.Background())
			if mode == "valid" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
