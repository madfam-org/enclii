package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/releaseidentity"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stest "k8s.io/client-go/testing"
)

type releaseRouteAuth struct {
	principal releaseidentity.Principal
	deny      bool
	actions   []releaseidentity.Action
}

func (a *releaseRouteAuth) Authorize(_ context.Context, _ string, action releaseidentity.Action) (releaseidentity.Principal, error) {
	a.actions = append(a.actions, action)
	if a.deny {
		return releaseidentity.Principal{}, errors.New("private identity details")
	}
	return a.principal, nil
}
func releaseRouteApp() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1", "kind": "Application", "metadata": map[string]any{"name": "karafiel-staging", "namespace": "argocd", "uid": "fixture-owned-uid", "resourceVersion": "7"},
		"spec": map[string]any{"project": "karafiel-staging", "source": map[string]any{"repoURL": "https://github.com/madfam-org/karafiel.git", "path": "infra/k8s/overlays/staging", "targetRevision": "release/karafiel-staging"},
			"destination": map[string]any{"namespace": "enclii-karafiel-staging", "server": "https://kubernetes.default.svc"}, "syncPolicy": map[string]any{"automated": map[string]any{"enabled": false}}},
	}}
}
func releaseRouteFixture(t *testing.T) (*Handler, *gin.Engine, *releaseRouteAuth, *dynamicfake.FakeDynamicClient, *kubefake.Clientset) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dyn := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), releaseRouteApp(), releaseRouteProject())
	kube := kubefake.NewClientset(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "argocd-cm", Namespace: "argocd", UID: types.UID("fixture-config-uid"), ResourceVersion: "3"}, Data: map[string]string{"application.resourceTrackingMethod": "annotation"}}, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "enclii-karafiel-staging", UID: types.UID("fixture-stage-namespace"), ResourceVersion: "9", Annotations: map[string]string{"argocd.argoproj.io/sync-options": "Prune=false", "private.example/data": "private-namespace-marker"}}})
	a := &releaseRouteAuth{principal: releaseidentity.Principal{SourceSHA: strings.Repeat("a", 40), Workflow: "release-candidate.yml", RunID: 303, RepositoryID: 101}}
	h := &Handler{k8sClient: &k8s.Client{DynamicClient: dyn, KubeClient: kube}, karafielReleaseAuth: a, karafielReleaseRefRead: func(context.Context) (string, error) { return strings.Repeat("b", 40), nil }}
	h.karafielReleaseAdoptionRead = func(context.Context) (*karafielStageAdoptionObservation, error) {
		return &karafielStageAdoptionObservation{ApplicationUID: "fixture-owned-uid", NamespaceUID: "fixture-stage-namespace", NamespaceRV: "9", PruneProtected: true, TrackingDetached: true, ExceptionAbsent: true}, nil
	}
	router := gin.New()
	registerKarafielReleaseRoutes(router.Group("/v1"), h)
	return h, router, a, dyn, kube
}
func releaseHTTP(router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}
func syncBody() string {
	return `{"expected_uid":"fixture-owned-uid","expected_resource_version":"7","expected_revision":"` + strings.Repeat("b", 40) + `","idempotency_key":"fixture-stage-prepare","reason":"reviewed synthetic stage prepare"}`
}

func TestKarafielReleaseRoutesNeverGrantPlatformRole(t *testing.T) {
	h, _, a, _, _ := releaseRouteFixture(t)
	router := gin.New()
	router.GET("/proof", h.karafielReleaseIdentity(releaseidentity.Observe), func(c *gin.Context) {
		for _, key := range []string{"user_id", "role", "user_role", "is_platform_admin"} {
			_, exists := c.Get(key)
			require.False(t, exists)
		}
		source, exists := expectedKarafielReleaseSource(c.Request.Context())
		require.True(t, exists)
		require.Equal(t, a.principal.SourceSHA, source)
		c.Status(204)
	})
	require.Equal(t, 204, releaseHTTP(router, "GET", "/proof", "").Code)
}
func TestKarafielReleaseAnonymousAndInvalidActionsDoNotTouchCluster(t *testing.T) {
	_, router, a, dyn, kube := releaseRouteFixture(t)
	a.deny = true
	for _, r := range []struct{ method, path, body string }{
		{"POST", "/v1/releases/karafiel/observe", `{"environment":"staging"}`},
		{"POST", "/v1/releases/karafiel/staging/sync", syncBody()},
		{"GET", "/v1/releases/karafiel/staging/prerequisites/not-an-id", ""},
		{"GET", "/v1/releases/karafiel/staging/quarantine/not-an-id", ""},
		{"GET", "/v1/releases/karafiel/staging/database/no/no", ""},
		{"POST", "/v1/releases/karafiel/staging/migration/no/no", `{}`},
		{"GET", "/v1/releases/karafiel/staging/migration/no/no/job", ""},
	} {
		w := releaseHTTP(router, r.method, r.path, r.body)
		require.Equal(t, 401, w.Code)
		require.NotContains(t, w.Body.String(), "private identity")
	}
	require.Empty(t, dyn.Actions())
	require.Empty(t, kube.Actions())
	require.Equal(t, 404, releaseHTTP(router, "POST", "/v1/releases/karafiel/production/sync", syncBody()).Code)
}

