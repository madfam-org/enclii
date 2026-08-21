package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/spec"
)

// NewDomainsCommand creates the domains management command with subcommands
func NewDomainsCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "domains",
		Aliases: []string{"domain", "dns"},
		Short:   "Manage custom domains for services",
		Long: `Manage custom domains for your services.

Custom domains allow you to serve your application from your own domain names.
Each domain requires DNS verification before it becomes active.

Examples:
  # List all domains for a service
  enclii domains list --service my-api

  # Add a custom domain
  enclii domains add api.example.com --service my-api --env production

  # Verify DNS configuration
  enclii domains verify api.example.com --service my-api

  # Show domain status with DNS instructions
  enclii domains status api.example.com --service my-api

  # Remove a domain
  enclii domains remove api.example.com --service my-api`,
	}

	cmd.AddCommand(newDomainsListCommand(cfg))
	cmd.AddCommand(newDomainsAddCommand(cfg))
	cmd.AddCommand(newDomainsRemoveCommand(cfg))
	cmd.AddCommand(newDomainsVerifyCommand(cfg))
	cmd.AddCommand(newDomainsStatusCommand(cfg))

	return cmd
}

// newDomainsListCommand creates the 'domains list' subcommand
func newDomainsListCommand(cfg *config.Config) *cobra.Command {
	var serviceName string
	var envName string
	var specFile string
	var showAll bool

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List custom domains for a service",
		Long: `List all custom domains for a service.

Examples:
  enclii domains list
  enclii domains list --service my-api
  enclii domains list --env production`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDomainsList(cfg, serviceName, envName, specFile, showAll)
		},
	}

	cmd.Flags().StringVarP(&serviceName, "service", "s", "", "Service name (uses service.yaml if not specified)")
	cmd.Flags().StringVarP(&envName, "env", "e", "", "Filter by environment")
	cmd.Flags().StringVarP(&specFile, "file", "f", "service.yaml", "Path to service.yaml")
	cmd.Flags().BoolVarP(&showAll, "all", "a", false, "Show all domain details")

	return cmd
}

// newDomainsAddCommand creates the 'domains add' subcommand
func newDomainsAddCommand(cfg *config.Config) *cobra.Command {
	var serviceName string
	var envName string
	var specFile string
	var tlsEnabled bool
	var tlsIssuer string

	cmd := &cobra.Command{
		Use:   "add DOMAIN",
		Short: "Add a custom domain to a service",
		Long: `Add a custom domain to a service.

After adding a domain, you'll need to:
1. Configure DNS to point to the provided CNAME
2. Add a TXT record for verification
3. Run 'enclii domains verify' to confirm ownership

Examples:
  enclii domains add api.example.com --service my-api --env production
  enclii domains add staging.example.com --service my-api --env staging --tls-issuer letsencrypt-staging`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDomainsAdd(cfg, args[0], serviceName, envName, specFile, tlsEnabled, tlsIssuer)
		},
	}

	cmd.Flags().StringVarP(&serviceName, "service", "s", "", "Service name (uses service.yaml if not specified)")
	cmd.Flags().StringVarP(&envName, "env", "e", "production", "Target environment")
	cmd.Flags().StringVarP(&specFile, "file", "f", "service.yaml", "Path to service.yaml")
	cmd.Flags().BoolVar(&tlsEnabled, "tls", true, "Enable TLS (default: true)")
	cmd.Flags().StringVar(&tlsIssuer, "tls-issuer", "", "TLS issuer (letsencrypt-prod, letsencrypt-staging)")

	return cmd
}

// newDomainsRemoveCommand creates the 'domains remove' subcommand
func newDomainsRemoveCommand(cfg *config.Config) *cobra.Command {
	var serviceName string
	var envName string
	var specFile string
	var force bool

	cmd := &cobra.Command{
		Use:     "remove DOMAIN",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove a custom domain from a service",
		Long: `Remove a custom domain from a service.

Examples:
  enclii domains remove api.example.com --service my-api
  enclii domains remove api.example.com --service my-api --force`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDomainsRemove(cfg, args[0], serviceName, envName, specFile, force)
		},
	}

	cmd.Flags().StringVarP(&serviceName, "service", "s", "", "Service name")
	cmd.Flags().StringVarP(&envName, "env", "e", "", "Environment (required if domain exists in multiple envs)")
	cmd.Flags().StringVarP(&specFile, "file", "f", "service.yaml", "Path to service.yaml")
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")

	return cmd
}

