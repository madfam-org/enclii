package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func TestAdminGAVerify_SubcommandExists(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev", APIToken: "tok"}
	gaVerify := findSubcommand(NewAdminCommand(cfg), "ga-verify")
	require.NotNil(t, gaVerify)
	assert.NotNil(t, gaVerify.Flags().Lookup("json"))
	assert.NotNil(t, gaVerify.Flags().Lookup("stability"))
}

func TestAdminGAVerify_PassWithMockedEndpoints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/health/public":
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/v1/dashboard/stats":
			w.WriteHeader(http.StatusUnauthorized)
		case "/v1/admin/db/schema":
			_, _ = w.Write([]byte(`{"healthy":true,"status":{"version":30,"dirty":false},"pending":0}`))
		case "/v1/ops/storage/settings-apply":
			_, _ = w.Write([]byte(`{"operation":"ops.storage.settings-apply","status":"ready_to_apply","dry_run":true,"summary":"1 change pending"}`))
		case "/v1/ops/storage/prune-detached":
			_, _ = w.Write([]byte(`{"operation":"ops.storage.prune-detached","status":"succeeded","dry_run":true,"summary":"no detached Longhorn volumes to prune"}`))
		case "/v1/ops/jobs/list":
			_, _ = w.Write([]byte(`{"operation":"ops.jobs.list","status":"succeeded","dry_run":true,"summary":"jobs.list read completed through Enclii"}`))
		case "/v1/ops/storage/r2-audit":
			_, _ = w.Write([]byte(`{"operation":"ops.storage.r2-audit","status":"succeeded","dry_run":true,"summary":"3 R2 binding(s) checked","data":{"critical_count":0}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cfg := &config.Config{APIEndpoint: srv.URL, APIToken: "tok"}
	cmd := NewAdminCommand(cfg)
	cmd.SetArgs([]string{"ga-verify"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	require.NoError(t, cmd.Execute())
}

func TestAdminGAVerify_StabilityWithMockedEndpoints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/health/public":
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/v1/dashboard/stats":
			w.WriteHeader(http.StatusUnauthorized)
		case "/v1/admin/db/schema":
			_, _ = w.Write([]byte(`{"healthy":true,"status":{"version":30,"dirty":false},"pending":0}`))
		case "/v1/ops/storage/settings-apply":
			_, _ = w.Write([]byte(`{"operation":"ops.storage.settings-apply","status":"ready_to_apply","dry_run":true,"summary":"1 change pending"}`))
		case "/v1/ops/storage/prune-detached":
			_, _ = w.Write([]byte(`{"operation":"ops.storage.prune-detached","status":"succeeded","dry_run":true,"summary":"no detached Longhorn volumes to prune"}`))
		case "/v1/ops/jobs/list":
			_, _ = w.Write([]byte(`{"operation":"ops.jobs.list","status":"succeeded","dry_run":true,"summary":"jobs.list read completed through Enclii"}`))
		case "/v1/ops/storage/r2-audit":
			_, _ = w.Write([]byte(`{"operation":"ops.storage.r2-audit","status":"succeeded","dry_run":true,"summary":"3 R2 binding(s) checked","data":{"critical_count":0}}`))
		case "/v1/ops/apps/diff":
			_, _ = w.Write([]byte(`{"operation":"ops.apps.diff","status":"succeeded","dry_run":true,"data":{"driftedCount":0}}`))
		case "/v1/ops/secrets/vault":
			_, _ = w.Write([]byte(`{"operation":"ops.secrets.vault","status":"succeeded","dry_run":true,"summary":"vault ready"}`))
		case "/v1/ops/policy/violations":
			_, _ = w.Write([]byte(`{"operation":"ops.policy.violations","status":"succeeded","dry_run":true,"summary":"ok"}`))
		case "/v1/ops/storage/storageclass-apply":
			_, _ = w.Write([]byte(`{"operation":"ops.storage.storageclass-apply","status":"succeeded","dry_run":true,"summary":"Longhorn StorageClasses already present"}`))
		case "/v1/ops/policy/cosign-enable":
			_, _ = w.Write([]byte(`{"operation":"ops.policy.cosign-enable","status":"succeeded","dry_run":true,"summary":"namespaces already labeled"}`))
		case "/v1/ops/secrets/sync-sweep":
			_, _ = w.Write([]byte(`{"operation":"ops.secrets.sync-sweep","status":"succeeded","dry_run":true,"summary":"all ExternalSecrets in sweep scope are Ready"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cfg := &config.Config{APIEndpoint: srv.URL, APIToken: "tok"}
	cmd := NewAdminCommand(cfg)
	cmd.SetArgs([]string{"ga-verify", "--stability"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	require.NoError(t, cmd.Execute())
}
