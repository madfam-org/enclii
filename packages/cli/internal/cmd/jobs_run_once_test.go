package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

const runOnceTestServiceID = "550e8400-e29b-41d4-a716-446655440001"

// runOnceServer records the request body and answers with createdJSON.
func runOnceServer(t *testing.T, createdJSON string, body *map[string]interface{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(body))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(createdJSON))
	}))
}

func TestJobsRunOnceNetworkIdentityFlag(t *testing.T) {
	cmd := NewJobsCommand(&config.Config{APIEndpoint: "https://api.test.dev", APIToken: "t"})
	runOnce := findSubcommand(cmd, "run-once")
	require.NotNil(t, runOnce)
	flag := runOnce.Flags().Lookup("network-identity")
	require.NotNil(t, flag)
	assert.Equal(t, "", flag.DefValue, "the default must stay: no network identity")
}

func TestRunJobsRunOnce_NetworkIdentity(t *testing.T) {
	t.Run("service is sent and confirmed", func(t *testing.T) {
		var body map[string]interface{}
		server := runOnceServer(t, `{"one_off_job": {"id": "660e8400-e29b-41d4-a716-446655440099", "name": "grant", "command": "node x.js -", "status": "pending", "network_identity": "service"}}`, &body)
		defer server.Close()

		err := runJobsRunOnce(&config.Config{APIEndpoint: server.URL, APIToken: "t"}, "dhanam", "grant", "node x.js -", runOnceTestServiceID, "", 600, "service")
		require.NoError(t, err)
		assert.Equal(t, "service", body["network_identity"])
	})

	t.Run("omitted by default", func(t *testing.T) {
		var body map[string]interface{}
		server := runOnceServer(t, `{"one_off_job": {"id": "660e8400-e29b-41d4-a716-446655440099", "name": "m", "command": "c", "status": "pending"}}`, &body)
		defer server.Close()

		err := runJobsRunOnce(&config.Config{APIEndpoint: server.URL, APIToken: "t"}, "dhanam", "m", "c", runOnceTestServiceID, "", 600, "")
		require.NoError(t, err)
		_, has := body["network_identity"]
		assert.False(t, has)
	})

	t.Run("a server that drops the field is reported, not trusted", func(t *testing.T) {
		var body map[string]interface{}
		server := runOnceServer(t, `{"one_off_job": {"id": "660e8400-e29b-41d4-a716-446655440099", "name": "grant", "command": "c", "status": "pending"}}`, &body)
		defer server.Close()

		err := runJobsRunOnce(&config.Config{APIEndpoint: server.URL, APIToken: "t"}, "dhanam", "grant", "c", runOnceTestServiceID, "", 600, "service")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "without the service's network identity")
	})

	t.Run("an unknown value is refused before any request", func(t *testing.T) {
		err := runJobsRunOnce(&config.Config{APIEndpoint: "http://127.0.0.1:1", APIToken: "t"}, "dhanam", "x", "c", runOnceTestServiceID, "", 600, "namespace")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid --network-identity")
	})
}
