package cmd

import (
	"fmt"
	"net/mail"
	"strings"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
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
	cmd.AddCommand(newProviderCommand(cfg, "cloudflare", "Cloudflare zones, DNS, tunnels, Access, R2, and SaaS hostnames", []providerAction{
		{name: "zones", short: "Inspect the account's Cloudflare zone inventory", readOnly: true},
		{name: "zone-add-apply", short: "Create the Cloudflare zone for an apex domain", readOnly: false},
		{name: "zone-settings-apply", short: "Apply Enclii's HTTPS posture to a zone", readOnly: false},
		{name: "dns", short: "Inspect or plan DNS record changes", readOnly: true},
		{name: "dns-apply", short: "Apply DNS record changes", readOnly: false},
		{name: "tunnels", short: "Inspect tunnel route inventory", readOnly: true},
		{name: "tunnels-apply", short: "Reconcile junction tunnel routes to correct K8s backends", readOnly: false},
		{name: "access", short: "Inspect or plan Access policy changes", readOnly: true},
		{name: "r2", short: "Inspect or plan R2 bucket changes", readOnly: true},
		{name: "hostnames", short: "Inspect custom hostname verification", readOnly: true},
		{name: "credentials", short: "Inspect Cloudflare provider credential readiness", readOnly: true},
	}))
	// Porkbun credentials are per Porkbun ACCOUNT, so every command below takes
	// --tenant/--project to select which account it authenticates against. A
	// client that keeps its own registrar account (CTM/creatumundo.mx) is
	// unreachable with the estate's global key — see
	// docs/infrastructure/porkbun-tenant-credentials.md.
	cmd.AddCommand(newProviderCommand(cfg, "porkbun", "Porkbun domains, DNS fallback, renewals, and per-tenant registrar accounts", []providerAction{
		{name: "credentials", short: "Inspect which Porkbun account a scope resolves to and whether it is usable", readOnly: true},
		{name: "ping", short: "Verify the scope's Porkbun credentials against the live API", readOnly: true},
		{name: "domains", short: "Inspect domain inventory and registration state", readOnly: true},
		{name: "dns", short: "Inspect or plan Porkbun DNS fallback changes", readOnly: true},
		{name: "dns-apply", short: "Apply Porkbun DNS fallback record changes", readOnly: false},
		{name: "renewals", short: "Inspect expiry, auto-renew, and per-domain API access", readOnly: true},
		{name: "nameservers", short: "Inspect or plan nameserver changes", readOnly: true},
		{name: "nameservers-apply", short: "Apply registrar nameserver changes", readOnly: false},
		{name: "auto-renew-apply", short: "Turn registrar auto-renew on or off for a domain", readOnly: false},
	}))
	cmd.AddCommand(newProviderCommand(cfg, "resend", "Resend transactional email domains and send-test", []providerAction{
		{name: "credentials", short: "Inspect Resend API key and sender readiness", readOnly: true},
		{name: "domains", short: "List Resend domains (filter by tenant scope)", readOnly: true},
		{name: "domain", short: "Inspect a single Resend domain and DNS records", readOnly: true},
		{name: "emails", short: "List recent sent emails", readOnly: true},
		{name: "domain-add-apply", short: "Register a domain in Resend", readOnly: false},
		{name: "domain-verify-apply", short: "Trigger Resend domain verification", readOnly: false},
		{name: "domain-dns-apply", short: "Apply Resend DNS records via Cloudflare", readOnly: false},
		{name: "send-test-apply", short: "Send a test email via Resend", readOnly: false},
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
	addTenantScopeFlag(cmd, &flags)
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
	var autoRenew string
	var priority string
	var replace bool
	var recipient string
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
			if autoRenew != "" {
				extra["auto_renew"] = autoRenew
			}
			if priority != "" {
				extra["priority"] = priority
			}
			if replace {
				extra["replace"] = "true"
			}
			if provider == "resend" && action == "send-test-apply" {
				to, err := validateSendTestRecipient(recipient)
				if err != nil {
					return err
				}
				extra["to"] = to
			}
			return runOperation(cmd, cfg, providerPath(provider, action), fmt.Sprintf("providers.%s.%s", provider, action), flags, extra)
		},
	}
	addOperationFlags(cmd, &flags)
	addTenantScopeFlag(cmd, &flags)
	if provider == "cloudflare" && action == "dns-apply" {
		cmd.Flags().StringVar(&recordType, "type", "", "DNS record type (default: CNAME)")
		cmd.Flags().StringVar(&content, "content", "", "DNS record content (default: Enclii tunnel CNAME)")
		cmd.Flags().StringVar(&proxied, "proxied", "", "Whether Cloudflare should proxy the record: true/false (default by record type)")
		// MX/SRV preference as its own field. The pre-#530 form — the number
		// typed into --content as "10 mail.example.com" — still works and is
		// still what the runbooks say; the server splits it back out. This
		// flag wins when both are given.
		cmd.Flags().StringVar(&priority, "priority", "", "MX/SRV preference, e.g. 10 (also accepted inside --content as \"10 host\"; default 10)")
		// Multiple TXT/MX/NS/SRV at one name is the normal shape, so an apply
		// that would collide ADDS a record by default. --replace is the
		// explicit, auditable way to overwrite one instead.
		cmd.Flags().BoolVar(&replace, "replace", false, "Overwrite an existing record of this type at this name instead of adding another (TXT/MX/NS/SRV)")
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
		cmd.Flags().StringVar(&zoneDomain, "domain", "", "Apex domain managed by Porkbun (derived from target if omitted)")
	}
	if provider == "porkbun" && action == "auto-renew-apply" {
		cmd.Flags().StringVar(&autoRenew, "auto-renew", "", "Desired registrar auto-renew state: on or off (required)")
		cmd.Flags().StringVar(&zoneDomain, "domain", "", "Apex domain managed by Porkbun (derived from target if omitted)")
	}
	if provider == "resend" && action == "send-test-apply" {
		// The server reads the recipient from args.to on both the dry-run and
		// the apply path (operator_provider_resend_dns.go) and answers
		// invalid_request without it, so the CLI requires it up front.
		cmd.Flags().StringVar(&recipient, "to", "", "Recipient email address for the test message (required)")
	}
	return cmd
}

// validateSendTestRecipient checks the --to value for resend send-test-apply
// before any API call: exactly one bare address, e.g. ops@example.com. A
// display-name form or a comma-separated list is rejected rather than passed
// through, because the server sends to args.to as a single recipient.
func validateSendTestRecipient(raw string) (string, error) {
	to := strings.TrimSpace(raw)
	if to == "" {
		return "", &exitcodes.ValidationError{Err: fmt.Errorf("--to is required: send-test-apply needs a recipient email address")}
	}
	addr, err := mail.ParseAddress(to)
	if err != nil || addr.Name != "" || addr.Address != to {
		return "", &exitcodes.ValidationError{Err: fmt.Errorf("--to %q is not a single bare email address (e.g. ops@example.com)", raw)}
	}
	return to, nil
}

func providerPath(provider, action string) string {
	return fmt.Sprintf("/v1/providers/%s/%s", provider, strings.ReplaceAll(action, "_", "-"))
}
