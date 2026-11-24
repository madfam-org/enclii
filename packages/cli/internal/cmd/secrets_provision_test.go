package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/ecosystemoidc"
)

// provisionOIDC runs `secrets provision oidc` against a server that fails the
// test on any request, so a passing case proves the run stayed local.
func provisionOIDC(t *testing.T, args ...string) error {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected network call: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	t.Setenv("ENCLII_JANUA_API_URL", server.URL)
	t.Setenv("JANUA_INTERNAL_API_KEY", "")

	cfg := &config.Config{APIEndpoint: server.URL, APIToken: "fixture"}
	root := newSecretsProvisionCommand(cfg)
	root.SetArgs(append([]string{"oidc"}, args...))
	root.SilenceUsage = true
	root.SilenceErrors = true
	return root.Execute()
}

func TestProvisionOIDCGraceHoursOutOfRangeRefusedLocally(t *testing.T) {
	for _, bad := range []string{"-1", "169"} {
		err := provisionOIDC(t, "--platform", "zavlo-cfdi-emitter", "--reason", "test", "--grace-hours", bad)
		require.Error(t, err, "--grace-hours %s accepted", bad)
		assert.Contains(t, err.Error(), "--grace-hours")
	}
}

func TestProvisionOIDCGraceHoursDryRunStaysLocal(t *testing.T) {
	for _, grace := range []string{"0", "168"} {
		require.NoError(t, provisionOIDC(t, "--platform", "zavlo-cfdi-emitter", "--reason", "test", "--dry-run", "--json", "--grace-hours", grace))
	}
	require.NoError(t, provisionOIDC(t, "--platform", "zavlo-cfdi-emitter", "--reason", "test", "--dry-run", "--json"))
}

func TestRotationSummary(t *testing.T) {
	zero := 0
	assert.Equal(t, " rotated=true grace_hours=0 old_secrets_expire_at=2026-10-03T00:00:00Z",
		rotationSummary(ecosystemoidc.ProvisionResult{RotatedSecret: true, GracePeriodHours: &zero, OldSecretsExpireAt: "2026-10-03T00:00:00Z"}))
	assert.Equal(t, " planned_grace_hours=0", rotationSummary(ecosystemoidc.ProvisionResult{DryRun: true, PlannedGraceHours: &zero}))
	assert.Equal(t, "", rotationSummary(ecosystemoidc.ProvisionResult{Created: true}))
}

