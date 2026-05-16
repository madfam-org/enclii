package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func TestNewProvidersCommand_Subcommands(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	root := NewProvidersCommand(cfg)
	require.NotNil(t, root)
	assert.Equal(t, "providers", root.Use)

	for _, want := range []string{"capabilities", "github", "cloudflare", "porkbun", "hetzner"} {
		assert.NotNil(t, findSubcommand(root, want), "expected providers %s", want)
	}
}

func TestProviderGitHub_Subcommands(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	github := findSubcommand(NewProvidersCommand(cfg), "github")
	require.NotNil(t, github)

	for _, want := range []string{"runs", "rerun", "cancel", "secrets", "packages", "protection"} {
		assert.NotNil(t, findSubcommand(github, want), "expected providers github %s", want)
	}
}

func TestProviderCloudflare_Subcommands(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	cloudflare := findSubcommand(NewProvidersCommand(cfg), "cloudflare")
	require.NotNil(t, cloudflare)

	for _, want := range []string{"dns", "dns-apply", "tunnels", "access", "r2", "hostnames"} {
		assert.NotNil(t, findSubcommand(cloudflare, want), "expected providers cloudflare %s", want)
	}
}

func TestProviderPorkbun_Subcommands(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	porkbun := findSubcommand(NewProvidersCommand(cfg), "porkbun")
	require.NotNil(t, porkbun)

	for _, want := range []string{"domains", "dns", "dns-apply", "renewals", "nameservers", "nameservers-apply"} {
		assert.NotNil(t, findSubcommand(porkbun, want), "expected providers porkbun %s", want)
	}
}

func TestProviderActionFlags(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	rerun := findSubcommand(findSubcommand(NewProvidersCommand(cfg), "github"), "rerun")
	require.NotNil(t, rerun)

	for _, want := range []string{"apply", "reason", "idempotency-key", "namespace", "project", "service", "json"} {
		assert.NotNil(t, rerun.Flags().Lookup(want), "expected --%s", want)
	}
}

func TestProviderPorkbunDNSApplyFlags(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	dnsApply := findSubcommand(findSubcommand(NewProvidersCommand(cfg), "porkbun"), "dns-apply")
	require.NotNil(t, dnsApply)

	for _, want := range []string{"type", "content", "ttl", "domain", "name"} {
		assert.NotNil(t, dnsApply.Flags().Lookup(want), "expected --%s", want)
	}
}