func TestKarafielStageObservationExposesActualLegacyHandoffState(t *testing.T) {
	for _, mode := range []string{"detached", "namespace-tracked", "exception-present"} {
		t.Run(mode, func(t *testing.T) {
			_, router, _, dyn, kube := releaseRouteFixture(t)
			if mode == "namespace-tracked" {
				ns, err := kube.CoreV1().Namespaces().Get(context.Background(), "enclii-karafiel-staging", metav1.GetOptions{})
				require.NoError(t, err)
				ns.Labels = map[string]string{"app.kubernetes.io/instance": "karafiel-staging"}
				_, err = kube.CoreV1().Namespaces().Update(context.Background(), ns, metav1.UpdateOptions{})
				require.NoError(t, err)
			}
			if mode == "exception-present" {
				_, err := dyn.Resource(schema.GroupVersionResource{Group: "kyverno.io", Version: "v2beta1", Resource: "policyexceptions"}).Namespace("enclii-karafiel-staging").Create(context.Background(), &unstructured.Unstructured{Object: map[string]any{
					"apiVersion": "kyverno.io/v2beta1", "kind": "PolicyException", "metadata": map[string]any{"name": "karafiel-staging-gitops-rollout-signature", "namespace": "enclii-karafiel-staging"}}}, metav1.CreateOptions{})
				require.NoError(t, err)
			}
			w := releaseHTTP(router, "POST", "/v1/releases/karafiel/observe", `{"environment":"staging"}`)
			require.Equal(t, 200, w.Code)
			var result struct {
				Namespace struct {
					Detached bool `json:"argo_tracking_detached"`
					Absent   bool `json:"legacy_policy_exception_absent"`
				} `json:"namespace"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
			require.Equal(t, mode != "namespace-tracked", result.Namespace.Detached)
			require.Equal(t, mode != "exception-present", result.Namespace.Absent)
		})
	}
}
func TestKarafielReleaseRequestsAreClosedBeforeAnyIO(t *testing.T) {
	h, router, _, dyn, kube := releaseRouteFixture(t)
	h.karafielReleaseRefRead = func(context.Context) (string, error) { t.Fatal("unexpected ref request"); return "", nil }
	for _, r := range []struct{ path, body string }{
		{"observe", `{"environment":"staging","namespace":"other"}`}, {"observe", `{"environment":"other"}`},
		{"observe", `{"environment":"staging"} {}`}, {"observe", `{"environment":"production","environment":"staging"}`}, {"staging/sync", strings.TrimSuffix(syncBody(), "}") + `,"image":"arbitrary"}`},
		{"staging/sync", `{}`},
	} {
		require.Equal(t, 400, releaseHTTP(router, "POST", "/v1/releases/karafiel/"+r.path, r.body).Code)
	}
	require.Empty(t, dyn.Actions())
	require.Empty(t, kube.Actions())
}
func TestKarafielReleaseObservationDropsNodeMessagesAndPrivateSnapshots(t *testing.T) {
	_, router, _, dyn, kube := releaseRouteFixture(t)
	app, err := dyn.Resource(argoApplicationGVR).Namespace("argocd").Get(context.Background(), "karafiel-staging", metav1.GetOptions{})
	require.NoError(t, err)
	app.Object["status"] = map[string]any{"message": "private-status", "sync": map[string]any{"status": "Synced", "revision": strings.Repeat("b", 40), "comparedTo": map[string]any{"source": map[string]any{"repoURL": "https://github.com/madfam-org/karafiel.git", "path": "infra/k8s/overlays/staging", "targetRevision": "release/karafiel-staging"}, "ignoreDifferences": []any{"private-jq"}}}, "health": map[string]any{"status": "Healthy", "message": "private-health"}, "history": []any{map[string]any{"source": map[string]any{"helm": map[string]any{"values": "private-helm"}}}}}
	_, err = dyn.Resource(argoApplicationGVR).Namespace("argocd").Update(context.Background(), app, metav1.UpdateOptions{})
	require.NoError(t, err)
	stamp := metav1.NewTime(time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC))
	_, err = kube.CoreV1().Pods("enclii-karafiel-staging").Create(context.Background(), &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "karafiel-api-fixture", Namespace: "enclii-karafiel-staging"}, Spec: corev1.PodSpec{NodeName: "private-node"}, Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue, Message: "private-condition", LastTransitionTime: stamp}}, ContainerStatuses: []corev1.ContainerStatus{{Name: "api", Image: "sha256:fixture", ImageID: "fixture-image", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: stamp}}}}}}, metav1.CreateOptions{})
	require.NoError(t, err)
	w := releaseHTTP(router, "POST", "/v1/releases/karafiel/observe", `{"environment":"staging"}`)
	require.Equal(t, 200, w.Code)
	for _, private := range []string{"private-status", "private-helm", "private-jq", "private-health", "private-node", "private-condition", "private-namespace-marker"} {
		require.NotContains(t, w.Body.String(), private)
	}
	require.Contains(t, w.Body.String(), "fixture-image")
	require.Contains(t, w.Body.String(), "fixture-owned-uid")
	require.Contains(t, w.Body.String(), "startedAt")
}
func TestKarafielStageSyncUsesExactRefUIDVersionAndNoProductionMutation(t *testing.T) {
	_, router, _, dyn, _ := releaseRouteFixture(t)
	w := releaseHTTP(router, "POST", "/v1/releases/karafiel/staging/sync", syncBody())
	require.Equal(t, 202, w.Code, w.Body.String())
	app, err := dyn.Resource(argoApplicationGVR).Namespace("argocd").Get(context.Background(), "karafiel-staging", metav1.GetOptions{})
	require.NoError(t, err)
	revision, _, _ := unstructured.NestedString(app.Object, "operation", "sync", "revision")
	require.Equal(t, strings.Repeat("b", 40), revision)
	require.Equal(t, "303", app.GetAnnotations()["enclii.dev/karafiel-release-run"])
	updates := 0
	for _, action := range dyn.Actions() {
		if action.GetVerb() == "update" {
			updates++
			require.Equal(t, "argocd", action.GetNamespace())
			require.Equal(t, "karafiel-staging", action.(k8stest.UpdateAction).GetObject().(metav1.Object).GetName())
		}
	}
	require.Equal(t, 1, updates)
	dyn.ClearActions()
	w = releaseHTTP(router, "POST", "/v1/releases/karafiel/staging/sync", syncBody())
	require.Equal(t, 202, w.Code)
	require.Contains(t, w.Body.String(), `"idempotent":true`)
	for _, action := range dyn.Actions() {
		require.NotEqual(t, "update", action.GetVerb())
	}
}
func TestKarafielStageSyncRejectsConcurrentResourceChange(t *testing.T) {
	_, router, _, dyn, _ := releaseRouteFixture(t)
	gets := 0
	dyn.PrependReactor("get", "applications", func(action k8stest.Action) (bool, runtime.Object, error) {
		gets++
		if gets == 2 {
			app := releaseRouteApp()
			app.SetResourceVersion("8")
			return true, app, nil
		}
		return false, nil, nil
	})
	w := releaseHTTP(router, "POST", "/v1/releases/karafiel/staging/sync", syncBody())
	require.Equal(t, 409, w.Code)
	for _, action := range dyn.Actions() {
		require.NotEqual(t, "update", action.GetVerb())
	}
}
func TestKarafielStageSyncSourceBoundary(t *testing.T) {
	request := karafielSyncRequest{UID: "fixture-owned-uid", Version: "7"}
	for _, tc := range []struct {
		name   string
		mutate func(*unstructured.Unstructured)
	}{
		{"uid", func(a *unstructured.Unstructured) { a.SetUID(types.UID("foreign")) }},
		{"version", func(a *unstructured.Unstructured) { a.SetResourceVersion("8") }},
		{"main", func(a *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(a.Object, "main", "spec", "source", "targetRevision")
		}},
		{"foreign-project", func(a *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(a.Object, "default", "spec", "project")
		}},
		{"foreign-repo", func(a *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(a.Object, "https://example.test/repo", "spec", "source", "repoURL")
		}},
		{"plugin", func(a *unstructured.Unstructured) {
			_ = unstructured.SetNestedMap(a.Object, map[string]any{"name": "private"}, "spec", "source", "plugin")
		}},
		{"multiple-sources", func(a *unstructured.Unstructured) {
			_ = unstructured.SetNestedSlice(a.Object, []any{}, "spec", "sources")
		}},
		{"production", func(a *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(a.Object, "karafiel", "spec", "destination", "namespace")
		}},
		{"foreign-cluster", func(a *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(a.Object, "https://example.test", "spec", "destination", "server")
		}},
		{"auto-enabled", func(a *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(a.Object, true, "spec", "syncPolicy", "automated", "enabled")
		}},
		{"auto-unknown", func(a *unstructured.Unstructured) {
			unstructured.RemoveNestedField(a.Object, "spec", "syncPolicy", "automated", "enabled")
		}},
		{"replica-ignored", func(a *unstructured.Unstructured) {
			_ = unstructured.SetNestedSlice(a.Object, []any{map[string]any{"group": "apps", "kind": "Deployment", "jsonPointers": []any{"/spec/replicas"}}}, "spec", "ignoreDifferences")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := releaseRouteApp()
			tc.mutate(app)
			require.Error(t, validateKarafielSyncApplication(app, request))
		})
	}
	require.NoError(t, validateKarafielSyncApplication(releaseRouteApp(), request))
}

func TestKarafielStageObservationProvesActualNamespacePruneProtection(t *testing.T) {
	for _, option := range []string{"Prune=false", "ServerSideApply=true, Prune=false", "Prune=true", "", "Prune=false,Prune=true", "Prune=true,Prune=false", "Prune=false,Prune=false"} {
		t.Run(option, func(t *testing.T) {
			_, router, _, _, kube := releaseRouteFixture(t)
			ns, err := kube.CoreV1().Namespaces().Get(context.Background(), "enclii-karafiel-staging", metav1.GetOptions{})
			require.NoError(t, err)
			ns.Annotations["argocd.argoproj.io/sync-options"] = option
			_, err = kube.CoreV1().Namespaces().Update(context.Background(), ns, metav1.UpdateOptions{})
			require.NoError(t, err)
			w := releaseHTTP(router, "POST", "/v1/releases/karafiel/observe", `{"environment":"staging"}`)
			require.Equal(t, 200, w.Code)
			var result struct {
				Namespace struct {
					Name      string `json:"name"`
					UID       string `json:"uid"`
					Version   string `json:"resource_version"`
					Protected bool   `json:"prune_protected"`
				} `json:"namespace"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
			require.Equal(t, "fixture-stage-namespace", result.Namespace.UID)
			require.Equal(t, "9", result.Namespace.Version)
			require.Equal(t, strings.Contains(option, "Prune=false") && strings.Count(option, "Prune=") == 1, result.Namespace.Protected)
			require.NotContains(t, w.Body.String(), "private-namespace-marker")
		})
	}
}