func TestOrganizationOnlyOverrideUsesEmbeddedSingleProfile(t *testing.T) {
	for _, platform := range []string{"crea-sim", "crea-sim-mcp"} {
		require.NoError(t, provisionOIDC(t, "--platform", platform, "--organization-id", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "--reason", "synthetic tenant proof", "--dry-run", "--json"))
	}
	for _, args := range [][]string{
		{"--all", "--organization-id", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"},
		{"--platform", "crea-sim", "--registry", "not-read.yaml", "--organization-id", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"},
		{"--platform", "crea-sim", "--organization-id", ""},
		{"--platform", "crea-sim", "--organization-id", "not-a-uuid"},
		{"--platform", "voxa-selva", "--organization-id", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"},
	} {
		require.Error(t, provisionOIDC(t, append(args, "--reason", "synthetic refusal", "--dry-run")...))
	}
}
func TestRotationRequiresExplicitFlag(t *testing.T) {
	cfg := &config.Config{}
	root := newSecretsProvisionCommand(cfg)
	var provision *cobra.Command
	for _, child := range root.Commands() {
		if child.Name() == "oidc" {
			provision = child
		}
	}
	require.NotNil(t, provision)
	value, err := provision.Flags().GetBool("rotate-secret")
	require.NoError(t, err)
	require.False(t, value)
	require.NoError(t, provision.Flags().Set("rotate-secret", "true"))
	value, err = provision.Flags().GetBool("rotate-secret")
	require.NoError(t, err)
	require.True(t, value)
}

func TestAllContractsAreValidatedBeforeFirstProviderEffect(t *testing.T) {
	good := ecosystemoidc.Platform{IntakeTarget: "fixture/oidc", JanuaClient: ecosystemoidc.JanuaClientSpec{Name: "fixture-code", Audience: "fixture-code", ClientKey: "fixture-code", RedirectURIs: []string{"https://fixture.example.test/callback"}, AllowedScopes: []string{"openid"}, GrantTypes: []string{"authorization_code"}}}
	bad := good
	bad.JanuaClient.GrantTypes = []string{"authorization_code", "client_credentials"}
	data, err := yaml.Marshal(ecosystemoidc.Registry{Issuer: "https://auth.example.test", Platforms: map[string]ecosystemoidc.Platform{"fixture-a-first": good, "fixture-b-bad": bad}})
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "registry.yaml")
	require.NoError(t, os.WriteFile(path, data, 0600))
	err = provisionOIDC(t, "--all", "--registry", path, "--reason", "synthetic complete preflight")
	require.Error(t, err)
	require.Contains(t, err.Error(), "grants")
}

func withKeychainOS(t *testing.T, goos string) {
	t.Helper()
	previous := keychainGOOS
	keychainGOOS = goos
	t.Cleanup(func() { keychainGOOS = previous })
}

// --keychain is refused locally, before any Janua or Vault call (the helper
// fails the test on any request), unless it names exactly one platform, is
// not a plan, and runs on macOS against a platform whose registry entry
// declares local_delivery.macos_keychain. The OS is pinned per case, so every
// refusal is reached on any CI host.
func TestProvisionOIDCKeychainRefusalsStayLocal(t *testing.T) {
	withKeychainOS(t, "darwin")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--all", "--keychain"}, "--keychain needs exactly one --platform"},
		{[]string{"--keychain"}, "--keychain needs exactly one --platform"},
		{[]string{"--platform", "ops-agents-karafiel-cogs", "--keychain", "--dry-run"}, "--keychain is refused with --dry-run"},
		{[]string{"--platform", "ops-agents-dhanam-cogs", "--keychain", "--dry-run", "--json"}, "--keychain is refused with --dry-run"},
		{[]string{"--platform", "zavlo-cfdi-emitter", "--keychain"}, `platform "zavlo-cfdi-emitter" declares no local_delivery.macos_keychain`},
		{[]string{"--platform", "ops-agents-selva-cogs", "--keychain"}, `platform "ops-agents-selva-cogs" declares no local_delivery.macos_keychain`},
	} {
		err := provisionOIDC(t, append(tc.args, "--reason", "synthetic keychain refusal")...)
		require.Error(t, err, "%v accepted", tc.args)
		assert.Contains(t, err.Error(), tc.want, "%v", tc.args)
	}

	withKeychainOS(t, "linux")
	err := provisionOIDC(t, "--platform", "ops-agents-karafiel-cogs", "--keychain", "--reason", "synthetic keychain refusal")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runs only on macOS")
}

func TestProvisionOIDCKeychainWriterIsTheSystemKeychainByDefault(t *testing.T) {
	assert.Equal(t, runtime.GOOS, keychainGOOS)
	assert.Equal(t, reflect.ValueOf(ecosystemoidc.NewMacOSKeychain).Pointer(), reflect.ValueOf(newKeychainWriter).Pointer())
}

// failingKeychain passes the preflight and fails the write the way a real
// partial write is reported.
type failingKeychain struct{}

func (failingKeychain) Preflight(context.Context, string) error { return nil }

func (failingKeychain) WriteItems(_ context.Context, service string, _ []ecosystemoidc.KeychainItem) error {
	return &ecosystemoidc.KeychainWriteError{
		Service: service, Account: "client_secret", Step: "write", Detail: "security -i exited 1",
		States: map[string]string{"client_id": ecosystemoidc.KeychainStateNew, "client_secret": ecosystemoidc.KeychainStateAbsent},
	}
}

// A Keychain failure after the intake still prints the --json result, with
// the intake id and what the Keychain holds, before the command fails.
func TestProvisionOIDCKeychainFailureStillPrintsTheJSONResult(t *testing.T) {
	withKeychainOS(t, "darwin")
	previous := newKeychainWriter
	newKeychainWriter = func() ecosystemoidc.KeychainWriter { return failingKeychain{} }
	t.Cleanup(func() { newKeychainWriter = previous })

	const platform = "ops-agents-karafiel-cogs"
	reg, err := ecosystemoidc.LoadRegistry("")
	require.NoError(t, err)
	p := reg.Platforms[platform]
	p.JanuaClient.ClientID = "" // the first run, whatever the registry pins
	reg.Platforms = map[string]ecosystemoidc.Platform{platform: p}
	data, err := yaml.Marshal(reg)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "registry.yaml")
	require.NoError(t, os.WriteFile(path, data, 0o600))

	spec := p.JanuaClient
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/oauth/clients/admin/all":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"clients": []interface{}{}, "total": 0})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/oauth/clients":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id": "dddddddd-dddd-4ddd-8ddd-dddddddddddd", "client_id": "jnc_cmdfixture", "client_secret": "fixture-cmd-secret",
				"name": spec.Name, "client_key": spec.ClientKey, "audience": spec.Audience, "redirect_uris": []string{},
				"organization_id": spec.OrganizationID, "allowed_scopes": spec.AllowedScopes, "grant_types": spec.GrantTypes,
				"is_confidential": true, "is_active": true,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/secrets/intake/targets":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"targets": []map[string]string{{"id": p.IntakeTarget}}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/secrets/intake":
			_ = json.NewEncoder(w).Encode(map[string]string{"intake_id": "int_cmdfixture"})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/secrets/intake/int_cmdfixture":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"intake_id": "int_cmdfixture", "target_id": p.IntakeTarget, "status": "ready", "vault_version": 1,
				"keys_written": []string{"KARAFIEL_COGS_CLIENT_ID", "KARAFIEL_COGS_CLIENT_SECRET"},
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("ENCLII_JANUA_API_URL", server.URL)
	t.Setenv("JANUA_INTERNAL_API_KEY", "")

	cfg := &config.Config{APIEndpoint: server.URL, APIToken: "fixture"}
	root := newSecretsProvisionCommand(cfg)
	root.SetArgs([]string{"oidc", "--platform", platform, "--keychain", "--json", "--registry", path, "--reason", "synthetic keychain failure"})
	root.SilenceUsage = true
	root.SilenceErrors = true
	var runErr error
	output := captureStdoutStderr(t, func() { runErr = root.Execute() })

	require.Error(t, runErr)
	var keychainErr *ecosystemoidc.KeychainDeliveryError
	require.True(t, errors.As(runErr, &keychainErr), "%v", runErr)
	assert.Contains(t, output, `"intake_id": "int_cmdfixture"`)
	assert.Contains(t, output, `"keychain_state"`)
	assert.Contains(t, output, `"madfam-ops-agents-karafiel-cogs/client_secret": "absent"`)
	assert.NotContains(t, output, "fixture-cmd-secret")
	assert.NotContains(t, runErr.Error(), "fixture-cmd-secret")
	assert.NotContains(t, output, "held intake_id", "a filed intake is not reported as held")
}
