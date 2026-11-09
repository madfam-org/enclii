package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	k8sclient "github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
)

// retireTestApp is an Argo Application as the live cluster serves it: a
// resourceVersion and UID, plus whatever finalizers and metadata the case needs.
func retireTestApp(finalizers []string, metadata map[string]any) *unstructured.Unstructured {
	meta := map[string]any{
		"name":            "legacy-crm",
		"namespace":       "argocd",
		"uid":             "uid-legacy-crm",
		"resourceVersion": "41",
	}
	for key, value := range metadata {
		meta[key] = value
	}
	app := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Application",
		"metadata":   meta,
		"status": map[string]any{
			"sync":   map[string]any{"status": "Synced", "revision": "abc123"},
			"health": map[string]any{"status": "Healthy"},
		},
	}}
	if finalizers != nil {
		app.SetFinalizers(finalizers)
	}
	return app
}

// retireDeleteObserver records, at the moment the Application is deleted, the
// finalizers it carries in the tracker and the options of the delete.
type retireDeleteObserver struct {
	called     bool
	finalizers []string
	options    metav1.DeleteOptions
}

func observeRetireDelete(t *testing.T, client *fake.FakeDynamicClient) *retireDeleteObserver {
	t.Helper()
	observer := &retireDeleteObserver{}
	client.PrependReactor("delete", "applications", func(action k8stesting.Action) (bool, runtime.Object, error) {
		del := action.(k8stesting.DeleteActionImpl)
		live, err := client.Tracker().Get(argoApplicationGVR, del.GetNamespace(), del.GetName())
		require.NoError(t, err)
		observer.called = true
		observer.finalizers = live.(*unstructured.Unstructured).GetFinalizers()
		observer.options = del.DeleteOptions
		return false, nil, nil
	})
	return observer
}

func runRetire(t *testing.T, client *fake.FakeDynamicClient, args map[string]string) (int, operatorOperationResponse) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handler := &Handler{k8sClient: &k8sclient.Client{DynamicClient: client}}
	router := gin.New()
	router.POST("/v1/ops/:domain/:action", handler.HandleOpsOperation)

	requestArgs := map[string]string{"target": "legacy-crm"}
	for key, value := range args {
		requestArgs[key] = value
	}
	body, err := json.Marshal(operatorOperationRequest{
		Operation: "ops.apps.retire",
		Reason:    "retire reviewed legacy Argo application",
		Scope:     map[string]string{"namespace": "argocd"},
		Args:      requestArgs,
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/v1/ops/apps/retire", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var resp operatorOperationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp), rec.Body.String())
	return rec.Code, resp
}

func retireVerbs(client *fake.FakeDynamicClient) []string {
	verbs := []string{}
	for _, action := range client.Actions() {
		verbs = append(verbs, action.GetVerb())
	}
	return verbs
}

// liveRetireApp reads the tracker directly, past any reactor a case installs.
func liveRetireApp(t *testing.T, client *fake.FakeDynamicClient) *unstructured.Unstructured {
	t.Helper()
	app, err := client.Tracker().Get(argoApplicationGVR, "argocd", "legacy-crm")
	require.NoError(t, err)
	return app.(*unstructured.Unstructured)
}

func TestHandleOpsAppsRetireDefaultRemovesArgoCascadeFinalizersBeforeTheDelete(t *testing.T) {
	app := retireTestApp([]string{
		"resources-finalizer.argocd.argoproj.io/background",
		"post-delete-finalizer.argocd.argoproj.io",
		"resources-finalizer.argocd.argoproj.io",
		"example.com/keep-me",
		"resources-finalizer.argocd.argoproj.io/foreground",
	}, nil)
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), app)
	deleted := observeRetireDelete(t, client)

	code, resp := runRetire(t, client, nil)

	require.Equal(t, http.StatusAccepted, code, resp.Summary)
	assert.Equal(t, "submitted", resp.Status)
	assert.Equal(t, []string{"get", "patch", "delete"}, retireVerbs(client))

	// What Argo CD sees when the Application is deleted: no finalizer that
	// makes it cascade, and every other finalizer still in place, in order.
	require.True(t, deleted.called)
	assert.Equal(t, []string{"post-delete-finalizer.argocd.argoproj.io", "example.com/keep-me"}, deleted.finalizers)
	require.NotNil(t, deleted.options.PropagationPolicy)
	assert.Equal(t, metav1.DeletePropagationOrphan, *deleted.options.PropagationPolicy)
	require.NotNil(t, deleted.options.Preconditions)
	assert.Equal(t, k8stypes.UID("uid-legacy-crm"), *deleted.options.Preconditions.UID)
	assert.Equal(t, "41", *deleted.options.Preconditions.ResourceVersion)

	// The patch is guarded by what was read, so a concurrent change fails it.
	patch := client.Actions()[1].(k8stesting.PatchActionImpl)
	assert.Equal(t, k8stypes.JSONPatchType, patch.GetPatchType())
	var ops []map[string]any
	require.NoError(t, json.Unmarshal(patch.GetPatch(), &ops))
	require.Len(t, ops, 3)
	assert.Equal(t, map[string]any{"op": "test", "path": "/metadata/resourceVersion", "value": "41"}, ops[0])
	assert.Equal(t, "test", ops[1]["op"])
	assert.Equal(t, "/metadata/finalizers", ops[1]["path"])
	assert.Len(t, ops[1]["value"], 5)
	assert.Equal(t, "replace", ops[2]["op"])

	data := resp.Data.(map[string]any)
	assert.Equal(t, "Orphan", data["propagation"])
	assert.Equal(t, false, data["cascade"])
	assert.Equal(t, []any{
		"resources-finalizer.argocd.argoproj.io/background",
		"resources-finalizer.argocd.argoproj.io",
		"resources-finalizer.argocd.argoproj.io/foreground",
	}, data["finalizersRemoved"])
	assert.Empty(t, resp.Warnings)

	_, err := client.Tracker().Get(argoApplicationGVR, "argocd", "legacy-crm")
	require.True(t, k8serrors.IsNotFound(err), "the Application must be gone")
}

