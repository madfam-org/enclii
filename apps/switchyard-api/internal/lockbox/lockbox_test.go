package lockbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// newTestVaultClient creates a VaultClient pointed at a test server.
func newTestVaultClient(t *testing.T, server *httptest.Server) *VaultClient {
	t.Helper()
	return &VaultClient{
		address:    server.URL,
		token:      "test-vault-token",
		namespace:  "",
		httpClient: server.Client(),
		enabled:    true,
	}
}

// newTestVaultClientWithNamespace creates a VaultClient that sends the
// X-Vault-Namespace header.
func newTestVaultClientWithNamespace(t *testing.T, server *httptest.Server, ns string) *VaultClient {
	t.Helper()
	c := newTestVaultClient(t, server)
	c.namespace = ns
	return c
}

// vaultKVv2Response returns a realistic Vault KV v2 JSON body for the
// /v1/secret/data/* endpoint.
func vaultKVv2Response(data map[string]interface{}, version int, created time.Time) []byte {
	resp := map[string]interface{}{
		"data": map[string]interface{}{
			"data": data,
			"metadata": map[string]interface{}{
				"version":      version,
				"created_time": created.Format(time.RFC3339Nano),
				"destroyed":    false,
			},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// vaultMetadataResponse returns a realistic Vault KV v2 metadata JSON body
// for the /v1/secret/metadata/* endpoint.
func vaultMetadataResponse(currentVersion int, versions map[string]VaultVersionInfo, created, updated time.Time) []byte {
	vMap := make(map[string]interface{}, len(versions))
	for k, v := range versions {
		vMap[k] = map[string]interface{}{
			"version":      v.Version,
			"created_time": v.CreatedTime.Format(time.RFC3339Nano),
			"destroyed":    v.Destroyed,
		}
	}
	resp := map[string]interface{}{
		"data": map[string]interface{}{
			"current_version": currentVersion,
			"versions":        vMap,
			"created_time":    created.Format(time.RFC3339Nano),
			"updated_time":    updated.Format(time.RFC3339Nano),
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// ---------------------------------------------------------------------------
// NewVaultClient
// ---------------------------------------------------------------------------

func TestNewVaultClient_NilConfig(t *testing.T) {
	client := NewVaultClient(nil)
	require.NotNil(t, client)
	assert.False(t, client.IsEnabled(), "client created from nil config must be disabled")
}

func TestNewVaultClient_ValidConfig(t *testing.T) {
	cfg := &VaultConfig{
		Address:      "https://vault.example.com/",
		Token:        "s.mytoken",
		Namespace:    "admin",
		PollInterval: 30 * time.Second,
		Enabled:      true,
	}

	client := NewVaultClient(cfg)
	require.NotNil(t, client)
	assert.True(t, client.IsEnabled())
	assert.Equal(t, "https://vault.example.com", client.address, "trailing slash must be stripped")
	assert.Equal(t, "s.mytoken", client.token)
	assert.Equal(t, "admin", client.namespace)
	assert.NotNil(t, client.httpClient)
}

func TestNewVaultClient_DisabledConfig(t *testing.T) {
	cfg := &VaultConfig{
		Address: "https://vault.example.com",
		Token:   "s.mytoken",
		Enabled: false,
	}

	client := NewVaultClient(cfg)
	require.NotNil(t, client)
	assert.False(t, client.IsEnabled(), "client must respect Enabled=false in config")
}

// ---------------------------------------------------------------------------
// GetSecret
// ---------------------------------------------------------------------------

func TestGetSecret_Success_StringValue(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/secret/data/myapp/db", r.URL.Path)
		assert.Equal(t, "test-vault-token", r.Header.Get("X-Vault-Token"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(vaultKVv2Response(
			map[string]interface{}{"DATABASE_URL": "postgres://localhost:5432/app"},
			3, now,
		))
	}))
	defer server.Close()

	client := newTestVaultClient(t, server)
	secret, err := client.GetSecret(context.Background(), "secret/data/myapp/db")

	require.NoError(t, err)
	require.NotNil(t, secret)
	assert.Equal(t, "secret/data/myapp/db", secret.Path)
	assert.Equal(t, ProviderVault, secret.Provider)
	assert.Equal(t, 3, secret.Version)
	assert.Equal(t, "DATABASE_URL", secret.Name)
	assert.Equal(t, "postgres://localhost:5432/app", secret.Value)
	assert.Equal(t, now, secret.CreatedAt.Truncate(time.Second))
}

func TestGetSecret_Success_NonStringValue(t *testing.T) {
	// When the value stored in Vault is not a string, the client should
	// marshal it to JSON.
	now := time.Now().UTC().Truncate(time.Second)
	complexVal := map[string]interface{}{"host": "localhost", "port": float64(5432)}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(vaultKVv2Response(
			map[string]interface{}{"config": complexVal},
			1, now,
		))
	}))
	defer server.Close()

	client := newTestVaultClient(t, server)
	secret, err := client.GetSecret(context.Background(), "secret/data/myapp/config")

	require.NoError(t, err)
	require.NotNil(t, secret)
	assert.Equal(t, "config", secret.Name)

	// The value should be valid JSON representing the complex object.
	var parsed map[string]interface{}
	err = json.Unmarshal([]byte(secret.Value), &parsed)
	require.NoError(t, err, "non-string value must be JSON-encoded")
	assert.Equal(t, "localhost", parsed["host"])
}

func TestGetSecret_NamespaceHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "admin/team-a", r.Header.Get("X-Vault-Namespace"),
			"namespace header must be set when configured")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		now := time.Now().UTC()
		w.Write(vaultKVv2Response(
			map[string]interface{}{"KEY": "value"}, 1, now,
		))
	}))
	defer server.Close()

	client := newTestVaultClientWithNamespace(t, server, "admin/team-a")
	secret, err := client.GetSecret(context.Background(), "secret/data/myapp/key")
	require.NoError(t, err)
	require.NotNil(t, secret)
}

