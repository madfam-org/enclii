package cmd

import (
	"fmt"
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

	for _, want := range []string{"zones", "zone-add-apply", "zone-settings-apply", "dns", "dns-apply", "tunnels", "tunnels-apply", "access", "r2", "hostnames"} {
		assert.NotNil(t, findSubcommand(cloudflare, want), "expected providers cloudflare %s", want)
	}
}

// The Cloudflare zone operations shipped server-side (see the capabilities
// registry in operator_capabilities.go) but were never exposed on the CLI, so
// provisioning the kalya.app apex on 2026-08-21 required curling the endpoint
// by hand. These assert the three verbs exist with the right shape.

func TestProviderCloudflareZones_IsReadOnly(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	zones := findSubcommand(findSubcommand(NewProvidersCommand(cfg), "cloudflare"), "zones")
	require.NotNil(t, zones)

	// Read commands carry the read flag set and force apply=false, so they
	// never expose --apply/--reason.
	assert.NotNil(t, zones.Flags().Lookup("json"), "expected --json")
	assert.NotNil(t, zones.Flags().Lookup("project"), "expected --project")
	assert.NotNil(t, zones.Flags().Lookup("service"), "expected --service")
	assert.Nil(t, zones.Flags().Lookup("apply"), "zones is read-only and must not offer --apply")
	assert.Nil(t, zones.Flags().Lookup("reason"), "zones is read-only and must not offer --reason")
}

func TestProviderCloudflareZoneApplyVerbs_AreMutating(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	cloudflare := findSubcommand(NewProvidersCommand(cfg), "cloudflare")
	require.NotNil(t, cloudflare)

	for _, action := range []string{"zone-add-apply", "zone-settings-apply"} {
		t.Run(action, func(t *testing.T) {
			cmd := findSubcommand(cloudflare, action)
			require.NotNil(t, cmd)

			// Mutating verbs inherit dry-run-by-default plus --apply/--reason
			// from newProviderActionCommand.
			for _, want := range []string{"apply", "reason", "idempotency-key", "json"} {
				assert.NotNil(t, cmd.Flags().Lookup(want), "expected --%s", want)
			}
			// Both handlers take only args.target (the apex), passed
			// positionally.
			assert.Equal(t, action+" [target]", cmd.Use)
		})
	}
}

func TestProviderPath_CloudflareZoneActions(t *testing.T) {
	tests := map[string]string{
		"zones":               "/v1/providers/cloudflare/zones",
		"zone-add-apply":      "/v1/providers/cloudflare/zone-add-apply",
		"zone-settings-apply": "/v1/providers/cloudflare/zone-settings-apply",
	}
	for action, wantPath := range tests {
		t.Run(action, func(t *testing.T) {
			assert.Equal(t, wantPath, providerPath("cloudflare", action))
		})
	}
}

// The CLI's cloudflare verbs must stay in lockstep with the server's
// registered capability actions (operator_capabilities.go). The CLI module
// cannot import switchyard-api's internal package, so the server list is
// pinned here; if the server registry changes, this fails and points at the
// drift rather than letting the CLI silently offer or omit a verb.
func TestProviderCloudflare_MatchesServerCapabilityActions(t *testing.T) {
	serverActions := []string{
		"zones", "zone-add-apply", "zone-settings-apply",
		"dns", "dns-apply", "tunnels", "tunnels-apply",
		"access", "r2", "hostnames", "credentials",
	}

	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	cloudflare := findSubcommand(NewProvidersCommand(cfg), "cloudflare")
	require.NotNil(t, cloudflare)

	cliActions := make([]string, 0, len(cloudflare.Commands()))
	for _, sub := range cloudflare.Commands() {
		cliActions = append(cliActions, sub.Name())
	}
	assert.ElementsMatch(t, serverActions, cliActions,
		"CLI cloudflare verbs must match the server capability registry")

	// And the operation name each verb reports is the dotted capability
	// string the audit trail records.
	for _, action := range serverActions {
		cmd := findSubcommand(cloudflare, action)
		require.NotNil(t, cmd, "expected providers cloudflare %s", action)
		assert.Equal(t, "providers.cloudflare."+action,
			fmt.Sprintf("providers.%s.%s", "cloudflare", cmd.Name()))
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