func TestHandleOpsAppsRetireDefaultWithoutCascadeFinalizerOnlyDeletes(t *testing.T) {
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), retireTestApp([]string{"post-delete-finalizer.argocd.argoproj.io"}, nil))
	deleted := observeRetireDelete(t, client)

	code, resp := runRetire(t, client, nil)

	require.Equal(t, http.StatusAccepted, code, resp.Summary)
	assert.Equal(t, []string{"get", "delete"}, retireVerbs(client))
	assert.Equal(t, []string{"post-delete-finalizer.argocd.argoproj.io"}, deleted.finalizers)
	assert.Equal(t, metav1.DeletePropagationOrphan, *deleted.options.PropagationPolicy)
	assert.Equal(t, "41", *deleted.options.Preconditions.ResourceVersion)
	assert.Equal(t, []any{}, resp.Data.(map[string]any)["finalizersRemoved"])
}

func TestHandleOpsAppsRetireCascadeKeepsTheResourcesFinalizer(t *testing.T) {
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), retireTestApp([]string{"resources-finalizer.argocd.argoproj.io"}, nil))
	deleted := observeRetireDelete(t, client)

	code, resp := runRetire(t, client, map[string]string{"cascade": "true"})

	require.Equal(t, http.StatusAccepted, code, resp.Summary)
	assert.Equal(t, []string{"get", "delete"}, retireVerbs(client), "cascade=true must not patch the finalizers")
	assert.Equal(t, []string{"resources-finalizer.argocd.argoproj.io"}, deleted.finalizers)
	require.NotNil(t, deleted.options.PropagationPolicy)
	assert.Equal(t, metav1.DeletePropagationForeground, *deleted.options.PropagationPolicy)
	assert.Nil(t, deleted.options.Preconditions)

	data := resp.Data.(map[string]any)
	assert.Equal(t, "Foreground", data["propagation"])
	assert.Equal(t, true, data["cascade"])
	assert.Equal(t, []any{}, data["finalizersRemoved"])
	assert.Empty(t, resp.Warnings)
}

func TestHandleOpsAppsRetireCascadeWithoutFinalizerWarnsNothingCascades(t *testing.T) {
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), retireTestApp(nil, nil))

	code, resp := runRetire(t, client, map[string]string{"cascade": "true"})

	require.Equal(t, http.StatusAccepted, code, resp.Summary)
	require.Len(t, resp.Warnings, 1)
	assert.Contains(t, resp.Warnings[0], "no Argo resources finalizer")
}

