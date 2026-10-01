package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
)

func newOpsJunctionsCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{Use: "junctions", Short: "Correct what a hostname's junction is bound to"}
	cmd.AddCommand(newOpsJunctionsRebindCommand(cfg))
	return cmd
}

func newOpsJunctionsRebindCommand(cfg *config.Config) *cobra.Command {
	var flags operationFlags
	var toService, environment, path string
	cmd := &cobra.Command{
		Use:   "rebind <hostname>",
		Short: "Bind a hostname's junction to the service and environment that serve it",
		Long: `Bind a hostname's junction to a service AND an environment.

A junction decides which backend tunnels-apply plans for a hostname. This
rewrites that binding and nothing else: the tunnel route is not touched. Run
the scoped tunnels-apply dry run afterwards to see what it would change; for a
hostname whose live route already serves the new binding, that plan is SKIP.

Dry run by default. Idempotent: a junction already bound as asked is reported
and left alone. Unknown hostnames, services and environments are refused,
with the ones that do exist listed.

Examples:
  # Preview binding the API hostname to the API service in production
  enclii ops junctions rebind api.example.com --project my-project \
    --to-service my-api --environment production

  # Then check the route plan for that hostname (dry run)
  enclii providers cloudflare tunnels-apply api.example.com --project my-project`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(flags.project) == "" || strings.TrimSpace(toService) == "" || strings.TrimSpace(environment) == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--project, --to-service and --environment are required")}
			}
			extra := map[string]string{
				"target":      strings.TrimSpace(args[0]),
				"to_service":  strings.TrimSpace(toService),
				"environment": strings.TrimSpace(environment),
			}
			if strings.TrimSpace(path) != "" {
				extra["path"] = strings.TrimSpace(path)
			}
			return runOperation(cmd, cfg, opsPath("junctions", "rebind"), "ops.junctions.rebind", flags, extra)
		},
	}
	addOperationFlags(cmd, &flags)
	cmd.Flags().StringVar(&toService, "to-service", "", "Service the hostname should be bound to (required)")
	cmd.Flags().StringVar(&environment, "environment", "", "Environment whose workload serves the hostname, e.g. production or staging (required)")
	cmd.Flags().StringVar(&path, "path", "", "Rebind only the junction on this path (default: every junction for the hostname)")
	return cmd
}
