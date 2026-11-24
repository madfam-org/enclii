package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	k8sclient "github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

func syncRaceFixture() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1", "kind": "Application",
		"metadata": map[string]any{"name": "fixture-app", "namespace": "argocd", "resourceVersion": "10", "annotations": map[string]any{"example.org/keep": "preserved"}},
		"spec":     map[string]any{"source": map[string]any{"targetRevision": "main"}},
		"status": map[string]any{
			"operationState": map[string]any{"phase": "Succeeded", "operation": selectiveAutoSync()},
			"sync":           map[string]any{"status": "Synced", "revision": "previous-revision"},
			"health":         map[string]any{"status": "Healthy"},
			"history":        []any{map[string]any{"id": int64(1), "revision": "previous-revision"}},
		},
	}}
}
func selectiveAutoSync() map[string]any {
	return map[string]any{"initiatedBy": map[string]any{"automated": true}, "sync": map[string]any{"revision": "old-revision", "resources": []any{map[string]any{"kind": "ConfigMap", "name": "fixture-config"}}}}
}
func syncRaceRequest() operatorOperationRequest {
	return operatorOperationRequest{Reason: "reconcile reviewed fixture", IdempotencyKey: "fixture-request", Args: map[string]string{"target": "fixture-app"}}
}

func TestAppsSyncConflictPreservesConcurrentOperation(t *testing.T) {
	app := syncRaceFixture()
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), app)
	handler := &Handler{k8sClient: &k8sclient.Client{DynamicClient: client}}
	var concurrent *unstructured.Unstructured
	updates := 0
	client.PrependReactor("update", "applications", func(action ktesting.Action) (bool, runtime.Object, error) {
		updates++
		requested := action.(ktesting.UpdateAction).GetObject().(*unstructured.Unstructured)
		require.Equal(t, "10", requested.GetResourceVersion())
		require.Empty(t, action.GetSubresource())
		// Model auto-heal winning the write after the handler's GET. Dynamicfake
		// does not enforce resourceVersion itself, so emulate the API's CAS here.
		concurrent = app.DeepCopy()
		concurrent.SetResourceVersion("11")
		concurrent.Object["operation"] = selectiveAutoSync()
		concurrent.SetAnnotations(map[string]string{"example.org/keep": "concurrently-updated"})
		require.NoError(t, client.Tracker().Update(argoApplicationGVR, concurrent, "argocd"))
		if requested.GetResourceVersion() != concurrent.GetResourceVersion() {
			return true, nil, k8serrors.NewConflict(argoApplicationGVR.GroupResource(), app.GetName(), fmt.Errorf("resource version changed"))
		}
		return false, nil, nil
	})
	response, status := handler.handleOpsAppsSyncApply(context.Background(), "ops.apps.sync", syncRaceRequest())
	require.Equal(t, http.StatusConflict, status)
	require.Equal(t, "conflict", response.Status)
	require.Equal(t, 1, updates, "must not retry and replace a concurrent operation")
	actual, err := client.Resource(argoApplicationGVR).Namespace("argocd").Get(context.Background(), app.GetName(), metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, concurrent.Object, actual.Object)
}

func TestAppsSyncReplacesWholeOperationAndPreservesApplication(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprint(explicit), func(t *testing.T) {
			app := syncRaceFixture()
			app.Object["operation"] = map[string]any{} // An empty operation is not active.
			client := fake.NewSimpleDynamicClient(runtime.NewScheme(), app)
			handler := &Handler{k8sClient: &k8sclient.Client{DynamicClient: client}}
			request := syncRaceRequest()
			syncSpec := map[string]any{"prune": true, "syncOptions": []any{"PruneLast=true"}}
			if explicit {
				request.Args["revision"] = "reviewed-revision"
				request.Args["prune"] = "false"
				request.Args["sync_options"] = "ApplyOutOfSyncOnly=true,PruneLast=true"
				syncSpec = map[string]any{"revision": "reviewed-revision", "prune": false, "syncOptions": []any{"ApplyOutOfSyncOnly=true", "PruneLast=true"}}
			}
			response, status := handler.handleOpsAppsSyncApply(context.Background(), "ops.apps.sync", request)
			require.Equal(t, http.StatusAccepted, status)
			require.Equal(t, "submitted", response.Status)
			actual, err := client.Resource(argoApplicationGVR).Namespace("argocd").Get(context.Background(), app.GetName(), metav1.GetOptions{})
			require.NoError(t, err)
			require.Equal(t, map[string]any{"initiatedBy": map[string]any{"username": "enclii-ops"}, "sync": syncSpec}, actual.Object["operation"])
			require.Equal(t, app.Object["spec"], actual.Object["spec"])
			expected := app.DeepCopy()
			unstructured.RemoveNestedField(expected.Object, "status", "operationState")
			require.Equal(t, expected.Object["status"], actual.Object["status"], "retain health, sync and history while clearing only old operation state")
			_, found, err := unstructured.NestedMap(actual.Object, "status", "operationState")
			require.NoError(t, err)
			require.False(t, found, "Argo's next status merge must not inherit old automated/resource filters")
			require.Equal(t, "10", actual.GetResourceVersion())
			require.Equal(t, "preserved", actual.GetAnnotations()["example.org/keep"])
			require.Equal(t, request.Reason, actual.GetAnnotations()["enclii.dev/last-ops-reason"])
			require.Equal(t, request.IdempotencyKey, actual.GetAnnotations()["enclii.dev/last-ops-idempotency-key"])
			for _, action := range client.Actions() {
				require.NotEqual(t, "patch", action.GetVerb())
			}
		})
	}
}

