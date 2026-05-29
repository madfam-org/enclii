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
