package api

import (
	"context"
	"encoding/json"
	"testing"

	k8sclient "github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
)

var releaseReadbackApplicationGVR = schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"}

func releaseReadbackPrivateSource() map[string]any {
	return map[string]any{
		"repoURL": "https://example.org/fixture/repo.git", "path": "infra/fixture", "targetRevision": "reviewed-fixture-ref", "chart": "fixture-chart",
		"helm":             map[string]any{"values": "private-helm-marker", "parameters": []any{map[string]any{"name": "fixture", "value": "private-parameter-marker"}}},
		"kustomize":        map[string]any{"images": []any{"private-kustomize-marker"}},
		"plugin":           map[string]any{"env": []any{map[string]any{"name": "FIXTURE", "value": "private-plugin-marker"}}},
		"privateExtension": "private-source-marker",
	}
}

func releaseReadbackFixture() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1", "kind": "Application",
		"metadata": map[string]any{
			"name": "fixture-app", "namespace": "argocd", "uid": "fixture-app-uid", "resourceVersion": "42",
			"labels":          map[string]any{"app.kubernetes.io/part-of": "fixture-platform"},
			"ownerReferences": []any{map[string]any{"apiVersion": "argoproj.io/v1alpha1", "kind": "ApplicationSet", "name": "fixture-parent", "uid": "fixture-parent-uid", "controller": true, "blockOwnerDeletion": true}},
			"annotations":     map[string]any{"argocd.argoproj.io/tracking-id": "fixture-root:argoproj.io/Application:argocd/fixture-app", "kubectl.kubernetes.io/last-applied-configuration": "private-last-applied-marker", "fixture.example/secret": "private-annotation-marker"},
			"managedFields":   []any{map[string]any{"manager": "private-manager-marker"}},
		},
		"spec": map[string]any{
			"project": "private-project-marker", "source": releaseReadbackPrivateSource(),
			"sources":     []any{releaseReadbackPrivateSource()},
			"destination": map[string]any{"namespace": "fixture-stage", "server": "https://kubernetes.default.svc", "privateExtension": "private-destination-marker"},
			"syncPolicy": map[string]any{
				"automated":   map[string]any{"enabled": false, "prune": false, "selfHeal": false, "allowEmpty": false, "privateExtension": "private-automation-marker"},
				"syncOptions": []any{"CreateNamespace=true", "PruneLast=true", "ServerSideApply=false", "PrunePropagationPolicy=foreground", "PrivateOption=private-option-marker", "CreateNamespace=private-option-value-marker"},
				"retry":       map[string]any{"privateExtension": "private-retry-marker"},
			},
			"privateExtension": "private-spec-marker",
		},
		"status": map[string]any{
			"health": map[string]any{"status": "Healthy", "message": "fixture health"},
			"sync":   map[string]any{"status": "Synced", "revision": "observed-fixture-revision", "comparedTo": map[string]any{"source": releaseReadbackPrivateSource(), "sources": []any{releaseReadbackPrivateSource()}, "destination": map[string]any{"namespace": "fixture-stage"}}},
			"operationState": map[string]any{
				"phase": "Succeeded", "finishedAt": "2026-01-02T03:04:05Z",
				"operation":  map[string]any{"sync": map[string]any{"revision": "operation-fixture-revision", "source": releaseReadbackPrivateSource(), "sources": []any{releaseReadbackPrivateSource()}}},
				"syncResult": map[string]any{"revision": "result-fixture-revision", "source": releaseReadbackPrivateSource(), "sources": []any{releaseReadbackPrivateSource()}, "resources": []any{map[string]any{"kind": "Deployment", "name": "fixture-web", "status": "Synced"}}},
			},
			"history":    []any{map[string]any{"id": int64(7), "revision": "history-fixture-revision", "source": releaseReadbackPrivateSource(), "sources": []any{releaseReadbackPrivateSource()}}},
			"resources":  []any{map[string]any{"kind": "Deployment", "name": "fixture-web", "namespace": "fixture-stage", "status": "Synced"}},
			"conditions": []any{map[string]any{"type": "FixtureCondition", "message": "fixture condition"}},
		},
	}}
}

