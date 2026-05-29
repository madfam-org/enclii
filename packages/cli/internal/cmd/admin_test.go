package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func newTestAdminCommand(t *testing.T) *cobra.Command {
	t.Helper()
	cfg := &config.Config{APIEndpoint: "https://api.test.dev", APIToken: "test-token"}
	cmd := NewAdminCommand(cfg)
	require.NotNil(t, cmd)
	return cmd
}

func TestNewAdminCommand_Subcommands(t *testing.T) {
	cmd := newTestAdminCommand(t)
	assert.Equal(t, "admin", cmd.Use)

	expected := []string{"fleet", "topology", "clusters", "drift", "propagation", "governance", "costs", "vclusters", "tenants", "status", "provision", "ga-verify"}
	for _, name := range expected {
		assert.NotNil(t, findSubcommand(cmd, name), "missing top-level admin subcommand: %s", name)
	}
	assert.Len(t, cmd.Commands(), len(expected))
}

func TestAdminFleet_Subcommands(t *testing.T) {
	cmd := newTestAdminCommand(t)
	fleet := findSubcommand(cmd, "fleet")
	require.NotNil(t, fleet)
	for _, sub := range []string{"list", "get", "register", "firmware", "partition", "wipe", "power"} {
		assert.NotNil(t, findSubcommand(fleet, sub), "missing fleet subcommand: %s", sub)
	}

	register := findSubcommand(fleet, "register")
	require.NotNil(t, register)
	assert.NotNil(t, register.Flags().Lookup("hostname"))
	assert.NotNil(t, register.Flags().Lookup("region"))
	assert.NotNil(t, register.Flags().Lookup("role"))
	assert.NotNil(t, register.Flags().Lookup("force"))

	firmware := findSubcommand(fleet, "firmware")
	require.NotNil(t, firmware)
	assert.NotNil(t, firmware.Flags().Lookup("version"))
	assert.NotNil(t, firmware.Flags().Lookup("force"))

	partition := findSubcommand(fleet, "partition")
	require.NotNil(t, partition)
	assert.NotNil(t, partition.Flags().Lookup("layout"))

	power := findSubcommand(fleet, "power")
	require.NotNil(t, power)
	assert.NotNil(t, power.Flags().Lookup("state"))
}

func TestAdminTopology_Exists(t *testing.T) {
	cmd := newTestAdminCommand(t)
	topo := findSubcommand(cmd, "topology")
	require.NotNil(t, topo)
	assert.Equal(t, "topology", topo.Use)
}

func TestAdminStatus_Subcommands(t *testing.T) {
	cmd := newTestAdminCommand(t)
	status := findSubcommand(cmd, "status")
	require.NotNil(t, status)
	regenerate := findSubcommand(status, "regenerate")
	require.NotNil(t, regenerate)
	assert.NotNil(t, regenerate.Flags().Lookup("force"))
}