func TestKarafielReleaseObservationRejectsActualAmbiguousSources(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field string
		value any
	}{
		{"multiple", "sources", []any{map[string]any{"repoURL": "https://example.test/foreign"}}},
		{"present-null", "sources", nil},
		{"private-render-override", "source", map[string]any{"repoURL": "https://github.com/madfam-org/karafiel.git", "path": "infra/k8s/overlays/staging", "targetRevision": "release/karafiel-staging", "helm": map[string]any{"values": "private-source-marker"}}},
		{"destination-name", "destination", map[string]any{"namespace": "enclii-karafiel-staging", "server": "https://kubernetes.default.svc", "name": "other"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, router, _, dyn, kube := releaseRouteFixture(t)
			app, err := dyn.Resource(argoApplicationGVR).Namespace("argocd").Get(context.Background(), "karafiel-staging", metav1.GetOptions{})
			require.NoError(t, err)
			require.NoError(t, unstructured.SetNestedField(app.Object, tc.value, "spec", tc.field))
			_, err = dyn.Resource(argoApplicationGVR).Namespace("argocd").Update(context.Background(), app, metav1.UpdateOptions{})
			require.NoError(t, err)
			w := releaseHTTP(router, "POST", "/v1/releases/karafiel/observe", `{"environment":"staging"}`)
			require.Equal(t, 503, w.Code)
			require.NotContains(t, w.Body.String(), "private-source-marker")
			require.Empty(t, kube.Actions())
		})
	}
	require.False(t, argoApplicationSourceConfigurationAmbiguousOrUnknown(releaseRouteApp().Object))
}

