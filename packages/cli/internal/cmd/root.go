package cmd

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func NewRootCommand(cfg *config.Config) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "enclii",
		Short: "Enclii - Deploy, scale, and operate your services",
		Long: `Enclii is an open source DevOps platform that lets teams build, deploy,
scale, and operate containerized services with guardrails.

Learn more at https://enclii.dev`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Bind flags to viper and update config with flag values
			if endpoint, _ := cmd.Flags().GetString("api-endpoint"); endpoint != "" {
				cfg.APIEndpoint = endpoint
			}
			if token, _ := cmd.Flags().GetString("api-token"); token != "" {
				cfg.APIToken = token
			}
		},
	}

	// Add global flags
	rootCmd.PersistentFlags().String("api-endpoint", cfg.APIEndpoint, "API endpoint URL")
	rootCmd.PersistentFlags().String("api-token", cfg.APIToken, "API authentication token (or set ENCLII_API_TOKEN)")
	rootCmd.PersistentFlags().String("log-level", "info", "Log level (debug, info, warn, error)")

	// Bind flags to viper for environment variable support
	_ = viper.BindPFlag("api-endpoint", rootCmd.PersistentFlags().Lookup("api-endpoint"))
	_ = viper.BindPFlag("api-token", rootCmd.PersistentFlags().Lookup("api-token"))

	// Add subcommands
	rootCmd.AddCommand(NewInitCommand(cfg))
	rootCmd.AddCommand(NewDeployCommand(cfg))
	rootCmd.AddCommand(NewLogsCommand(cfg))
	rootCmd.AddCommand(NewPsCommand(cfg))
	rootCmd.AddCommand(NewRollbackCommand(cfg))
	rootCmd.AddCommand(NewVersionCommand())
	rootCmd.AddCommand(NewLocalCommand(cfg))
	rootCmd.AddCommand(NewServicesSyncCommand(cfg))
	rootCmd.AddCommand(NewServicesDeleteCommand(cfg))
	rootCmd.AddCommand(NewSecretsCommand(cfg))
	rootCmd.AddCommand(NewDomainsCommand(cfg))
	rootCmd.AddCommand(NewReleasesCommand(cfg))

	// Serverless functions (scale-to-zero)
	rootCmd.AddCommand(NewFunctionsCommand(cfg))

	// Scheduled jobs (cron + one-off)
	rootCmd.AddCommand(NewJobsCommand(cfg))

	// Routing and ingress
	rootCmd.AddCommand(NewJunctionsCommand(cfg))

	// Authentication commands
	rootCmd.AddCommand(NewLoginCommand(cfg))
	rootCmd.AddCommand(NewLogoutCommand(cfg))
	rootCmd.AddCommand(NewWhoamiCommand(cfg))

	// Self-serve signup (P3.2 Sprint 1 — browser-based stub)
	rootCmd.AddCommand(NewSignupCommand(cfg))

	// Admin commands
	rootCmd.AddCommand(NewOnboardCommand(cfg))

	// Vault (P0.2 — RFC 0005 Sprint 3 prep; status only, no secret ops)
	rootCmd.AddCommand(NewVaultCommand(cfg))

	// Database inspection (P1.1 — `enclii db wal-status`)
	rootCmd.AddCommand(NewDBCommand(cfg))

	// Canary rollouts (P2.7 — `enclii deploy --canary=N%` + `enclii canary status|promote|rollback`)
	rootCmd.AddCommand(NewCanaryCommand(cfg))

	// Outbound lifecycle webhooks (P2.3)
	rootCmd.AddCommand(NewWebhooksCommand(cfg))

	// Spend visibility + budgets (P2.2)
	rootCmd.AddCommand(NewBillingCommand(cfg))

	// Managed-DB addons (P3.1 Sprint 1)
	rootCmd.AddCommand(NewAddonCommand(cfg))

	// Tenant data export (P3.6)
	rootCmd.AddCommand(NewExportCommand(cfg))

	// Project resource management (containers for services/envs/secrets)
	rootCmd.AddCommand(NewProjectsCommand(cfg))

	// Team membership and invitations
	rootCmd.AddCommand(NewTeamsCommand(cfg))

	// Personal API tokens (CI/CD authentication)
	rootCmd.AddCommand(NewTokensCommand(cfg))

	// Platform operator commands (admin-console parity)
	rootCmd.AddCommand(NewAdminCommand(cfg))

	// Audit log + lifecycle activity feed (mirror /audit, /activity in switchyard-ui)
	rootCmd.AddCommand(NewAuditCommand(cfg))
	rootCmd.AddCommand(NewActivityCommand(cfg))

	// Observability (mirror /observability page)
	rootCmd.AddCommand(NewObserveCommand(cfg))

	// Third-party integrations (GitHub today; more to come)
	rootCmd.AddCommand(NewIntegrationsCommand(cfg))

	// Deployment query surface (complements `deploy` and `releases`)
	rootCmd.AddCommand(NewDeploymentsCommand(cfg))

	return rootCmd
}

// Build metadata. Override at link time via:
//
//	go build -ldflags "-X github.com/madfam-org/enclii/packages/cli/internal/cmd.Version=v1.2.3 \
//	                   -X github.com/madfam-org/enclii/packages/cli/internal/cmd.Commit=abc1234 \
//	                   -X github.com/madfam-org/enclii/packages/cli/internal/cmd.BuildDate=2026-05-02T00:00:00Z"
var (
	Version   = "1.0.0-alpha"
	Commit    = "development"
	BuildDate = "unknown"
)

func NewVersionCommand() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show version information",
		Run: func(cmd *cobra.Command, args []string) {
			if jsonOut {
				_, _ = cmd.OutOrStdout().Write([]byte(
					`{"version":"` + Version + `","commit":"` + Commit + `","build_date":"` + BuildDate + `"}` + "\n",
				))
				return
			}
			cmd.Printf("enclii version %s\n", Version)
			cmd.Printf("Commit:     %s\n", Commit)
			cmd.Printf("Build date: %s\n", BuildDate)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}