func TestAppsSyncRefusesActiveOrUnknownStatusWithoutRootOperation(t *testing.T) {
	for _, prior := range []any{
		map[string]any{"phase": "Running", "operation": selectiveAutoSync()},
		map[string]any{"phase": "Terminating", "operation": selectiveAutoSync()},
		map[string]any{"phase": "Pending"},
		map[string]any{"phase": ""},
		map[string]any{"phase": int64(1)},
		map[string]any{},
		"unreadable-state",
	} {
		app := syncRaceFixture()
		require.NoError(t, unstructured.SetNestedField(app.Object, prior, "status", "operationState"))
		client := fake.NewSimpleDynamicClient(runtime.NewScheme(), app)
		handler := &Handler{k8sClient: &k8sclient.Client{DynamicClient: client}}
		response, status := handler.handleOpsAppsSyncApply(context.Background(), "ops.apps.sync", syncRaceRequest())
		require.Equal(t, http.StatusConflict, status)
		require.Equal(t, "conflict", response.Status)
		for _, action := range client.Actions() {
			require.Equal(t, "get", action.GetVerb(), "unconfirmed state must never be erased")
		}
		actual, err := client.Resource(argoApplicationGVR).Namespace("argocd").Get(context.Background(), app.GetName(), metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, app.Object, actual.Object)
	}
}

func TestAppsSyncAcceptsOnlyAbsentOrKnownTerminalPreviousState(t *testing.T) {
	for _, phase := range []string{"absent", "Succeeded", "Failed", "Error"} {
		t.Run(phase, func(t *testing.T) {
			app := syncRaceFixture()
			if phase == "absent" {
				unstructured.RemoveNestedField(app.Object, "status", "operationState")
			} else {
				require.NoError(t, unstructured.SetNestedField(app.Object, phase, "status", "operationState", "phase"))
			}
			client := fake.NewSimpleDynamicClient(runtime.NewScheme(), app)
			handler := &Handler{k8sClient: &k8sclient.Client{DynamicClient: client}}
			response, status := handler.handleOpsAppsSyncApply(context.Background(), "ops.apps.sync", syncRaceRequest())
			require.Equal(t, http.StatusAccepted, status)
			require.Equal(t, "submitted", response.Status)
			for _, action := range client.Actions() {
				if action.GetVerb() == "update" {
					requested := action.(ktesting.UpdateAction).GetObject().(*unstructured.Unstructured)
					_, found, err := unstructured.NestedMap(requested.Object, "status", "operationState")
					require.NoError(t, err)
					require.False(t, found)
					require.Equal(t, "10", requested.GetResourceVersion())
					require.Empty(t, action.GetSubresource(), "Application CRD has no status subresource; both fields must change atomically")
				}
			}
		})
	}
}

func TestAppsSyncActiveOperationRemainsUntouched(t *testing.T) {
	app := syncRaceFixture()
	app.Object["operation"] = selectiveAutoSync()
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), app)
	handler := &Handler{k8sClient: &k8sclient.Client{DynamicClient: client}}
	response, status := handler.handleOpsAppsSyncApply(context.Background(), "ops.apps.sync", syncRaceRequest())
	require.Equal(t, http.StatusConflict, status)
	require.Equal(t, "already_running", response.Status)
	for _, action := range client.Actions() {
		require.Equal(t, "get", action.GetVerb())
	}
	actual, err := client.Resource(argoApplicationGVR).Namespace("argocd").Get(context.Background(), app.GetName(), metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, app.Object, actual.Object)
}