func TestAdminStatusRegenerate_RequiresForce(t *testing.T) {
	cmd := newTestAdminCommand(t)
	cmd.SetArgs([]string{"status", "regenerate"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--force")
}

func TestAdminStatusRegenerate_CallsEndpoint(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"no_changes","total_count":67}`))
	}))
	defer srv.Close()

	cfg := &config.Config{APIEndpoint: srv.URL, APIToken: "test-token"}
	cmd := NewAdminCommand(cfg)
	cmd.SetArgs([]string{"status", "regenerate", "--force"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	require.NoError(t, cmd.Execute())
	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/v1/admin/status/regenerate", gotPath)
	assert.Equal(t, "Bearer test-token", gotAuth)
}

func TestAdminClusters_Subcommands(t *testing.T) {
	cmd := newTestAdminCommand(t)
	clusters := findSubcommand(cmd, "clusters")
	require.NotNil(t, clusters)
	for _, sub := range []string{"list", "get", "register", "update", "deregister"} {
		assert.NotNil(t, findSubcommand(clusters, sub), "missing clusters subcommand: %s", sub)
	}

	register := findSubcommand(clusters, "register")
	require.NotNil(t, register)
	assert.NotNil(t, register.Flags().Lookup("name"))
	assert.NotNil(t, register.Flags().Lookup("kubeconfig-file"))
	assert.NotNil(t, register.Flags().Lookup("force"))

	dereg := findSubcommand(clusters, "deregister")
	require.NotNil(t, dereg)
	assert.NotNil(t, dereg.Flags().Lookup("force"))
}

func TestAdminDrift_Subcommands(t *testing.T) {
	cmd := newTestAdminCommand(t)
	drift := findSubcommand(cmd, "drift")
	require.NotNil(t, drift)
	for _, sub := range []string{"list", "get", "resolve"} {
		assert.NotNil(t, findSubcommand(drift, sub), "missing drift subcommand: %s", sub)
	}
	list := findSubcommand(drift, "list")
	require.NotNil(t, list)
	assert.NotNil(t, list.Flags().Lookup("status"))
	assert.NotNil(t, list.Flags().Lookup("json"))

	resolve := findSubcommand(drift, "resolve")
	require.NotNil(t, resolve)
	assert.NotNil(t, resolve.Flags().Lookup("reason"))
	assert.NotNil(t, resolve.Flags().Lookup("force"))
}

func TestAdminPropagation_Subcommands(t *testing.T) {
	cmd := newTestAdminCommand(t)
	prop := findSubcommand(cmd, "propagation")
	require.NotNil(t, prop)
	for _, sub := range []string{"list", "get", "create", "delete"} {
		assert.NotNil(t, findSubcommand(prop, sub), "missing propagation subcommand: %s", sub)
	}
	create := findSubcommand(prop, "create")
	require.NotNil(t, create)
	assert.NotNil(t, create.Flags().Lookup("name"))
	assert.NotNil(t, create.Flags().Lookup("source-cluster"))
	assert.NotNil(t, create.Flags().Lookup("target-clusters"))
	assert.NotNil(t, create.Flags().Lookup("resource-kind"))
	assert.NotNil(t, create.Flags().Lookup("force"))
}

func TestAdminGovernance_Subcommands(t *testing.T) {
	cmd := newTestAdminCommand(t)
	gov := findSubcommand(cmd, "governance")
	require.NotNil(t, gov)
	for _, sub := range []string{"list-resources", "get-resource", "create-resource", "update-policy", "delete-resource"} {
		assert.NotNil(t, findSubcommand(gov, sub), "missing governance subcommand: %s", sub)
	}
	create := findSubcommand(gov, "create-resource")
	require.NotNil(t, create)
	assert.NotNil(t, create.Flags().Lookup("kind"))
	assert.NotNil(t, create.Flags().Lookup("name"))
	assert.NotNil(t, create.Flags().Lookup("owner"))
	assert.NotNil(t, create.Flags().Lookup("force"))

	update := findSubcommand(gov, "update-policy")
	require.NotNil(t, update)
	assert.NotNil(t, update.Flags().Lookup("policy-file"))
	assert.NotNil(t, update.Flags().Lookup("force"))
}

func TestAdminCosts_Subcommands(t *testing.T) {
	cmd := newTestAdminCommand(t)
	costs := findSubcommand(cmd, "costs")
	require.NotNil(t, costs)
	for _, sub := range []string{"allocations", "summary", "allocate"} {
		assert.NotNil(t, findSubcommand(costs, sub), "missing costs subcommand: %s", sub)
	}

	allocate := findSubcommand(costs, "allocate")
	require.NotNil(t, allocate)
	assert.NotNil(t, allocate.Flags().Lookup("resource"))
	assert.NotNil(t, allocate.Flags().Lookup("tenant"))
	assert.NotNil(t, allocate.Flags().Lookup("amount-cents"))
	assert.NotNil(t, allocate.Flags().Lookup("force"))

	allocations := findSubcommand(costs, "allocations")
	require.NotNil(t, allocations)
	assert.NotNil(t, allocations.Flags().Lookup("from"))
	assert.NotNil(t, allocations.Flags().Lookup("to"))
}

func TestAdminVClusters_Subcommands(t *testing.T) {
	cmd := newTestAdminCommand(t)
	vc := findSubcommand(cmd, "vclusters")
	require.NotNil(t, vc)
	for _, sub := range []string{"list", "get", "provision", "teardown", "kubeconfig"} {
		assert.NotNil(t, findSubcommand(vc, sub), "missing vclusters subcommand: %s", sub)
	}

	prov := findSubcommand(vc, "provision")
	require.NotNil(t, prov)
	assert.NotNil(t, prov.Flags().Lookup("name"))
	assert.NotNil(t, prov.Flags().Lookup("node"))
	assert.NotNil(t, prov.Flags().Lookup("force"))

	kc := findSubcommand(vc, "kubeconfig")
	require.NotNil(t, kc)
	assert.NotNil(t, kc.Flags().Lookup("out"))
}

func TestAdminTenants_Subcommands(t *testing.T) {
	cmd := newTestAdminCommand(t)
	tenants := findSubcommand(cmd, "tenants")
	require.NotNil(t, tenants)
	for _, sub := range []string{"list", "active", "enter", "exit"} {
		assert.NotNil(t, findSubcommand(tenants, sub), "missing tenants subcommand: %s", sub)
	}

	enter := findSubcommand(tenants, "enter")
	require.NotNil(t, enter)
	assert.NotNil(t, enter.Flags().Lookup("reason"))
	assert.NotNil(t, enter.Flags().Lookup("duration-seconds"))

	for _, name := range []string{"list", "active", "enter", "exit"} {
		sub := findSubcommand(tenants, name)
		assert.NotNil(t, sub.Flags().Lookup("json"), "tenants %s must support --json", name)
	}
}

func TestAdminCommand_RegisteredOnRoot(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev", APIToken: "test-token"}
	root := NewRootCommand(cfg)
	require.NotNil(t, root)

	admin := findSubcommand(root, "admin")
	require.NotNil(t, admin, "admin command must be registered on root")
}
