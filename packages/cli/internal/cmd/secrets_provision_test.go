package cmd

import (
	"gopkg.in/yaml.v3"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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
