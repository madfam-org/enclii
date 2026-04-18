package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func TestNewVaultCommand(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	cmd := NewVaultCommand(cfg)
	require.NotNil(t, cmd)
	assert.Equal(t, "vault", cmd.Use)

	// Must have a status subcommand wired.
	statusCmd, _, err := cmd.Find([]string{"status"})
	require.NoError(t, err)
	require.NotNil(t, statusCmd)
	assert.Equal(t, "status", statusCmd.Use)
}

func TestResolveVaultAddr_FlagWins(t *testing.T) {
	t.Setenv("ENCLII_VAULT_ADDR", "http://from-env:8200")
	t.Setenv("VAULT_ADDR", "http://from-vault-env:8200")
	cfg := &config.Config{}

	got := resolveVaultAddr("http://from-flag:8200", cfg)
	assert.Equal(t, "http://from-flag:8200", got)
}

func TestResolveVaultAddr_EnclilEnvOverridesVaultAddr(t *testing.T) {
	// If both env vars are set, the enclii-specific one wins to avoid
	// accidentally pointing at a user's dev Vault when on the corporate
	// cluster.
	t.Setenv("ENCLII_VAULT_ADDR", "http://enclii:8200")
	t.Setenv("VAULT_ADDR", "http://generic:8200")
	cfg := &config.Config{}

	got := resolveVaultAddr("", cfg)
	assert.Equal(t, "http://enclii:8200", got)
}

func TestResolveVaultAddr_VaultAddrFallback(t *testing.T) {
	os.Unsetenv("ENCLII_VAULT_ADDR")
	t.Setenv("VAULT_ADDR", "http://generic:8200")
	cfg := &config.Config{}

	got := resolveVaultAddr("", cfg)
	assert.Equal(t, "http://generic:8200", got)
}

func TestResolveVaultAddr_DefaultClusterInternal(t *testing.T) {
	os.Unsetenv("ENCLII_VAULT_ADDR")
	os.Unsetenv("VAULT_ADDR")
	cfg := &config.Config{}

	got := resolveVaultAddr("", cfg)
	assert.Equal(t, defaultVaultAddr, got)
}

func TestRunVaultStatus_NotInitialized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(sysHealthResponse{
			Initialized: false,
			Sealed:      true,
		})
	}))
	defer server.Close()

	var out bytes.Buffer
	err := runVaultStatus(context.Background(), &out, server.URL)
	require.NoError(t, err)

	result := out.String()
	assert.Contains(t, result, "Initialized:    false")
	assert.Contains(t, result, "vault-bootstrap.md")
}

func TestRunVaultStatus_SealedButInitialized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK) // uninitcode=200&sealedcode=200 in query
		_ = json.NewEncoder(w).Encode(sysHealthResponse{
			Initialized: true,
			Sealed:      true,
			Version:     "1.18.3",
		})
	}))
	defer server.Close()

	var out bytes.Buffer
	err := runVaultStatus(context.Background(), &out, server.URL)
	require.NoError(t, err)

	result := out.String()
	assert.Contains(t, result, "Initialized:    true")
	assert.Contains(t, result, "Sealed:         true")
	assert.Contains(t, result, "Version:        1.18.3")
	assert.Contains(t, result, "unseal")
}

func TestRunVaultStatus_Healthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(sysHealthResponse{
			Initialized: true,
			Sealed:      false,
			Version:     "1.18.3",
			ClusterName: "madfam-prod-vault",
		})
	}))
	defer server.Close()

	var out bytes.Buffer
	err := runVaultStatus(context.Background(), &out, server.URL)
	require.NoError(t, err)

	result := out.String()
	assert.Contains(t, result, "initialized and unsealed")
	assert.Contains(t, result, "madfam-prod-vault")
}

func TestRunVaultStatus_Unreachable(t *testing.T) {
	// Point at a port nothing listens on; expect a friendly error, not a
	// crash.
	var out bytes.Buffer
	err := runVaultStatus(context.Background(), &out, "http://127.0.0.1:1")
	// No-op on unreachable — see comment in runVaultStatus.
	require.NoError(t, err)
	assert.Contains(t, out.String(), "unreachable")
	assert.Contains(t, strings.ToLower(out.String()), "port-forward")
}

func TestRunVaultStatus_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json at all"))
	}))
	defer server.Close()

	var out bytes.Buffer
	err := runVaultStatus(context.Background(), &out, server.URL)
	// Malformed body is surfaced, not error-returned — we want the operator
	// to see the raw response, not a Go type assertion panic.
	require.NoError(t, err)
	assert.Contains(t, out.String(), "HTTP 200")
	assert.Contains(t, out.String(), "not json at all")
}
