package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOpsPreviousLogsRequestAndConfirmation(t *testing.T) {
	for _, confirmed := range []bool{true, false} {
		name := "supported"
		if !confirmed {
			name = "old-server"
		}
		t.Run(name, func(t *testing.T) {
			requests := make(chan operationRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request operationRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					http.Error(w, "bad request", 400)
					return
				}
				requests <- request
				data := map[string]any{"logs": "fixture logs"}
				if confirmed {
					data["previous"] = true
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"status": "succeeded", "dry_run": true, "data": data})
			}))
			defer server.Close()
			command := newOpsPodsLogsCommand(&config.Config{APIEndpoint: server.URL, APIToken: "test-token"})
			var output bytes.Buffer
			command.SetOut(&output)
			command.SetErr(&output)
			command.SetArgs([]string{"sample-pod", "-n", "fixture", "--container", "app", "--previous", "--tail", "50", "--limit-bytes", "4096"})
			err := command.Execute()
			if confirmed {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "server did not confirm previous-container logs")
			}
			request := <-requests
			require.Equal(t, "ops.pods.logs", request.Operation)
			require.True(t, request.DryRun)
			require.Equal(t, "fixture", request.Scope["namespace"])
			require.Equal(t, "true", request.Args["previous"])
			require.Equal(t, "app", request.Args["container"])
			require.Equal(t, "50", request.Args["tailLines"])
			require.Equal(t, "4096", request.Args["limitBytes"])
		})
	}
}

func TestOpsPreviousLogsReportsAdapterFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "failed", "summary": "previous terminated container not found"})
	}))
	defer server.Close()
	command := newOpsPodsLogsCommand(&config.Config{APIEndpoint: server.URL, APIToken: "test-token"})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"sample-pod", "-n", "fixture", "--previous"})
	require.ErrorContains(t, command.Execute(), "previous terminated container not found")
}
