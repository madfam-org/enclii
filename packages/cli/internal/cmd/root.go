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

	return rootCmd
}

func NewVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version information",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Println("enclii version 1.0.0-alpha")
			cmd.Println("Build: development")
		},
	}
}