// newDomainsVerifyCommand creates the 'domains verify' subcommand
func newDomainsVerifyCommand(cfg *config.Config) *cobra.Command {
	var serviceName string
	var envName string
	var specFile string

	cmd := &cobra.Command{
		Use:   "verify DOMAIN",
		Short: "Verify domain ownership via DNS",
		Long: `Verify domain ownership by checking for the DNS TXT record.

Before running this command, ensure you've added the TXT record shown
when you added the domain.

Examples:
  enclii domains verify api.example.com --service my-api`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDomainsVerify(cfg, args[0], serviceName, envName, specFile)
		},
	}

	cmd.Flags().StringVarP(&serviceName, "service", "s", "", "Service name")
	cmd.Flags().StringVarP(&envName, "env", "e", "", "Environment")
	cmd.Flags().StringVarP(&specFile, "file", "f", "service.yaml", "Path to service.yaml")

	return cmd
}

// newDomainsStatusCommand creates the 'domains status' subcommand
func newDomainsStatusCommand(cfg *config.Config) *cobra.Command {
	var serviceName string
	var envName string
	var specFile string
	var verbose bool

	cmd := &cobra.Command{
		Use:   "status [DOMAIN]",
		Short: "Show domain status and DNS instructions",
		Long: `Show the status of a specific domain or all domains.

Without a domain argument, shows status of all domains.
With a domain argument, shows detailed status and DNS instructions.

Examples:
  enclii domains status --service my-api
  enclii domains status api.example.com --service my-api`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			domain := ""
			if len(args) > 0 {
				domain = args[0]
			}
			return runDomainsStatus(cfg, domain, serviceName, envName, specFile, verbose)
		},
	}

	cmd.Flags().StringVarP(&serviceName, "service", "s", "", "Service name")
	cmd.Flags().StringVarP(&envName, "env", "e", "", "Environment")
	cmd.Flags().StringVarP(&specFile, "file", "f", "service.yaml", "Path to service.yaml")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed information")

	return cmd
}

// runDomainsList implements the domains list command
func runDomainsList(cfg *config.Config, serviceName, envName, specFile string, showAll bool) error {
	ctx := context.Background()

	// Get service info
	service, projectSlug, err := resolveService(ctx, cfg, serviceName, specFile)
	if err != nil {
		return err
	}

	// Create API client
	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)

	// List domains
	domains, err := apiClient.ListCustomDomains(ctx, service.ID.String())
	if err != nil {
		return fmt.Errorf("failed to list domains: %w", err)
	}

	// Filter by environment if specified
	if envName != "" {
		env, err := getEnvironmentByName(ctx, apiClient, projectSlug, envName)
		if err != nil {
			return fmt.Errorf("failed to find environment %s: %w", envName, err)
		}
		filtered := make([]client.CustomDomainResponse, 0)
		for _, d := range domains {
			if d.EnvironmentID != nil && *d.EnvironmentID == env.ID {
				filtered = append(filtered, d)
			}
		}
		domains = filtered
	}

	if len(domains) == 0 {
		fmt.Println("No custom domains found")
		fmt.Println("💡 Add a domain with: enclii domains add example.com --service", service.Name)
		return nil
	}

	// Print as table
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if showAll {
		_, _ = fmt.Fprintln(w, "DOMAIN\tSTATUS\tTLS\tVERIFIED\tCNAME\tCREATED")
	} else {
		_, _ = fmt.Fprintln(w, "DOMAIN\tSTATUS\tTLS\tVERIFIED")
	}

	for _, d := range domains {
		verifiedIcon := "✗"
		if d.Verified {
			verifiedIcon = "✓"
		}

		tlsStatus := "disabled"
		if d.TLSEnabled {
			tlsStatus = "enabled"
		}

		status := d.Status
		if status == "" {
			status = "pending"
		}
		if d.Verified && status == "pending" {
			status = "verified"
		}

		if showAll {
			cname := d.DNSCNAME
			if cname == "" {
				cname = "-"
			}
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				d.Domain, status, tlsStatus, verifiedIcon, cname, d.CreatedAt.Format("2006-01-02"))
		} else {
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
				d.Domain, status, tlsStatus, verifiedIcon)
		}
	}

	_ = w.Flush()

	return nil
}