func TestGetSecret_DisabledClient(t *testing.T) {
	client := NewVaultClient(nil) // disabled
	secret, err := client.GetSecret(context.Background(), "secret/data/anything")
	assert.Nil(t, secret)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

func TestGetSecret_Forbidden(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"errors":["permission denied"]}`))
	}))
	defer server.Close()

	client := newTestVaultClient(t, server)
	secret, err := client.GetSecret(context.Background(), "secret/data/restricted")

	assert.Nil(t, secret)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
	assert.Contains(t, err.Error(), "permission denied")
}

func TestGetSecret_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"errors":[]}`))
	}))
	defer server.Close()

	client := newTestVaultClient(t, server)
	secret, err := client.GetSecret(context.Background(), "secret/data/nonexistent")

	assert.Nil(t, secret)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

// ---------------------------------------------------------------------------
// GetSecretMetadata
// ---------------------------------------------------------------------------

func TestGetSecretMetadata_Success(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	v3Time := now.Add(-2 * time.Hour)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		// The client must rewrite /data/ to /metadata/ in the path.
		assert.Equal(t, "/v1/secret/metadata/myapp/db", r.URL.Path,
			"metadata request must replace /data/ with /metadata/ in the path")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(vaultMetadataResponse(
			3,
			map[string]VaultVersionInfo{
				"1": {Version: 1, CreatedTime: now.Add(-24 * time.Hour)},
				"2": {Version: 2, CreatedTime: now.Add(-12 * time.Hour)},
				"3": {Version: 3, CreatedTime: v3Time},
			},
			now.Add(-24*time.Hour),
			now,
		))
	}))
	defer server.Close()

	client := newTestVaultClient(t, server)
	meta, err := client.GetSecretMetadata(context.Background(), "secret/data/myapp/db")

	require.NoError(t, err)
	require.NotNil(t, meta)
	assert.Equal(t, "secret/data/myapp/db", meta.Path)
	assert.Equal(t, ProviderVault, meta.Provider)
	assert.Equal(t, 3, meta.Version)
	require.NotNil(t, meta.LastRotated, "LastRotated must be set from the current version info")
	assert.Equal(t, v3Time, meta.LastRotated.Truncate(time.Second))
}

func TestGetSecretMetadata_DisabledClient(t *testing.T) {
	client := NewVaultClient(nil)
	meta, err := client.GetSecretMetadata(context.Background(), "secret/data/anything")
	assert.Nil(t, meta)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

func TestGetSecretMetadata_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"errors":["internal error"]}`))
	}))
	defer server.Close()

	client := newTestVaultClient(t, server)
	meta, err := client.GetSecretMetadata(context.Background(), "secret/data/myapp/db")

	assert.Nil(t, meta)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

// ---------------------------------------------------------------------------
// ValidateConnection
// ---------------------------------------------------------------------------

func TestValidateConnection_Healthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/sys/health", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"initialized":true,"sealed":false,"standby":false}`))
	}))
	defer server.Close()

	client := newTestVaultClient(t, server)
	err := client.ValidateConnection(context.Background())
	assert.NoError(t, err)
}

func TestValidateConnection_Standby(t *testing.T) {
	// Vault returns 429 for standby nodes. The client should still treat
	// this as reachable (not an error), since status < 500.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests) // 429 = standby
		w.Write([]byte(`{"initialized":true,"sealed":false,"standby":true}`))
	}))
	defer server.Close()

	client := newTestVaultClient(t, server)
	err := client.ValidateConnection(context.Background())
	assert.NoError(t, err, "standby (429) should not be treated as unhealthy")
}

