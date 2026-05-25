package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

// NewProvidersCommand creates the provider control surface that will replace
// direct gh/cloudflare/porkbun/hetzner tooling for MADFAM operations.
func NewProvidersCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "providers",
		Short: "Operate GitHub, Cloudflare, Porkbun, and Hetzner integrations",
		Long: `Operate external providers through Enclii's audited provider layer.

Mutating commands are dry-run by default. Add --apply --reason "..." to execute
once the server-side provider adapter supports the operation.`,
	}
	cmd.AddCommand(newProvidersCapabilitiesCommand(cfg))
	cmd.AddCommand(newProviderCommand(cfg, "github", "GitHub Actions, secrets, packages, and protection", []providerAction{
		{name: "runs", short: "Inspect workflow runs and jobs", readOnly: true},
		{name: "rerun", short: "Rerun a workflow run", readOnly: false},
		{name: "cancel", short: "Cancel a workflow run", readOnly: false},
		{name: "secrets", short: "Inspect or plan repository/org secret changes", readOnly: true},
		{name: "packages", short: "Inspect GHCR package ownership and permissions", readOnly: true},
		{name: "protection", short: "Inspect or plan branch protection changes", readOnly: true},
	}))
	cmd.AddCommand(newProviderCommand(cfg, "cloudflare", "Cloudflare DNS, tunnels, Access, R2, and SaaS hostnames", []providerAction{
		{name: "dns", short: "Inspect or plan DNS record changes", readOnly: true},
		{name: "dns-apply", short: "Apply DNS record changes", readOnly: false},
		{name: "tunnels", short: "Inspect or plan tunnel route changes", readOnly: true},
		{name: "access", short: "Inspect or plan Access policy changes", readOnly: true},
		{name: "r2", short: "Inspect or plan R2 bucket changes", readOnly: true},
		{name: "hostnames", short: "Inspect custom hostname verification", readOnly: true},
		{name: "credentials", short: "Inspect Cloudflare provider credential readiness", readOnly: true},
	}))
	cmd.AddCommand(newProviderCommand(cfg, "porkbun", "Porkbun domains, DNS fallback, and renewals", []providerAction{
		{name: "domains", short: "Inspect domain inventory and registration state", readOnly: true},
		{name: "dns", short: "Inspect or plan Porkbun DNS fallback changes", readOnly: true},
		{name: "dns-apply", short: "Apply Porkbun DNS fallback record changes", readOnly: false},
		{name: "renewals", short: "Inspect or plan domain renewal actions", readOnly: true},
		{name: "nameservers", short: "Inspect or plan nameserver changes", readOnly: true},
		{name: "nameservers-apply", short: "Apply registrar nameserver changes", readOnly: false},
	}))
	cmd.AddCommand(newProviderCommand(cfg, "hetzner", "Hetzner Robot/Cloud nodes, LB, vSwitch, and storage", []providerAction{
		{name: "nodes", short: "Inspect server inventory, labels, taints, and capacity", readOnly: true},
		{name: "lb", short: "Inspect or plan DR load balancer fallback", readOnly: true},
		{name: "vswitch", short: "Inspect or plan vSwitch/private network changes", readOnly: true},
		{name: "storage", short: "Inspect Storage Box state and backup targets", readOnly: true},
		{name: "firewall", short: "Inspect or plan firewall changes", readOnly: true},
	}))
	return cmd
}

type providerAction struct {
	name     string
	short    string
	readOnly bool
}

func newProvidersCapabilitiesCommand(cfg *config.Config) *cobra.Command {
	var flags operationFlags
	cmd := &cobra.Command{
		Use:   "capabilities",
		Short: "List server-supported provider capabilities",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCapabilities(cmd, cfg, "/v1/providers/capabilities", flags)
		},
	}
	addReadFlags(cmd, &flags)
	return cmd
}

func newProviderCommand(cfg *config.Config, provider, short string, actions []providerAction) *cobra.Command {
	cmd := &cobra.Command{
		Use:   provider,
		Short: short,
	}
	for _, action := range actions {
		if action.readOnly {
			cmd.AddCommand(newProviderReadCommand(cfg, provider, action.name, action.short))
		} else {
			cmd.AddCommand(newProviderActionCommand(cfg, provider, action.name, action.short))
		}
	}
	return cmd
}

func newProviderReadCommand(cfg *config.Config, provider, action, short string) *cobra.Command {
	var flags operationFlags
	cmd := &cobra.Command{
		Use:   action + " [target]",
		Short: short,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			extra := map[string]string{}
			if len(args) == 1 {
				extra["target"] = args[0]
			}
			flags.apply = false
			return runOperation(cmd, cfg, providerPath(provider, action), fmt.Sprintf("providers.%s.%s", provider, action), flags, extra)
		},
	}
	addReadFlags(cmd, &flags)
	cmd.Flags().StringVar(&flags.project, "project", "", "Enclii project slug scope")
	cmd.Flags().StringVar(&flags.service, "service", "", "Enclii service name/id scope")
	return cmd
}

func newProviderActionCommand(cfg *config.Config, provider, action, short string) *cobra.Command {
	var flags operationFlags
	var recordType string
	var content string
	var ttl string
	var zoneDomain string
	var recordName string
	var nameservers string
	var proxied string
	cmd := &cobra.Command{
		Use:   action + " [target]",
		Short: short,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			extra := map[string]string{}
			if len(args) == 1 {
				extra["target"] = args[0]
			}
			if recordType != "" {
				extra["type"] = recordType
			}
			if content != "" {
				extra["content"] = content
			}
			if ttl != "" {
				extra["ttl"] = ttl
			}
			if zoneDomain != "" {
				extra["domain"] = zoneDomain
			}
			if recordName != "" {
				extra["name"] = recordName
			}
			if nameservers != "" {
				extra["nameservers"] = nameservers
			}
			if proxied != "" {
				extra["proxied"] = proxied
			}
			return runOperation(cmd, cfg, providerPath(provider, action), fmt.Sprintf("providers.%s.%s", provider, action), flags, extra)
		},
	}
	addOperationFlags(cmd, &flags)
	if provider == "cloudflare" && action == "dns-apply" {
		cmd.Flags().StringVar(&recordType, "type", "", "DNS record type (default: CNAME)")
		cmd.Flags().StringVar(&content, "content", "", "DNS record content (default: Enclii tunnel CNAME)")
		cmd.Flags().StringVar(&proxied, "proxied", "", "Whether Cloudflare should proxy the record: true/false (default by record type)")
	}
	if provider == "porkbun" && action == "dns-apply" {
		cmd.Flags().StringVar(&recordType, "type", "", "DNS record type (default: CNAME)")
		cmd.Flags().StringVar(&content, "content", "", "DNS record content (default: Enclii tunnel CNAME)")
		cmd.Flags().StringVar(&ttl, "ttl", "", "DNS TTL in seconds")
		cmd.Flags().StringVar(&zoneDomain, "domain", "", "Apex domain managed by Porkbun (derived from target if omitted)")
		cmd.Flags().StringVar(&recordName, "name", "", "Record host/subdomain (derived from target if omitted)")
	}
	if provider == "porkbun" && action == "nameservers-apply" {
		cmd.Flags().StringVar(&nameservers, "nameservers", "", "Comma or space separated authoritative nameservers")
	}
	return cmd
}

func providerPath(provider, action string) string {
	return fmt.Sprintf("/v1/providers/%s/%s", provider, strings.ReplaceAll(action, "_", "-"))
}