func TestKarafielSourceProjectionRejectsComparedMultiSourceAndRenderOverrides(t *testing.T) {
	for _, tc := range []struct {
		field string
		value any
	}{
		{"sources", []any{}}, {"sources", nil},
		{"source", map[string]any{"repoURL": "https://github.com/madfam-org/karafiel.git", "path": "infra/k8s/overlays/staging", "targetRevision": "release/karafiel-staging", "plugin": map[string]any{"name": "private-renderer"}}},
	} {
		app := releaseRouteApp()
		require.NoError(t, unstructured.SetNestedField(app.Object, tc.value, "status", "sync", "comparedTo", tc.field))
		require.True(t, argoApplicationSourceConfigurationAmbiguousOrUnknown(app.Object))
	}
}

func TestKarafielStageObservationRequiresFreshNativeRootAuthority(t *testing.T) {
	for _, mode := range []string{"unavailable", "recreated-application", "recreated-namespace", "namespace-changed"} {
		t.Run(mode, func(t *testing.T) {
			h, router, _, _, _ := releaseRouteFixture(t)
			h.karafielReleaseAdoptionRead = func(context.Context) (*karafielStageAdoptionObservation, error) {
				a := &karafielStageAdoptionObservation{ApplicationUID: "fixture-owned-uid", NamespaceUID: "fixture-stage-namespace", NamespaceRV: "9"}
				switch mode {
				case "unavailable":
					return nil, errors.New("private-root-source")
				case "recreated-application":
					a.ApplicationUID = "other"
				case "recreated-namespace":
					a.NamespaceUID = "other"
				case "namespace-changed":
					a.NamespaceRV = "10"
				}
				return a, nil
			}
			w := releaseHTTP(router, "POST", "/v1/releases/karafiel/observe", `{"environment":"staging"}`)
			require.Equal(t, 503, w.Code)
			require.NotContains(t, w.Body.String(), "private-root-source")
		})
	}
}

