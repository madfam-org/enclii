package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	k8sclient "github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestReadPodLogsSelectsPreviousContainer(t *testing.T) {
	for _, previous := range []string{"", "false", "true"} {
		t.Run("previous="+previous, func(t *testing.T) {
			requests := make(chan *http.Request, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- r
				if r.URL.Query().Get("previous") == "true" {
					_, _ = w.Write([]byte("failed startup fixture"))
				} else {
					_, _ = w.Write([]byte("current process fixture"))
				}
			}))
			defer server.Close()
			client, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
			require.NoError(t, err)
			handler := &Handler{k8sClient: &k8sclient.Client{KubeClient: client}}
			data, err := handler.readPodLogs(context.Background(), operatorOperationRequest{Scope: map[string]string{"namespace": "fixture"}, Args: map[string]string{"target": "sample-pod", "container": "app", "previous": previous, "tailLines": "50", "limitBytes": "4096"}})
			require.NoError(t, err)
			request := <-requests
			require.Equal(t, "/api/v1/namespaces/fixture/pods/sample-pod/log", request.URL.Path)
			require.Equal(t, "app", request.URL.Query().Get("container"))
			require.Equal(t, "50", request.URL.Query().Get("tailLines"))
			require.Equal(t, "4096", request.URL.Query().Get("limitBytes"))
			require.Equal(t, previous == "true", data["previous"])
			if previous == "true" {
				require.Equal(t, "true", request.URL.Query().Get("previous"))
				require.Equal(t, "failed startup fixture", data["logs"])
			} else {
				require.NotEqual(t, "true", request.URL.Query().Get("previous"))
				require.Equal(t, "current process fixture", data["logs"])
			}
		})
	}
}

func TestReadPodLogsRejectsInvalidPreviousBeforeAccess(t *testing.T) {
	handler := &Handler{}
	_, err := handler.readPodLogs(context.Background(), operatorOperationRequest{Args: map[string]string{"target": "sample-pod", "previous": "sometimes"}})
	require.ErrorContains(t, err, "previous must be a boolean")
}
