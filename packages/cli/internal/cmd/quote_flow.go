package cmd

import (
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func NewQuoteFlowCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "quote-flow",
		Short: "Verify the Selva -> Yantra4D -> Cotiza -> ForgeSight quote path",
		Long: `Verify the Tablaco quote path through Enclii's audited operator API.

The command does not use kubectl or container access. It calls Switchyard API's
read-only quote-flow doctor and reports whether the flow is client-ready,
review-only, blocked by auth, blocked by market data, or blocked by unhealthy
infrastructure.`,
	}
	cmd.AddCommand(newQuoteFlowVerifyCommand(cfg))
	return cmd
}

func newQuoteFlowVerifyCommand(cfg *config.Config) *cobra.Command {
	var flags operationFlags
	project := "tablaco"
	agent := "selva"
	requireMarketVerified := false
	selvaURL := ""
	yantraURL := ""
	cotizaURL := ""
	forgeSightURL := ""

	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Run the Enclii quote-flow readiness doctor",
		Example: `  enclii quote-flow verify --project tablaco --agent selva --require-market-verified
  enclii quote-flow verify --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			args := map[string]string{
				"agent":                   strings.TrimSpace(agent),
				"require_market_verified": strconv.FormatBool(requireMarketVerified),
			}
			if strings.TrimSpace(selvaURL) != "" {
				args["selva_worker_url"] = strings.TrimSpace(selvaURL)
			}
			if strings.TrimSpace(yantraURL) != "" {
				args["yantra_project_url"] = strings.TrimSpace(yantraURL)
			}
			if strings.TrimSpace(cotizaURL) != "" {
				args["cotiza_import_url"] = strings.TrimSpace(cotizaURL)
			}
			if strings.TrimSpace(forgeSightURL) != "" {
				args["forgesight_pricing_url"] = strings.TrimSpace(forgeSightURL)
			}
			flags.apply = false
			flags.project = strings.TrimSpace(project)
			return runOperation(cmd, cfg, opsPath("quote-flow", "verify"), "ops.quote-flow.verify", flags, args)
		},
	}

	addReadFlags(cmd, &flags)
	cmd.Flags().StringVar(&project, "project", project, "Quote-flow project slug")
	cmd.Flags().StringVar(&agent, "agent", agent, "Agent expected to start the flow")
	cmd.Flags().BoolVar(&requireMarketVerified, "require-market-verified", false, "Block readiness unless ForgeSight exposes an explicit pricing/market-data endpoint")
	cmd.Flags().StringVar(&selvaURL, "selva-url", "", "Override Selva worker health URL")
	cmd.Flags().StringVar(&yantraURL, "yantra-url", "", "Override Yantra4D project endpoint URL")
	cmd.Flags().StringVar(&cotizaURL, "cotiza-url", "", "Override Cotiza import health URL")
	cmd.Flags().StringVar(&forgeSightURL, "forgesight-url", "", "Override ForgeSight pricing health URL")
	_ = cmd.RegisterFlagCompletionFunc("project", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return []string{"tablaco"}, cobra.ShellCompDirectiveNoFileComp
	})
	_ = cmd.RegisterFlagCompletionFunc("agent", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return []string{"selva"}, cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}