func TestKarafielStageSyncRequiresCompletedNativeHandoff(t *testing.T) {
	for _, mode := range []string{"unknown", "namespace-tracked", "prune-unprotected", "legacy-exception"} {
		t.Run(mode, func(t *testing.T) {
			h, router, _, dyn, kube := releaseRouteFixture(t)
			h.karafielReleaseRefRead = func(context.Context) (string, error) { t.Fatal("handoff must precede ref access"); return "", nil }
			h.karafielReleaseAdoptionRead = func(context.Context) (*karafielStageAdoptionObservation, error) {
				a := &karafielStageAdoptionObservation{ApplicationUID: "fixture-owned-uid", PruneProtected: true, TrackingDetached: true, ExceptionAbsent: true}
				switch mode {
				case "unknown":
					return nil, errors.New("private-adoption-details")
				case "namespace-tracked":
					a.TrackingDetached = false
				case "prune-unprotected":
					a.PruneProtected = false
				case "legacy-exception":
					a.ExceptionAbsent = false
				}
				return a, nil
			}
			w := releaseHTTP(router, "POST", "/v1/releases/karafiel/staging/sync", syncBody())
			require.Equal(t, 409, w.Code)
			require.NotContains(t, w.Body.String(), "private-adoption-details")
			require.Empty(t, dyn.Actions())
			require.Empty(t, kube.Actions())
		})
	}
}