func TestHandleOpsAppsRetireRefusesApplicationSetGeneratedApplications(t *testing.T) {
	for _, cascade := range []string{"", "true"} {
		t.Run("cascade="+cascade, func(t *testing.T) {
			app := retireTestApp([]string{"resources-finalizer.argocd.argoproj.io"}, map[string]any{
				"ownerReferences": []any{map[string]any{
					"apiVersion":         "argoproj.io/v1alpha1",
					"kind":               "ApplicationSet",
					"name":               "project-applications",
					"uid":                "uid-appset",
					"controller":         true,
					"blockOwnerDeletion": true,
				}},
			})
			client := fake.NewSimpleDynamicClient(runtime.NewScheme(), app)

			code, resp := runRetire(t, client, map[string]string{"cascade": cascade})

			require.Equal(t, http.StatusConflict, code)
			assert.Equal(t, "refused", resp.Status)
			assert.Contains(t, resp.Summary, "ApplicationSet project-applications")
			assert.Contains(t, strings.Join(resp.Warnings, "\n"), `"lifecycle": "retired"`)
			assert.Equal(t, "project-applications", resp.Data.(map[string]any)["applicationSet"])
			assert.Equal(t, []string{"get"}, retireVerbs(client), "a refused retire must not write")
			assert.Equal(t, []string{"resources-finalizer.argocd.argoproj.io"}, liveRetireApp(t, client).GetFinalizers())
		})
	}
}

func TestHandleOpsAppsRetireDeletesNothingWhenTheApplicationChangedSinceRead(t *testing.T) {
	live := retireTestApp([]string{"resources-finalizer.argocd.argoproj.io", "post-delete-finalizer.argocd.argoproj.io"}, nil)
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), live)
	// The read returns an older copy: the live object has moved on to a new
	// resourceVersion and gained a finalizer since.
	stale := retireTestApp([]string{"resources-finalizer.argocd.argoproj.io"}, map[string]any{"resourceVersion": "40"})
	client.PrependReactor("get", "applications", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, stale.DeepCopy(), nil
	})

	code, resp := runRetire(t, client, nil)

	assert.NotEqual(t, http.StatusAccepted, code)
	assert.Contains(t, resp.Summary, "nothing was deleted")
	assert.Equal(t, []string{"get", "patch"}, retireVerbs(client))
	assert.Equal(t, []string{"resources-finalizer.argocd.argoproj.io", "post-delete-finalizer.argocd.argoproj.io"}, liveRetireApp(t, client).GetFinalizers())
}

func TestHandleOpsAppsRetireRefusesAnApplicationAlreadyBeingDeleted(t *testing.T) {
	app := retireTestApp([]string{"resources-finalizer.argocd.argoproj.io"}, map[string]any{
		"deletionTimestamp": "2026-11-04T10:00:00Z",
	})
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), app)

	code, resp := runRetire(t, client, nil)

	require.Equal(t, http.StatusConflict, code)
	assert.Equal(t, "already_deleting", resp.Status)
	assert.Equal(t, []string{"get"}, retireVerbs(client))
	assert.Equal(t, []string{"resources-finalizer.argocd.argoproj.io"}, liveRetireApp(t, client).GetFinalizers())
}

func TestHandleOpsAppsRetireWarnsWhenAParentApplicationTracksIt(t *testing.T) {
	app := retireTestApp([]string{"resources-finalizer.argocd.argoproj.io"}, map[string]any{
		"annotations": map[string]any{
			argoTrackingIDAnnotation: "enclii-infrastructure:argoproj.io/Application:argocd/legacy-crm",
		},
	})
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), app)

	code, resp := runRetire(t, client, nil)

	require.Equal(t, http.StatusAccepted, code, resp.Summary)
	require.Len(t, resp.Warnings, 1)
	assert.Contains(t, resp.Warnings[0], "enclii-infrastructure")
	assert.Equal(t, "enclii-infrastructure", resp.Data.(map[string]any)["trackedBy"])
}

func TestArgoTrackingParent(t *testing.T) {
	for _, tc := range []struct {
		trackingID string
		want       string
	}{
		{"", ""},
		{"enclii-infrastructure:argoproj.io/Application:argocd/legacy-crm", "enclii-infrastructure"},
		{"legacy-crm:argoproj.io/Application:argocd/legacy-crm", ""},
		{"no-separator", ""},
	} {
		app := retireTestApp(nil, nil)
		if tc.trackingID != "" {
			app.SetAnnotations(map[string]string{argoTrackingIDAnnotation: tc.trackingID})
		}
		assert.Equal(t, tc.want, argoTrackingParent(app), tc.trackingID)
	}
}