// runDomainsAdd implements the domains add command
func runDomainsAdd(cfg *config.Config, domain, serviceName, envName, specFile string, tlsEnabled bool, tlsIssuer string) error {
	ctx := context.Background()

	// Get service info
	service, projectSlug, err := resolveService(ctx, cfg, serviceName, specFile)
	if err != nil {
		return err
	}

	// Create API client
	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)

	// Get environment
	env, err := getEnvironmentByName(ctx, apiClient, projectSlug, envName)
	if err != nil {
		return fmt.Errorf("failed to find environment %s: %w", envName, err)
	}

	// Create domain request
	req := client.CustomDomainRequest{
		Domain:        domain,
		Environment:   envName,
		EnvironmentID: env.ID.String(),
		TLSEnabled:    tlsEnabled,
		TLSIssuer:     tlsIssuer,
	}

	// Add domain
	result, err := apiClient.AddCustomDomain(ctx, service.ID.String(), req)
	if err != nil {
		return fmt.Errorf("failed to add domain: %w", err)
	}

	// Print success and DNS instructions
	fmt.Printf("✅ Domain %s added to %s (%s)\n\n", domain, service.Name, envName)
	printDNSInstructions(result, service.Name)

	return nil
}

// runDomainsRemove implements the domains remove command
func runDomainsRemove(cfg *config.Config, domain, serviceName, envName, specFile string, force bool) error {
	ctx := context.Background()

	// Get service info
	service, _, err := resolveService(ctx, cfg, serviceName, specFile)
	if err != nil {
		return err
	}

	// Create API client
	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)

	// Find domain by name
	domainInfo, err := getDomainByName(ctx, apiClient, service.ID.String(), domain)
	if err != nil {
		return err
	}

	// Confirm deletion if not forced
	if !force {
		fmt.Printf("About to remove domain: %s\n", domain)
		fmt.Print("Continue? [y/N]: ")
		var response string
		_, _ = fmt.Scanln(&response)
		if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
			fmt.Println("Aborted")
			return nil
		}
	}

	// Delete domain
	err = apiClient.DeleteCustomDomain(ctx, service.ID.String(), domainInfo.ID.String())
	if err != nil {
		return fmt.Errorf("failed to remove domain: %w", err)
	}

	fmt.Printf("✅ Domain %s removed from %s\n", domain, service.Name)
	fmt.Println("💡 Remember to remove DNS records for this domain.")

	return nil
}

// runDomainsVerify implements the domains verify command
func runDomainsVerify(cfg *config.Config, domain, serviceName, envName, specFile string) error {
	ctx := context.Background()

	// Get service info
	service, _, err := resolveService(ctx, cfg, serviceName, specFile)
	if err != nil {
		return err
	}

	// Create API client
	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)

	// Find domain by name
	domainInfo, err := getDomainByName(ctx, apiClient, service.ID.String(), domain)
	if err != nil {
		return err
	}

	// Verify domain
	result, err := apiClient.VerifyCustomDomain(ctx, service.ID.String(), domainInfo.ID.String())
	if err != nil {
		return fmt.Errorf("failed to verify domain: %w", err)
	}

	// Check result
	if result.Domain != nil && result.Domain.Verified {
		fmt.Printf("✅ Domain %s verified successfully!\n", domain)
		fmt.Println("🔒 TLS certificate will be provisioned automatically.")
		fmt.Println("🌐 Your domain should be active within 5 minutes.")
	} else {
		fmt.Printf("❌ Domain %s not verified\n\n", domain)
		fmt.Println("Expected TXT record not found. Please add:")
		fmt.Printf("   %s  TXT  enclii-verification=%s\n\n", domain, domainInfo.ID.String())
		fmt.Println("You can check your DNS with:")
		fmt.Printf("   dig TXT %s\n\n", domain)
		fmt.Println("💡 DNS changes may take up to 24 hours to propagate.")
	}

	return nil
}

