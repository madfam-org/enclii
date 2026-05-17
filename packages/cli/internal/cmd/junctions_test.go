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

func TestNewJunctionsCommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewJunctionsCommand(cfg)
	require.NotNil(t, cmd)

	assert.Equal(t, "junctions", cmd.Use)
	assert.Equal(t, []string{"junction", "routes"}, cmd.Aliases)
}

func TestJunctionsCommand_HasExpectedSubcommands(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewJunctionsCommand(cfg)

	subNames := make([]string, 0, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		subNames = append(subNames, sub.Name())
	}

	expectedSubs := []string{"list", "add", "get", "delete"}
	for _, name := range expectedSubs {
		assert.Contains(t, subNames, name, "missing subcommand: %s", name)
	}
	assert.Len(t, cmd.Commands(), 4, "expected exactly 4 subcommands")
}

func TestJunctionsListSubcommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewJunctionsCommand(cfg)
	listCmd := findSubcommand(cmd, "list")
	require.NotNil(t, listCmd, "list subcommand should exist")

	projectFlag := listCmd.Flags().Lookup("project")
	require.NotNil(t, projectFlag)
	assert.Equal(t, "p", projectFlag.Shorthand)
	assert.Equal(t, "", projectFlag.DefValue)

	jsonFlag := listCmd.Flags().Lookup("json")
	require.NotNil(t, jsonFlag)
	assert.Equal(t, "false", jsonFlag.DefValue)
}

func TestJunctionsAddSubcommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewJunctionsCommand(cfg)
	addCmd := findSubcommand(cmd, "add")
	require.NotNil(t, addCmd, "add subcommand should exist")

	// Requires exactly 1 arg (domain)
	assert.NotNil(t, addCmd.Args, "add subcommand should have args validation")

	// --project required, shorthand "p"
	projectFlag := addCmd.Flags().Lookup("project")
	require.NotNil(t, projectFlag)
	assert.Equal(t, "p", projectFlag.Shorthand)

	// --service-id required, no shorthand
	serviceIDFlag := addCmd.Flags().Lookup("service-id")
	require.NotNil(t, serviceIDFlag)
	assert.Equal(t, "", serviceIDFlag.Shorthand)

	// --path default "/"
	pathFlag := addCmd.Flags().Lookup("path")
	require.NotNil(t, pathFlag)
	assert.Equal(t, "/", pathFlag.DefValue)

	// --protocol default "https"
	protocolFlag := addCmd.Flags().Lookup("protocol")
	require.NotNil(t, protocolFlag)
	assert.Equal(t, "https", protocolFlag.DefValue)
}

func TestJunctionsGetSubcommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewJunctionsCommand(cfg)
	getCmd := findSubcommand(cmd, "get")
	require.NotNil(t, getCmd, "get subcommand should exist")

	// Requires exactly 1 arg
	assert.NotNil(t, getCmd.Args, "get subcommand should have args validation")
}

func TestJunctionsDeleteSubcommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewJunctionsCommand(cfg)
	deleteCmd := findSubcommand(cmd, "delete")
	require.NotNil(t, deleteCmd, "delete subcommand should exist")

	// Requires exactly 1 arg
	assert.NotNil(t, deleteCmd.Args, "delete subcommand should have args validation")

	// Aliases
	assert.Equal(t, []string{"rm", "remove"}, deleteCmd.Aliases)

	// --force (no shorthand)
	forceFlag := deleteCmd.Flags().Lookup("force")
	require.NotNil(t, forceFlag)
	assert.Equal(t, "false", forceFlag.DefValue)
	assert.Equal(t, "", forceFlag.Shorthand)
}

func TestJunctionsRequest_SetsHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer my-api-token", r.Header.Get("Authorization"))
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/projects/test/junctions", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]map[string]string{})
	}))
	defer server.Close()

	cfg := &config.Config{
		APIEndpoint: server.URL,
		APIToken:    "my-api-token",
	}

	resp, err := junctionsRequest(cfg, http.MethodGet, "/v1/projects/test/junctions", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestJunctionsRequest_SendsBodyAsJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)

		var body map[string]string
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, "api.example.com", body["domain"])
		assert.Equal(t, "/api", body["path"])

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(body)
	}))
	defer server.Close()

	cfg := &config.Config{
		APIEndpoint: server.URL,
		APIToken:    "test-token",
	}

	payload := map[string]string{
		"domain": "api.example.com",
		"path":   "/api",
	}

	resp, err := junctionsRequest(cfg, http.MethodPost, "/v1/projects/test/junctions", payload)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
}

func TestJunctionsDecodeOrError_ReturnsErrorForHTTP4xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"access denied"}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		APIEndpoint: server.URL,
		APIToken:    "test-token",
	}

	resp, err := junctionsRequest(cfg, http.MethodGet, "/v1/junctions/abc", nil)
	require.NoError(t, err)

	var target map[string]string
	err = junctionsDecodeOrError(resp, &target)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error (HTTP 403)")
	assert.Contains(t, err.Error(), "access denied")
}

func TestJunctionsDecodeOrError_DecodesSuccessResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"domain": "app.example.com"})
	}))
	defer server.Close()

	cfg := &config.Config{
		APIEndpoint: server.URL,
		APIToken:    "test-token",
	}

	resp, err := junctionsRequest(cfg, http.MethodGet, "/v1/junctions/123", nil)
	require.NoError(t, err)

	var target map[string]string
	err = junctionsDecodeOrError(resp, &target)
	require.NoError(t, err)
	assert.Equal(t, "app.example.com", target["domain"])
}

func TestRunJunctionsList_DecodesWrappedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/projects/madfam-site/junctions", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"junctions": []map[string]interface{}{
				{
					"id":       "00000000-0000-0000-0000-000000000001",
					"domain":   "dash.madfam.io",
					"path":     "/",
					"protocol": "https",
				},
			},
			"total": 1,
		})
	}))
	defer server.Close()

	cfg := &config.Config{
		APIEndpoint: server.URL,
		APIToken:    "test-token",
	}

	require.NoError(t, runJunctionsList(cfg, "madfam-site", false))
}

func TestRunJunctionsList_JSON_DecodesWrappedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/projects/phynd-crm/junctions", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"junctions": []map[string]interface{}{
				{
					"id":       "00000000-0000-0000-0000-000000000001",
					"domain":   "crm.phyne.app",
					"path":     "/",
					"protocol": "https",
				},
			},
			"total": 1,
		})
	}))
	defer server.Close()

	cfg := &config.Config{
		APIEndpoint: server.URL,
		APIToken:    "test-token",
	}

	require.NoError(t, runJunctionsList(cfg, "phynd-crm", true))
}

func TestRunJunctionsAdd_DecodesWrappedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/projects/madfam-site/junctions", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"junction": map[string]interface{}{
				"id":         "00000000-0000-0000-0000-000000000001",
				"domain":     "dash.madfam.io",
				"path":       "/",
				"protocol":   "https",
				"service_id": "00000000-0000-0000-0000-000000000002",
				"project_id": "00000000-0000-0000-0000-000000000003",
			},
			"message": "Junction created successfully",
		})
	}))
	defer server.Close()

	cfg := &config.Config{
		APIEndpoint: server.URL,
		APIToken:    "test-token",
	}

	require.NoError(t, runJunctionsAdd(cfg, "dash.madfam.io", "madfam-site", "00000000-0000-0000-0000-000000000002", "/", "https"))
}
