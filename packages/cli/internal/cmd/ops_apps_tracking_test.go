package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAppsTrackingCLIReviewBinding(t *testing.T) {
	for _, apply := range []bool{false, true} {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			require.Equal(t, "/v1/ops/apps/tracking", r.URL.Path)
			var req operationRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			require.Equal(t, "ops.apps.tracking", req.Operation)
			require.Equal(t, !apply, req.DryRun)
			require.Empty(t, req.Scope["namespace"])
			if apply {
				require.Equal(t, "declare default", req.Reason)
				require.Equal(t, "cm-uid", req.Args["expect_uid"])
				require.Equal(t, "7", req.Args["expect_resource_version"])
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"planned"}`))
		}))
		cmd := newOpsAppsTrackingCommand(&config.Config{APIEndpoint: server.URL, APIToken: "test"})
		args := []string{"--json"}
		if apply {
			args = append(args, "--apply", "--reason", "declare default", "--expect-uid", "cm-uid", "--expect-resource-version", "7")
		}
		cmd.SetArgs(args)
		require.NoError(t, cmd.Execute())
		require.Equal(t, 1, calls)
		server.Close()
	}
	cmd := newOpsAppsTrackingCommand(&config.Config{})
	cmd.SetArgs([]string{"--apply", "--reason", "declare default"})
	require.ErrorContains(t, cmd.Execute(), "reviewed dry-run")
	cmd = newOpsAppsTrackingCommand(&config.Config{})
	cmd.SetArgs([]string{"argocd-cm"})
	require.Error(t, cmd.Execute())
}
