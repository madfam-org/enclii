package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func TestNewRollbackCommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewRollbackCommand(cfg)
	require.NotNil(t, cmd)

	// P2.6 widened the signature to accept an optional positional target
	// (v-label, UUID, or digest shortsha) alongside the service name.
	assert.Equal(t, "rollback [service] [v{n}|digest]", cmd.Use)

	// Verify flags exist with correct defaults
	envFlag := cmd.Flags().Lookup("env")
	require.NotNil(t, envFlag)
	assert.Equal(t, "dev", envFlag.DefValue)

	toFlag := cmd.Flags().Lookup("to")
	require.NotNil(t, toFlag)
	assert.Equal(t, "", toFlag.DefValue)

	// Verify shorthand flags
	assert.Equal(t, "e", envFlag.Shorthand)
	assert.Equal(t, "t", toFlag.Shorthand)
}

func TestRollbackService_NoServiceName(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	err := rollbackService(cfg, "", "dev", "")
	require.Error(t, err)

	// Should be a ValidationError
	var validationErr *exitcodes.ValidationError
	assert.ErrorAs(t, err, &validationErr)
	assert.Contains(t, err.Error(), "service name is required")
}

// TestResolveRollbackTarget_VLabel exercises the P2.6 v-label resolution
// path: `enclii rollback my-svc v42` should look up the deployment via
// GET /v1/services/<id>/versions/42 and return its UUID. Works for any
// v-number in history, not just the most recent.
func TestResolveRollbackTarget_VLabel(t *testing.T) {
	serviceID := uuid.New()
	expectedDepID := uuid.New()
	var recordedPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recordedPath = r.URL.Path
		require.Equal(t, "/v1/services/"+serviceID.String()+"/versions/7", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		version := 7
		resp := types.Deployment{
			ID:            expectedDepID,
			ReleaseID:     uuid.New(),
			EnvironmentID: uuid.New(),
			VersionNumber: &version,
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	apiClient := client.NewAPIClient(server.URL, "token")
	// Passing v7 resolves to the deployment UUID even though it's not the
	// most recent — verifies the "works for any v-number in history"
	// contract in the P2.6 spec.
	resolved, err := resolveRollbackTarget(context.Background(), apiClient, serviceID.String(), "v7")
	require.NoError(t, err)
	assert.Equal(t, expectedDepID.String(), resolved)
	assert.Equal(t, "/v1/services/"+serviceID.String()+"/versions/7", recordedPath)
}

// TestResolveRollbackTarget_UUID passes the UUID through unchanged.
func TestResolveRollbackTarget_UUID(t *testing.T) {
	apiClient := client.NewAPIClient("http://unused", "")
	depID := uuid.New().String()
	resolved, err := resolveRollbackTarget(context.Background(), apiClient, uuid.New().String(), depID)
	require.NoError(t, err)
	assert.Equal(t, depID, resolved)
}

// TestResolveRollbackTarget_Empty returns empty so caller can fall back
// to "previous running deployment" default.
func TestResolveRollbackTarget_Empty(t *testing.T) {
	apiClient := client.NewAPIClient("http://unused", "")
	resolved, err := resolveRollbackTarget(context.Background(), apiClient, uuid.New().String(), "")
	require.NoError(t, err)
	assert.Equal(t, "", resolved)
}

// TestResolveRollbackTarget_UnresolvableShortsha surfaces a clear error
// when the input is neither a v-label, nor a UUID, nor a shortsha prefix
// that matches any known deployment.
func TestResolveRollbackTarget_UnresolvableShortsha(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return an empty deployment list so nothing matches the shortsha.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"deployments": []any{}})
	}))
	defer server.Close()

	apiClient := client.NewAPIClient(server.URL, "token")
	_, err := resolveRollbackTarget(context.Background(), apiClient, uuid.New().String(), "zzz9999")
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "could not resolve rollback target"))
}

// TestParseVersionLabel covers the pure-string parser used by both the
// CLI and the API handler.
func TestParseVersionLabel(t *testing.T) {
	cases := []struct {
		in     string
		wantN  int
		wantOK bool
	}{
		{"v1", 1, true},
		{"v42", 42, true},
		{"V42", 42, true},       // case-insensitive prefix
		{" v7 ", 7, true},       // whitespace tolerated
		{"", 0, false},          // empty
		{"v", 0, false},         // prefix only
		{"42", 0, false},        // no prefix
		{"v0", 0, false},        // zero is invalid (versions start at 1)
		{"v-1", 0, false},       // negative
		{"vabc", 0, false},      // non-numeric
		{"version42", 0, false}, // wrong prefix
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			n, ok := types.ParseVersionLabel(tc.in)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.wantN, n)
		})
	}
}