func TestApplicationReleaseReadbackGetAndListExposeOnlyProjectedMetadata(t *testing.T) {
	for _, target := range []string{"fixture-app", ""} {
		t.Run("target="+target, func(t *testing.T) {
			app := releaseReadbackFixture()
			original, err := json.Marshal(app.Object)
			require.NoError(t, err)
			client := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{releaseReadbackApplicationGVR: "ApplicationList"}, app)
			handler := &Handler{k8sClient: &k8sclient.Client{DynamicClient: client}}
			result, err := handler.readArgoApplications(context.Background(), operatorOperationRequest{Scope: map[string]string{"namespace": "argocd"}, Args: map[string]string{"target": target}})
			require.NoError(t, err)
			require.Equal(t, "argocd", result["namespace"])
			require.Equal(t, 1, result["count"])
			entries, ok := result["applications"].([]map[string]any)
			require.True(t, ok)
			require.Len(t, entries, 1)
			entry := entries[0]
			require.Equal(t, "fixture-app", entry["name"])
			require.Equal(t, "argocd", entry["namespace"])
			require.Equal(t, map[string]string{"app.kubernetes.io/part-of": "fixture-platform"}, entry["labels"])
			require.Equal(t, "fixture-app-uid", entry["uid"])
			require.Equal(t, "42", entry["resourceVersion"])
			require.Equal(t, app.GetOwnerReferences(), entry["ownerReferences"])
			require.Equal(t, map[string]string{"argocd.argoproj.io/tracking-id": "fixture-root:argoproj.io/Application:argocd/fixture-app"}, entry["annotations"])
			spec := entry["spec"].(map[string]any)
			require.Equal(t, map[string]any{
				"source":      map[string]any{"repoURL": "https://example.org/fixture/repo.git", "path": "infra/fixture", "targetRevision": "reviewed-fixture-ref", "chart": "fixture-chart"},
				"destination": map[string]any{"namespace": "fixture-stage", "server": "https://kubernetes.default.svc"},
				"syncPolicy":  map[string]any{"automated": map[string]any{"enabled": false, "prune": false, "selfHeal": false, "allowEmpty": false}, "syncOptions": []string{"CreateNamespace=true", "PruneLast=true", "ServerSideApply=false", "PrunePropagationPolicy=foreground"}},
			}, spec)
			encoded, err := json.Marshal(entry)
			require.NoError(t, err)
			for _, private := range []string{"private-", "managedFields", "last-applied-configuration", "helm", "parameters", "kustomize", "plugin", "privateExtension"} {
				require.NotContains(t, string(encoded), private)
			}
			status := entry["status"].(map[string]any)
			require.Equal(t, map[string]any{"status": "Healthy", "message": "fixture health"}, status["health"])
			require.Equal(t, app.Object["status"].(map[string]any)["resources"], status["resources"])
			require.Equal(t, app.Object["status"].(map[string]any)["conditions"], status["conditions"])
			revision, _, err := unstructured.NestedString(status, "sync", "revision")
			require.NoError(t, err)
			require.Equal(t, "observed-fixture-revision", revision)
			phase, _, err := unstructured.NestedString(status, "operationState", "phase")
			require.NoError(t, err)
			require.Equal(t, "Succeeded", phase)
			for _, path := range [][]string{{"sync", "comparedTo"}, {"operationState", "operation", "sync"}, {"operationState", "syncResult"}} {
				snapshot, found, err := unstructured.NestedMap(status, path...)
				require.NoError(t, err)
				require.True(t, found)
				require.Equal(t, spec["source"], snapshot["source"])
				require.Equal(t, []any{spec["source"]}, snapshot["sources"])
			}
			history := status["history"].([]any)[0].(map[string]any)
			require.Equal(t, "history-fixture-revision", history["revision"])
			require.Equal(t, int64(7), history["id"])
			require.Equal(t, spec["source"], history["source"])
			require.Equal(t, []any{spec["source"]}, history["sources"])
			resultRevision, _, _ := unstructured.NestedString(status, "operationState", "syncResult", "revision")
			require.Equal(t, "result-fixture-revision", resultRevision)
			actions := client.Actions()
			require.Len(t, actions, 1)
			verb := "get"
			if target == "" {
				verb = "list"
			}
			require.Equal(t, verb, actions[0].GetVerb(), "readback must never mutate Kubernetes")
			require.Equal(t, releaseReadbackApplicationGVR, actions[0].GetResource())
			stored, err := client.Tracker().Get(releaseReadbackApplicationGVR, "argocd", "fixture-app")
			require.NoError(t, err)
			after, err := json.Marshal(stored.(*unstructured.Unstructured).Object)
			require.NoError(t, err)
			require.JSONEq(t, string(original), string(after), "projection must not mutate the stored Application")
		})
	}
}