// runDomainsStatus implements the domains status command
func runDomainsStatus(cfg *config.Config, domain, serviceName, envName, specFile string, verbose bool) error {
	ctx := context.Background()

	// Get service info
	service, _, err := resolveService(ctx, cfg, serviceName, specFile)
	if err != nil {
		return err
	}

	// Create API client
	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)

	// If no specific domain, list all
	if domain == "" {
		return runDomainsList(cfg, serviceName, envName, specFile, verbose)
	}

	// Find specific domain
	domainInfo, err := getDomainByName(ctx, apiClient, service.ID.String(), domain)
	if err != nil {
		return err
	}

	// Print detailed status
	fmt.Printf("Domain Status: %s\n", domain)
	fmt.Println(strings.Repeat("─", 50))
	fmt.Println()
	fmt.Printf("  Service:        %s\n", service.Name)

	status := domainInfo.Status
	if status == "" {
		status = "pending"
	}
	if domainInfo.Verified && status == "pending" {
		status = "verified"
	}
	fmt.Printf("  Status:         %s\n", status)

	tlsStatus := "disabled"
	if domainInfo.TLSEnabled {
		tlsStatus = fmt.Sprintf("enabled (%s)", domainInfo.TLSIssuer)
		if domainInfo.TLSIssuer == "" {
			tlsStatus = "enabled"
		}
	}
	fmt.Printf("  TLS:            %s\n", tlsStatus)

	verifiedStatus := "✗ Not verified"
	if domainInfo.Verified {
		verifiedStatus = "✓ Verified"
		if domainInfo.VerifiedAt != nil {
			verifiedStatus += fmt.Sprintf(" (%s)", domainInfo.VerifiedAt.Format("2006-01-02 15:04"))
		}
	}
	fmt.Printf("  Verified:       %s\n", verifiedStatus)
	fmt.Printf("  Created:        %s\n", domainInfo.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Println()

	// Print DNS instructions if not verified
	if !domainInfo.Verified {
		printDNSInstructions(domainInfo, service.Name)
	} else {
		fmt.Println("🌐 Domain is active and serving traffic.")
	}

	return nil
}

// Helper functions

// resolveService gets service info from --service flag or service.yaml
func resolveService(ctx context.Context, cfg *config.Config, serviceName, specFile string) (*client.ServiceInfo, string, error) {
	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)

	var projectSlug string
	var svcName string

	// The manifest may be the onboard-standard two-document enclii.yaml
	// (kind: Project + kind: Service), so select the Service document by
	// name when --service was given; ParseServiceSpecNamed ignores the
	// Project document either way.
	parser := spec.NewParser()
	serviceSpec, err := parser.ParseServiceSpecNamed(specFile, serviceName)
	if err != nil {
		if serviceName != "" {
			return nil, "", fmt.Errorf("failed to parse %s: %w (use --service with a valid service.yaml or enclii.yaml)", specFile, err)
		}
		return nil, "", fmt.Errorf("failed to parse %s: %w", specFile, err)
	}
	projectSlug = serviceSpec.Metadata.Project
	if serviceName != "" {
		svcName = serviceName
	} else {
		svcName = serviceSpec.Metadata.Name
	}

	// Find service
	service, err := getServiceByName(ctx, apiClient, projectSlug, svcName)
	if err != nil {
		return nil, "", fmt.Errorf("failed to find service %s: %w", svcName, err)
	}

	return service, projectSlug, nil
}

// getDomainByName finds a domain by domain name for a service
func getDomainByName(ctx context.Context, apiClient *client.APIClient, serviceID, domainName string) (*client.CustomDomainResponse, error) {
	domains, err := apiClient.ListCustomDomains(ctx, serviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to list domains: %w", err)
	}

	for i, d := range domains {
		if d.Domain == domainName {
			return &domains[i], nil
		}
	}

	return nil, fmt.Errorf("domain %s not found", domainName)
}

// printDNSInstructions prints formatted DNS setup instructions
func printDNSInstructions(domain *client.CustomDomainResponse, serviceName string) {
	fmt.Println("📋 DNS Configuration Required:")
	fmt.Println()

	// CNAME record
	cname := domain.DNSCNAME
	if cname == "" {
		cname = "<tunnel-cname>.cfargotunnel.com"
	}
	fmt.Println("   1. Add a CNAME record:")
	fmt.Printf("      %s  CNAME  %s\n", domain.Domain, cname)
	fmt.Println()

	// TXT record for verification
	fmt.Println("   2. Add a TXT record for verification:")
	fmt.Printf("      %s  TXT  enclii-verification=%s\n", domain.Domain, domain.ID.String())
	fmt.Println()

	// Verification command
	fmt.Println("   3. Run verification:")
	fmt.Printf("      enclii domains verify %s --service %s\n", domain.Domain, serviceName)
	fmt.Println()

	fmt.Println("💡 DNS changes may take up to 24 hours to propagate.")
}