func TestValidateConnection_Sealed(t *testing.T) {
	// Vault returns 503 when sealed. The current implementation treats
	// status >= 500 as unhealthy.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable) // 503 = sealed
		w.Write([]byte(`{"initialized":true,"sealed":true,"standby":false}`))
	}))
	defer server.Close()

	client := newTestVaultClient(t, server)
	err := client.ValidateConnection(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unhealthy")
	assert.Contains(t, err.Error(), "503")
}

func TestValidateConnection_DisabledClient(t *testing.T) {
	client := NewVaultClient(nil)
	err := client.ValidateConnection(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

func TestValidateConnection_Unreachable(t *testing.T) {
	// Point the client at an address that will immediately refuse connection.
	client := &VaultClient{
		address:    "http://127.0.0.1:1", // nothing listens here
		token:      "test",
		httpClient: &http.Client{Timeout: 1 * time.Second},
		enabled:    true,
	}

	err := client.ValidateConnection(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect")
}

// ---------------------------------------------------------------------------
// WatchSecret
// ---------------------------------------------------------------------------

func TestWatchSecret_DetectsVersionChange(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		// First poll: version 2 (baseline). Second poll: version 3 (change).
		version := 2
		if callCount > 1 {
			version = 3
		}

		w.Write(vaultMetadataResponse(
			version,
			map[string]VaultVersionInfo{
				"2": {Version: 2, CreatedTime: now.Add(-1 * time.Hour)},
				"3": {Version: 3, CreatedTime: now},
			},
			now.Add(-24*time.Hour),
			now,
		))
	}))
	defer server.Close()

	client := newTestVaultClient(t, server)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	events := client.WatchSecret(ctx, "secret/data/myapp/db", 50*time.Millisecond)

	select {
	case event := <-events:
		require.NotNil(t, event)
		assert.Equal(t, 2, event.OldVersion)
		assert.Equal(t, 3, event.NewVersion)
		assert.Equal(t, ProviderVault, event.Provider)
		assert.Equal(t, RotationPending, event.Status)
		assert.Equal(t, "watcher", event.TriggeredBy)
		assert.Equal(t, "secret/data/myapp/db", event.SecretPath)
	case <-ctx.Done():
		t.Fatal("timed out waiting for version change event")
	}
}

func TestWatchSecret_DisabledClientClosesChannel(t *testing.T) {
	client := NewVaultClient(nil)
	events := client.WatchSecret(context.Background(), "secret/data/any", time.Second)

	// The channel should be closed immediately for a disabled client.
	_, open := <-events
	assert.False(t, open, "watch channel must be closed immediately for a disabled client")
}

func TestWatchSecret_CancelledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := time.Now().UTC()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(vaultMetadataResponse(
			1,
			map[string]VaultVersionInfo{
				"1": {Version: 1, CreatedTime: now},
			},
			now, now,
		))
	}))
	defer server.Close()

	client := newTestVaultClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())

	events := client.WatchSecret(ctx, "secret/data/myapp/db", 50*time.Millisecond)

	// Allow one poll cycle to run, then cancel.
	time.Sleep(100 * time.Millisecond)
	cancel()

	// The channel must eventually close after context cancellation.
	timeout := time.After(2 * time.Second)
	for {
		select {
		case _, open := <-events:
			if !open {
				return // success - channel closed
			}
		case <-timeout:
			t.Fatal("watch channel was not closed after context cancellation")
		}
	}
}

// ---------------------------------------------------------------------------
// Types sanity checks
// ---------------------------------------------------------------------------

func TestSecretProvider_Constants(t *testing.T) {
	assert.Equal(t, SecretProvider("vault"), ProviderVault)
	assert.Equal(t, SecretProvider("1password"), ProviderOnePassword)
	assert.Equal(t, SecretProvider("kubernetes"), ProviderKubernetes)
}

func TestRotationStatus_Constants(t *testing.T) {
	statuses := []RotationStatus{
		RotationPending,
		RotationInProgress,
		RotationCompleted,
		RotationFailed,
		RotationRolledBack,
	}
	for _, s := range statuses {
		assert.NotEmpty(t, string(s), "rotation status constant must not be empty")
	}
	// Verify the full set of expected values.
	assert.Equal(t, RotationStatus("pending"), RotationPending)
	assert.Equal(t, RotationStatus("in_progress"), RotationInProgress)
	assert.Equal(t, RotationStatus("completed"), RotationCompleted)
	assert.Equal(t, RotationStatus("failed"), RotationFailed)
	assert.Equal(t, RotationStatus("rolled_back"), RotationRolledBack)
}
