package api

import (
	"context"
	"encoding/json"
	k8sclient "github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	jsonpatch "gopkg.in/evanphx/json-patch.v4"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
	"net/http"
	"testing"
)

func syncAtomicFixture() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1", "kind": "Application",
		"metadata": map[string]any{"name": "app-a", "namespace": "argocd", "resourceVersion": "1", "annotations": map[string]any{"other-controller": "preserve"}},
	}}
}
func syncAtomicRequest() operatorOperationRequest {
	return operatorOperationRequest{Args: map[string]string{"target": "app-a", "revision": "reviewed-revision", "prune": "false"}, Reason: "reviewed migration rollout"}
}
func TestAppsSyncAtomicFullOperation(t *testing.T) {
	for _, initial := range []string{"absent", "null", "empty"} {
		t.Run(initial, func(t *testing.T) {
			app := syncAtomicFixture()
			if initial == "null" {
				app.Object["operation"] = nil
			}
			if initial == "empty" {
				app.Object["operation"] = map[string]any{}
			}
			client := fake.NewSimpleDynamicClient(runtime.NewScheme(), app)
			client.PrependReactor("patch", "applications", func(action k8stesting.Action) (bool, runtime.Object, error) {
				patch := action.(k8stesting.PatchAction)
				require.Equal(t, k8stypes.JSONPatchType, patch.GetPatchType())
				var ops []map[string]any
				require.NoError(t, json.Unmarshal(patch.GetPatch(), &ops))
				require.Equal(t, map[string]any{"op": "test", "path": "/metadata/resourceVersion", "value": "1"}, ops[0])
				return false, nil, nil
			})
			h := &Handler{k8sClient: &k8sclient.Client{DynamicClient: client}}
			resp, code := h.handleOpsAppsSyncApply(context.Background(), "ops.apps.sync", syncAtomicRequest())
			require.Equal(t, http.StatusAccepted, code)
			assert.Equal(t, "submitted", resp.Status)
			got, err := client.Resource(argoApplicationGVR).Namespace("argocd").Get(context.Background(), "app-a", metav1.GetOptions{})
			require.NoError(t, err)
			assert.Equal(t, map[string]any{"initiatedBy": map[string]any{"username": "enclii-ops"}, "sync": map[string]any{"prune": false, "revision": "reviewed-revision", "syncOptions": []any{"PruneLast=true"}}}, got.Object["operation"])
			assert.Equal(t, "preserve", got.GetAnnotations()["other-controller"])
			assert.Equal(t, "reviewed migration rollout", got.GetAnnotations()["enclii.dev/last-ops-reason"])
		})
	}
}
func TestAppsSyncAtomicRejectsConcurrentSelfHeal(t *testing.T) {
	app := syncAtomicFixture()
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), app)
	competing := app.DeepCopy()
	competing.SetResourceVersion("2")
	competing.Object["operation"] = map[string]any{
		"initiatedBy": map[string]any{"automated": true},
		"sync":        map[string]any{"autoHealAttemptsCount": int64(1), "resources": []any{map[string]any{"group": "apps", "kind": "Deployment", "name": "app-a"}}},
	}
	patchCalls := 0
	client.PrependReactor("patch", "applications", func(action k8stesting.Action) (bool, runtime.Object, error) {
		patchCalls++
		require.NoError(t, client.Tracker().Update(argoApplicationGVR, competing, "argocd"))
		patchAction := action.(k8stesting.PatchAction)
		require.Equal(t, k8stypes.JSONPatchType, patchAction.GetPatchType())
		patch, err := jsonpatch.DecodePatch(patchAction.GetPatch())
		require.NoError(t, err)
		current, err := json.Marshal(competing.Object)
		require.NoError(t, err)
		_, err = patch.Apply(current)
		require.Error(t, err, "real JSON Patch evaluator must reject the stale version before writes")
		// Kubernetes translates a failed test into Invalid; its fake omits HTTP translation.
		return true, nil, k8serrors.NewInvalid(schema.GroupKind{Group: "argoproj.io", Kind: "Application"}, "app-a", field.ErrorList{field.Invalid(field.NewPath("metadata", "resourceVersion"), "1", "test failed")})
	})
	h := &Handler{k8sClient: &k8sclient.Client{DynamicClient: client}}
	resp, code := h.handleOpsAppsSyncApply(context.Background(), "ops.apps.sync", syncAtomicRequest())
	require.Equal(t, http.StatusConflict, code)
	assert.Equal(t, "state_changed", resp.Status)
	assert.Equal(t, 1, patchCalls, "must not retry over a concurrent operation")
	got, err := client.Resource(argoApplicationGVR).Namespace("argocd").Get(context.Background(), "app-a", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, competing.Object, got.Object, "no inherited operation or audit writes")
}
func TestAppsSyncAtomicRefusesActiveOrUnversionedApplication(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing-version", true: "active-operation"}[active], func(t *testing.T) {
			app := syncAtomicFixture()
			if active {
				app.Object["operation"] = map[string]any{"initiatedBy": map[string]any{"automated": true}}
			} else {
				app.SetResourceVersion("")
			}
			client := fake.NewSimpleDynamicClient(runtime.NewScheme(), app)
			h := &Handler{k8sClient: &k8sclient.Client{DynamicClient: client}}
			resp, code := h.handleOpsAppsSyncApply(context.Background(), "ops.apps.sync", syncAtomicRequest())
			if active {
				assert.Equal(t, http.StatusConflict, code)
				assert.Equal(t, "already_running", resp.Status)
			} else {
				assert.Equal(t, http.StatusInternalServerError, code)
			}
			for _, action := range client.Actions() {
				assert.NotEqual(t, "patch", action.GetVerb())
				assert.NotEqual(t, "update", action.GetVerb())
			}
		})
	}
}
