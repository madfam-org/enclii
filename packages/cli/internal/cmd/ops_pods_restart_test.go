package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func TestOpsPodsRestartCLISendsReviewedTarget(t *testing.T) {
	for _, apply := range []bool{false, true} {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			require.Equal(t, "/v1/ops/pods/restart", r.URL.Path)
			var req operationRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			require.Equal(t, "ops.pods.restart", req.Operation)
			require.Equal(t, !apply, req.DryRun)
			require.Equal(t, "logging", req.Scope["namespace"])
			require.Equal(t, "pod/loki-0", req.Args["target"])
			if apply {
				require.Equal(t, "recreate the stuck ordinal", req.Reason)
				require.Equal(t, "uid-1", req.Args["expect_uid"])
			} else {
				require.NotContains(t, req.Args, "expect_uid")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"operation":"ops.pods.restart","status":"ready_to_apply"}`))
		}))
		cmd := newOpsPodsRestartCommand(&config.Config{APIEndpoint: server.URL, APIToken: "test"})
		args := []string{"pod/loki-0", "-n", "logging", "--json"}
		if apply {
			args = append(args, "--expect-uid", "uid-1", "--apply", "--reason", "recreate the stuck ordinal")
		}
		cmd.SetArgs(args)
		require.NoError(t, cmd.Execute())
		require.Equal(t, 1, calls)
		server.Close()
	}
}

func TestOpsPodsRestartCLIRejectsBeforeCallingTheAPI(t *testing.T) {
	for name, args := range map[string][]string{
		"missing kind":         {"loki-0", "-n", "logging"},
		"missing namespace":    {"pod/loki-0"},
		"apply without reason": {"pod/loki-0", "-n", "logging", "--apply"},
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
			defer server.Close()
			cmd := newOpsPodsRestartCommand(&config.Config{APIEndpoint: server.URL, APIToken: "test"})
			cmd.SetArgs(args)
			require.Error(t, cmd.Execute())
			require.Zero(t, calls, "invalid input never reaches the API")
		})
	}
}

func TestOpsPodsRestartIsWiredUnderOpsPods(t *testing.T) {
	restart := findSubcommand(findSubcommand(NewOpsCommand(&config.Config{}), "pods"), "restart")
	require.NotNil(t, restart)
	for _, flag := range []string{"apply", "reason", "expect-uid", "namespace", "json"} {
		require.NotNil(t, restart.Flags().Lookup(flag), "expected --%s", flag)
	}
}
