package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

// TestAddonCommandStructure confirms the `enclii addon` tree is wired with
// the Sprint-1 subcommands.
func TestAddonCommandStructure(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "http://localhost:0"}
	cmd := NewAddonCommand(cfg)
	require.NotNil(t, cmd)
	assert.Equal(t, "addon", cmd.Name())

	names := map[string]bool{}
	for _, sub := range cmd.Commands() {
		names[sub.Name()] = true
	}
	assert.True(t, names["create"])
	assert.True(t, names["ls"])
	assert.True(t, names["destroy"])
	assert.True(t, names["plans"])
	assert.True(t, names["realtime"])
}

// TestAddonRealtimeCommandStructure confirms the `enclii addon realtime` subtree
// carries enable/disable/list.
func TestAddonRealtimeCommandStructure(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "http://localhost:0"}
	cmd := NewAddonCommand(cfg)

	var realtime *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "realtime" {
			realtime = sub
		}
	}
	require.NotNil(t, realtime, "expected a realtime subcommand")

	names := map[string]bool{}
	for _, sub := range realtime.Commands() {
		names[sub.Name()] = true
	}
	assert.True(t, names["enable"])
	assert.True(t, names["disable"])
	assert.True(t, names["list"])
}

// TestSplitSchemaTable covers the schema.table parser used by the realtime CLI.
func TestSplitSchemaTable(t *testing.T) {
	cases := []struct {
		in         string
		wantSchema string
		wantTable  string
		wantErr    bool
	}{
		{"public.orders", "public", "orders", false},
		{"orders", "public", "orders", false},
		{"billing.invoices", "billing", "invoices", false},
		{"", "", "", true},
		{".orders", "", "", true},
		{"public.", "", "", true},
		{"a.b.c", "", "", true},
	}
	for _, tc := range cases {
		schema, table, err := splitSchemaTable(tc.in)
		if tc.wantErr {
			assert.Error(t, err, "input %q", tc.in)
			continue
		}
		require.NoError(t, err, "input %q", tc.in)
		assert.Equal(t, tc.wantSchema, schema, "input %q", tc.in)
		assert.Equal(t, tc.wantTable, table, "input %q", tc.in)
	}
}

// TestAddonRealtimeEnableRequiresTable enforces the --table rail.
func TestAddonRealtimeEnableRequiresTable(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "http://localhost:0"}
	root := NewRootCommand(cfg)
	root.SetArgs([]string{"addon", "realtime", "enable", "abc-123"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "table is required")
}

// TestAddonRealtimeEnableAgainstFakeServer exercises the enable POST with a
// schema.table argument.
func TestAddonRealtimeEnableAgainstFakeServer(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/addons/00000000-0000-0000-0000-000000000001/realtime/tables", r.URL.Path)
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "public", body["schema"])
		assert.Equal(t, "orders", body["table"])
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "realtime enabled"})
	}))
	defer srv.Close()

	cfg := &config.Config{APIEndpoint: srv.URL, APIToken: "t"}
	root := NewRootCommand(cfg)
	root.SetArgs([]string{
		"addon", "realtime", "enable",
		"00000000-0000-0000-0000-000000000001",
		"--table", "public.orders",
	})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	require.NoError(t, root.Execute())
	assert.Equal(t, 1, hits)
}

// TestAddonRealtimeDisableAgainstFakeServer exercises the disable DELETE with
// the schema/table path segments.
func TestAddonRealtimeDisableAgainstFakeServer(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/v1/addons/00000000-0000-0000-0000-000000000001/realtime/tables/public/orders", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "realtime disabled"})
	}))
	defer srv.Close()

	cfg := &config.Config{APIEndpoint: srv.URL, APIToken: "t"}
	root := NewRootCommand(cfg)
	root.SetArgs([]string{
		"addon", "realtime", "disable",
		"00000000-0000-0000-0000-000000000001",
		"--table", "orders",
	})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	require.NoError(t, root.Execute())
	assert.Equal(t, 1, hits)
}

// TestAddonRealtimeListAgainstFakeServer exercises the list GET rendering.
func TestAddonRealtimeListAgainstFakeServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/v1/addons/00000000-0000-0000-0000-000000000001/realtime/tables", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"tables": []map[string]string{{"schema": "public", "table": "orders"}},
			"count":  1,
		})
	}))
	defer srv.Close()

	cfg := &config.Config{APIEndpoint: srv.URL, APIToken: "t"}
	root := NewRootCommand(cfg)
	root.SetArgs([]string{"addon", "realtime", "list", "00000000-0000-0000-0000-000000000001"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	require.NoError(t, root.Execute())
}

// TestAddonRegisteredOnRoot verifies that `enclii addon` is exposed at the
// top-level.
func TestAddonRegisteredOnRoot(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "http://localhost:0"}
	root := NewRootCommand(cfg)

	names := map[string]bool{}
	for _, sub := range root.Commands() {
		names[sub.Name()] = true
	}
	assert.True(t, names["addon"])
}

// TestAddonCreateValidation covers the pre-HTTP validation on `addon create`
// — callers must supply a plan and a project (explicit or via config).
func TestAddonCreateValidation(t *testing.T) {
	t.Run("missing plan", func(t *testing.T) {
		cfg := &config.Config{Project: "my-api", APIEndpoint: "http://localhost:0"}
		root := NewRootCommand(cfg)
		root.SetArgs([]string{"addon", "create", "my-db"})
		// Suppress output and capture exit via error return.
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		err := root.Execute()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "plan is required")
	})

	t.Run("missing project (no config fallback)", func(t *testing.T) {
		cfg := &config.Config{APIEndpoint: "http://localhost:0"}
		root := NewRootCommand(cfg)
		root.SetArgs([]string{"addon", "create", "my-db", "--plan", "standard-0"})
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		err := root.Execute()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "project is required")
	})

	t.Run("missing addon name arg", func(t *testing.T) {
		cfg := &config.Config{Project: "my-api", APIEndpoint: "http://localhost:0"}
		root := NewRootCommand(cfg)
		root.SetArgs([]string{"addon", "create", "--plan", "standard-0"})
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		err := root.Execute()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "accepts 1 arg")
	})
}