func TestApplicationReleaseReadbackPreservesAutomationPresence(t *testing.T) {
	for _, test := range []struct {
		name   string
		policy map[string]any
		want   map[string]any
	}{
		{name: "absent", policy: map[string]any{}, want: map[string]any{}},
		{name: "legacy-enabled", policy: map[string]any{"automated": map[string]any{}}, want: map[string]any{"automated": map[string]any{}}},
		{name: "explicit-enabled", policy: map[string]any{"automated": map[string]any{"enabled": true, "prune": true, "selfHeal": true, "allowEmpty": true}}, want: map[string]any{"automated": map[string]any{"enabled": true, "prune": true, "selfHeal": true, "allowEmpty": true}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := releaseReadbackFixture()
			require.NoError(t, unstructured.SetNestedMap(app.Object, test.policy, "spec", "syncPolicy"))
			entry := map[string]any{}
			addArgoApplicationReleaseReadback(entry, *app)
			require.Equal(t, test.want, entry["spec"].(map[string]any)["syncPolicy"])
		})
	}
}

func TestApplicationReleaseReadbackOmitsCredentialAndQueryURLs(t *testing.T) {
	for _, value := range []string{"https://fixture-user:private-url-marker@example.org/repo", "https://example.org/repo?token=private-url-marker", "https://example.org/repo?", "https://example.org/repo#private-url-marker", "https://example.org/repo#", "file:///private-url-marker", "not-a-url"} {
		t.Run(value, func(t *testing.T) {
			app := releaseReadbackFixture()
			require.NoError(t, unstructured.SetNestedField(app.Object, value, "spec", "source", "repoURL"))
			require.NoError(t, unstructured.SetNestedField(app.Object, value, "spec", "destination", "server"))
			require.NoError(t, unstructured.SetNestedField(app.Object, value, "status", "sync", "comparedTo", "source", "repoURL"))
			status, _, _ := unstructured.NestedMap(app.Object, "status")
			entry := map[string]any{"status": status}
			addArgoApplicationReleaseReadback(entry, *app)
			spec := entry["spec"].(map[string]any)
			require.NotContains(t, spec["source"], "repoURL")
			require.NotContains(t, spec["destination"], "server")
			projectedSource, found, err := unstructured.NestedMap(entry["status"].(map[string]any), "sync", "comparedTo", "source")
			require.NoError(t, err)
			require.True(t, found)
			require.NotContains(t, projectedSource, "repoURL")
		})
	}
}

func TestApplicationReleaseReadbackDropsMalformedSnapshotSources(t *testing.T) {
	for _, path := range [][]string{{"sync", "comparedTo"}, {"operationState", "operation", "sync"}, {"operationState", "syncResult"}} {
		app := releaseReadbackFixture()
		status, _, _ := unstructured.NestedMap(app.Object, "status")
		snapshot := map[string]any{"source": "private-malformed-marker", "sources": []any{"private-malformed-marker", releaseReadbackPrivateSource()}}
		require.NoError(t, unstructured.SetNestedMap(status, snapshot, path...))
		entry := map[string]any{"status": status}
		addArgoApplicationReleaseReadback(entry, *app)
		projected, found, err := unstructured.NestedMap(status, path...)
		require.NoError(t, err)
		require.True(t, found)
		require.NotContains(t, projected, "source")
		require.Equal(t, []any{argoApplicationPublicSource(releaseReadbackPrivateSource())}, projected["sources"])
	}
}

func TestApplicationReleaseReadbackLeavesOtherResourcesUnchanged(t *testing.T) {
	gvr := schema.GroupVersionResource{Group: "fixture.example.org", Version: "v1", Resource: "widgets"}
	obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "fixture.example.org/v1", "kind": "Widget", "metadata": map[string]any{"name": "fixture-widget", "namespace": "fixture-space", "uid": "unexposed-uid", "resourceVersion": "99", "labels": map[string]any{"fixture": "yes"}, "annotations": map[string]any{"fixture": "unexposed-annotation"}}, "spec": map[string]any{"fixture": "unexposed-spec"}, "status": map[string]any{"state": "ready", "source": "existing-wire-value"}}}
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), obj)
	handler := &Handler{k8sClient: &k8sclient.Client{DynamicClient: client}}
	entries, err := handler.readDynamicResources(context.Background(), "fixture-space", "fixture-widget", gvr)
	require.NoError(t, err)
	require.Equal(t, []map[string]any{{"name": "fixture-widget", "namespace": "fixture-space", "labels": map[string]string{"fixture": "yes"}, "status": map[string]any{"state": "ready", "source": "existing-wire-value"}}}, entries)
	require.Len(t, client.Actions(), 1)
	require.Equal(t, "get", client.Actions()[0].GetVerb())
}
