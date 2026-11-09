package api

import (
	"context"
	"net/http"
	"testing"

	k8sclient "github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
)

func TestAppsSyncOperationOptions(t *testing.T) {
	policy := []any{"CreateNamespace=true", "RespectIgnoreDifferences=true", "ServerSideApply=true"}
	for _, tt := range []struct {
		name     string
		policy   []any
		override string
		want     []string
	}{
		{"legacy default", nil, "", []string{"PruneLast=true"}},
		{"inherits application enforcement", policy, "", []string{"CreateNamespace=true", "RespectIgnoreDifferences=true", "ServerSideApply=true", "PruneLast=true"}},
		{"blank override inherits policy", policy, " \t ", []string{"CreateNamespace=true", "RespectIgnoreDifferences=true", "ServerSideApply=true", "PruneLast=true"}},
		{"existing prune last is not duplicated", []any{"PruneLast=true", "ServerSideApply=true"}, "", []string{"PruneLast=true", "ServerSideApply=true"}},
		{"application prune last false is preserved", []any{"PruneLast=false", "RespectIgnoreDifferences=true"}, "", []string{"PruneLast=false", "RespectIgnoreDifferences=true"}},
		{"explicit options replace policy", policy, " ApplyOutOfSyncOnly=true, PruneLast=false ", []string{"ApplyOutOfSyncOnly=true", "PruneLast=false"}},
		{"explicit options do not gain defaults", policy, "ServerSideApply=false", []string{"ServerSideApply=false"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			app := syncRaceFixture()
			if tt.policy != nil {
				require.NoError(t, unstructured.SetNestedSlice(app.Object, tt.policy, "spec", "syncPolicy", "syncOptions"))
			}
			before := app.DeepCopy()
			client := fake.NewSimpleDynamicClient(runtime.NewScheme(), app)
			handler := &Handler{k8sClient: &k8sclient.Client{DynamicClient: client}}
			request := syncRaceRequest()
			request.Args["sync_options"] = tt.override

			response, status := handler.handleOpsAppsSyncApply(context.Background(), "ops.apps.sync", request)
			require.Equal(t, http.StatusAccepted, status)
			require.Equal(t, "submitted", response.Status)
			actual, err := client.Resource(argoApplicationGVR).Namespace("argocd").Get(context.Background(), app.GetName(), metav1.GetOptions{})
			require.NoError(t, err)
			options, found, err := unstructured.NestedStringSlice(actual.Object, "operation", "sync", "syncOptions")
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, tt.want, options)
			data, ok := response.Data.(map[string]any)
			require.True(t, ok)
			require.Equal(t, tt.want, data["syncOptions"])
			require.Equal(t, before.Object["spec"], actual.Object["spec"], "submission must not rewrite application policy")
		})
	}
}

func TestAppsSyncMalformedApplicationOptionsDoesNotSubmit(t *testing.T) {
	app := syncRaceFixture()
	require.NoError(t, unstructured.SetNestedField(app.Object, "ServerSideApply=true", "spec", "syncPolicy", "syncOptions"))
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), app)
	handler := &Handler{k8sClient: &k8sclient.Client{DynamicClient: client}}

	response, status := handler.handleOpsAppsSyncApply(context.Background(), "ops.apps.sync", syncRaceRequest())
	require.Equal(t, http.StatusInternalServerError, status)
	require.Equal(t, "failed", response.Status)
	for _, action := range client.Actions() {
		require.Equal(t, "get", action.GetVerb())
	}
	actual, err := client.Resource(argoApplicationGVR).Namespace("argocd").Get(context.Background(), app.GetName(), metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, app.Object, actual.Object)
}