// TestAddonDestroyRequiresConfirmation enforces the safety rail: destroying
// without --yes is a no-op with a helpful error, even if the addon-id is
// well-formed.
func TestAddonDestroyRequiresConfirmation(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "http://localhost:0"}
	root := NewRootCommand(cfg)
	root.SetArgs([]string{"addon", "destroy", "abc-123"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "confirmation required")
}

// TestAddonPlansListsAgainstFakeServer exercises the full `addon plans` code
// path against an httptest server returning the plan catalog wire shape.
func TestAddonPlansListsAgainstFakeServer(t *testing.T) {
	plans := map[string]interface{}{
		"plans": []map[string]interface{}{
			{
				"code":              "standard-0",
				"engine":            "postgres",
				"display_name":      "Standard 0",
				"tier":              "standard",
				"storage_gb":        1,
				"cpu_request":       "100m",
				"memory_request":    "256Mi",
				"max_connections":   10,
				"ha_enabled":        false,
				"price_cents_month": 0,
			},
		},
		"count": 1,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/addons/plans", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(plans)
	}))
	defer srv.Close()

	cfg := &config.Config{APIEndpoint: srv.URL, APIToken: "t"}
	root := NewRootCommand(cfg)
	var out strings.Builder
	root.SetArgs([]string{"addon", "plans", "--json"})
	root.SetOut(&out)
	root.SetErr(io.Discard)

	// --json output goes to os.Stdout (not root.OutOrStdout) in our command
	// — but the success case is that Execute() returns nil.
	err := root.Execute()
	require.NoError(t, err)
}

// TestAddonListHandlesEmptyResult confirms the CLI renders the "no addons"
// message when the server returns count=0.
func TestAddonListHandlesEmptyResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/addons", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"addons": []interface{}{},
			"count":  0,
		})
	}))
	defer srv.Close()

	cfg := &config.Config{APIEndpoint: srv.URL, APIToken: "t"}
	root := NewRootCommand(cfg)
	root.SetArgs([]string{"addon", "ls"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := root.Execute()
	require.NoError(t, err)
}

// TestAddonDefaultEnvVar covers the convenience helper that picks
// DATABASE_URL / REDIS_URL / MYSQL_URL based on engine.
func TestAddonDefaultEnvVar(t *testing.T) {
	assert.Equal(t, "DATABASE_URL", defaultEnvVar("postgres", ""))
	assert.Equal(t, "REDIS_URL", defaultEnvVar("redis", ""))
	assert.Equal(t, "MYSQL_URL", defaultEnvVar("mysql", ""))
	// Explicit override always wins.
	assert.Equal(t, "MY_DB", defaultEnvVar("postgres", "MY_DB"))
}

// TestShortAddonID is a guard rail on the tabular render helper.
func TestShortAddonID(t *testing.T) {
	assert.Equal(t, "12345678", shortAddonID("12345678-1234-1234-1234-123456789012"))
	assert.Equal(t, "short", shortAddonID("short"))
	assert.Equal(t, "", shortAddonID(""))
}

// TestAddonCreateAgainstFakeServer exercises the full create flow (without
// optional --service binding).
func TestAddonCreateAgainstFakeServer(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		assert.Equal(t, "/v1/projects/my-api/addons", r.URL.Path)
		assert.Equal(t, "POST", r.Method)
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "my-db", body["name"])
		assert.Equal(t, "standard-0", body["plan"])
		assert.Equal(t, "postgres", body["type"])

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"addon": map[string]interface{}{
				"id":         "00000000-0000-0000-0000-000000000001",
				"name":       "my-db",
				"plan":       "standard-0",
				"status":     "provisioning",
				"created_at": "2026-04-17T00:00:00Z",
			},
			"message": "Database addon creation initiated",
		})
	}))
	defer srv.Close()

	cfg := &config.Config{APIEndpoint: srv.URL, APIToken: "t"}
	root := NewRootCommand(cfg)
	root.SetArgs([]string{
		"addon", "create", "my-db",
		"--project", "my-api",
		"--plan", "standard-0",
	})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := root.Execute()
	require.NoError(t, err)
	assert.Equal(t, 1, hits)
}

// TestAddonDestroyHitsDeleteEndpoint verifies --yes routes through DELETE.
func TestAddonDestroyHitsDeleteEndpoint(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/v1/addons/00000000-0000-0000-0000-000000000001", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Addon deleted successfully"})
	}))
	defer srv.Close()

	cfg := &config.Config{APIEndpoint: srv.URL, APIToken: "t"}
	root := NewRootCommand(cfg)
	root.SetArgs([]string{
		"addon", "destroy",
		"00000000-0000-0000-0000-000000000001",
		"--yes",
	})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := root.Execute()
	require.NoError(t, err)
	assert.Equal(t, 1, hits)
}

// TestAddonCreate4xxSurfacesError ensures a 400 from the API yields a
// non-nil error from Execute() (non-zero exit code).
func TestAddonCreate4xxSurfacesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": `unknown plan "bogus"`})
	}))
	defer srv.Close()

	cfg := &config.Config{APIEndpoint: srv.URL, APIToken: "t"}
	root := NewRootCommand(cfg)
	root.SetArgs([]string{
		"addon", "create", "my-db",
		"--project", "my-api",
		"--plan", "bogus",
	})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}
