package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func TestOpsAddonsBackupCredentialRefusesAnUnboundApply(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"operation":"ops.addons.backup-credential","status":"succeeded"}`))
	}))
	defer srv.Close()
	for _, args := range [][]string{
		{"--apply", "--reason", "r", "--idempotency-key", "k"},
		{"--apply", "--reason", "r", "--expect-plan", "abc"},
		{"--expect-plan", "abc"},
	} {
		cmd := newOpsAddonsBackupCredentialCommand(&config.Config{APIEndpoint: srv.URL, APIToken: "synthetic"})
		cmd.SetArgs(args)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		assert.Error(t, cmd.Execute(), args)
	}
	assert.Equal(t, 0, calls, "an invalid flag set must not reach the API")
}

func TestOpsAddonsBackupCredentialDryRunAndApplyRequests(t *testing.T) {
	var got []operationRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/ops/addons/backup-credential", r.URL.Path)
		var req operationRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		got = append(got, req)
		_, _ = w.Write([]byte(`{"operation":"ops.addons.backup-credential","status":"ready_to_apply"}`))
	}))
	defer srv.Close()
	run := func(args ...string) {
		cmd := newOpsAddonsBackupCredentialCommand(&config.Config{APIEndpoint: srv.URL, APIToken: "synthetic"})
		cmd.SetArgs(append(args, "--json"))
		cmd.SetOut(io.Discard)
		require.NoError(t, cmd.Execute())
	}
	run()
	run("--rotate", "--apply", "--expect-plan", "abc123", "--idempotency-key", "addon-backups-1", "--reason", "addon backups credential")
	require.Len(t, got, 2)
	assert.True(t, got[0].DryRun)
	assert.Empty(t, got[0].Args["expected_plan"])
	assert.False(t, got[1].DryRun)
	assert.Equal(t, "abc123", got[1].Args["expected_plan"])
	assert.Equal(t, "true", got[1].Args["rotate"])
	assert.Equal(t, "addon-backups-1", got[1].IdempotencyKey)
	assert.Equal(t, "addon backups credential", got[1].Reason)
}
