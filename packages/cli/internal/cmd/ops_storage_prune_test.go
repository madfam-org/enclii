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

func TestOpsStoragePruneDetachedRequiresExpectPlanWithApply(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"operation":"ops.storage.prune-detached","status":"succeeded"}`))
	}))
	defer srv.Close()
	for _, args := range [][]string{
		{"--apply", "--reason", "orphan cleanup"},
		{"--expect-plan", "abc"},
	} {
		cmd := newOpsStoragePruneDetachedCommand(&config.Config{APIEndpoint: srv.URL, APIToken: "synthetic"})
		cmd.SetArgs(args)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		assert.Error(t, cmd.Execute(), args)
	}
	assert.Equal(t, 0, calls, "an invalid flag set must not reach the API")
}

func TestOpsStoragePruneDetachedSendsTheReviewedPlan(t *testing.T) {
	var got operationRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/ops/storage/prune-detached", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		_, _ = w.Write([]byte(`{"operation":"ops.storage.prune-detached","status":"submitted"}`))
	}))
	defer srv.Close()
	cmd := newOpsStoragePruneDetachedCommand(&config.Config{APIEndpoint: srv.URL, APIToken: "synthetic"})
	cmd.SetArgs([]string{"pvc-x", "--expect-plan", "abc123", "--apply", "--reason", "orphan cleanup", "--json"})
	cmd.SetOut(io.Discard)
	require.NoError(t, cmd.Execute())
	assert.False(t, got.DryRun)
	assert.Equal(t, "abc123", got.Args["expected_plan"])
	assert.Equal(t, "pvc-x", got.Args["target"])
	assert.Equal(t, "orphan cleanup", got.Reason)
}
