package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func captureOperation(t *testing.T, reply string) (*httptest.Server, *string, *operationRequest) {
	t.Helper()
	var seen string
	var body operationRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Method + " " + r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen, &body
}

func TestOpsJunctionsRebind_IsADryRunByDefault(t *testing.T) {
	srv, seen, body := captureOperation(t, `{"operation":"ops.junctions.rebind","status":"ready_to_apply","dry_run":true}`)
	root := NewRootCommand(&config.Config{APIEndpoint: srv.URL, APIToken: "tok"})
	root.SetArgs([]string{"ops", "junctions", "rebind", "api.example.com",
		"--project", "my-project", "--to-service", "my-api", "--environment", "production"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	require.NoError(t, root.Execute())
	assert.Equal(t, "POST /v1/ops/junctions/rebind", *seen)
	assert.True(t, body.DryRun)
	assert.Equal(t, "my-project", body.Scope["project"])
	assert.Equal(t, map[string]string{
		"target": "api.example.com", "to_service": "my-api", "environment": "production",
	}, body.Args)
}

func TestOpsJunctionsRebind_RequiresServiceAndEnvironment(t *testing.T) {
	for name, args := range map[string][]string{
		"no service":     {"--project", "p", "--environment", "production"},
		"no environment": {"--project", "p", "--to-service", "s"},
		"no project":     {"--to-service", "s", "--environment", "production"},
	} {
		t.Run(name, func(t *testing.T) {
			called := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true }))
			defer srv.Close()
			root := NewRootCommand(&config.Config{APIEndpoint: srv.URL, APIToken: "tok"})
			root.SetArgs(append([]string{"ops", "junctions", "rebind", "api.example.com"}, args...))
			root.SetOut(io.Discard)
			root.SetErr(io.Discard)
			assert.Error(t, root.Execute())
			assert.False(t, called, "the API must not be called without the required flags")
		})
	}
}

func TestOpsJunctionsRebind_ApplyNeedsReason(t *testing.T) {
	srv, seen, _ := captureOperation(t, `{}`)
	root := NewRootCommand(&config.Config{APIEndpoint: srv.URL, APIToken: "tok"})
	root.SetArgs([]string{"ops", "junctions", "rebind", "api.example.com",
		"--project", "p", "--to-service", "s", "--environment", "production", "--apply"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	assert.Error(t, root.Execute())
	assert.Empty(t, *seen)
}

func TestTunnelsApply_SendsAllowRepointAndExpectPlan(t *testing.T) {
	srv, _, body := captureOperation(t, `{"operation":"providers.cloudflare.tunnels-apply","status":"blocked","dry_run":true}`)
	root := NewRootCommand(&config.Config{APIEndpoint: srv.URL, APIToken: "tok"})
	root.SetArgs([]string{"providers", "cloudflare", "tunnels-apply", "--project", "p",
		"--allow-repoint", "api.example.com", "--allow-repoint", "admin.example.com", "--expect-plan", "abc123"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	require.NoError(t, root.Execute())
	assert.True(t, body.DryRun)
	assert.Equal(t, "api.example.com,admin.example.com", body.Args["allow_repoint"])
	assert.Equal(t, "abc123", body.Args["expect_plan"])
}

func TestTunnelsApply_WithoutGuardFlagsSendsNoGuardArgs(t *testing.T) {
	srv, _, body := captureOperation(t, `{"operation":"providers.cloudflare.tunnels-apply","status":"succeeded","dry_run":true}`)
	root := NewRootCommand(&config.Config{APIEndpoint: srv.URL, APIToken: "tok"})
	root.SetArgs([]string{"providers", "cloudflare", "tunnels-apply", "--project", "p"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	require.NoError(t, root.Execute())
	_, hasAllow := body.Args["allow_repoint"]
	assert.False(t, hasAllow, "no hostname is ever allowed implicitly")
}

func TestPrintRoutePlan_LabelsComeFirst(t *testing.T) {
	var out bytes.Buffer
	printRoutePlan(&out, map[string]any{"plan": []any{
		map[string]any{
			"hostname": "api.example.com", "label": "REPOINT (blocked)",
			"current_service": "http://api.prod.svc.cluster.local:80", "desired_service": "http://web.prod.svc.cluster.local:80",
			"environment": "production", "environment_source": "default",
		},
	}})
	assert.Contains(t, out.String(), "REPOINT (blocked)  api.example.com")
	assert.Contains(t, out.String(), "production (default)")

	out.Reset()
	printRoutePlan(&out, map[string]any{"rows": []any{}})
	assert.Empty(t, out.String())
}
