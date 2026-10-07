package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/stretchr/testify/require"
)

func TestJobsSuspendCLIReviewBinding(t *testing.T) {
	for _, apply := range []bool{false, true} {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			require.Equal(t, "/v1/ops/jobs/suspend", r.URL.Path)
			var req operationRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			require.Equal(t, !apply, req.DryRun)
			require.Equal(t, "fixture-dev", req.Scope["namespace"])
			require.Equal(t, "fixture-job", req.Args["target"])
			if apply {
				require.Equal(t, "uid-1", req.Args["expect_uid"])
				require.Equal(t, "42", req.Args["expect_resource_version"])
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"succeeded"}`))
		}))
		cfg := &config.Config{APIEndpoint: server.URL, APIToken: "test"}
		cmd := newOpsJobsSuspendCommand(cfg)
		args := []string{"fixture-job", "--namespace", "fixture-dev", "--project", "fixture", "--service", "00000000-0000-0000-0000-000000000001", "--json"}
		if apply {
			args = append(args, "--apply", "--reason", "containment", "--expect-uid", "uid-1", "--expect-resource-version", "42")
		}
		cmd.SetArgs(args)
		require.NoError(t, cmd.Execute())
		require.Equal(t, 1, calls)
		server.Close()
	}
	cmd := newOpsJobsSuspendCommand(&config.Config{})
	cmd.SetArgs([]string{"fixture-job", "--namespace", "fixture-dev", "--project", "fixture", "--service", "id", "--apply", "--reason", "containment"})
	require.ErrorContains(t, cmd.Execute(), "reviewed dry-run")
}
