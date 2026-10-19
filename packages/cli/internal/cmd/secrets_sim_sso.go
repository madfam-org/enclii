package cmd

import (
	"encoding/json"
	"fmt"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/ecosystemoidc"
	"github.com/spf13/cobra"
)

func newOIDCSIMSSOCommand(cfg *config.Config) *cobra.Command {
	var expected, reason string
	var apply, disable bool
	command := &cobra.Command{Use: "sim-sso", Short: "Plan one scope-only training UI SSO opt-in; writes require explicit apply and reviewed metadata hash", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		token := cfg.APIToken
		if cfg.Credentials != nil && cfg.Credentials.AccessToken != "" {
			token = cfg.Credentials.AccessToken
		}
		if token == "" {
			return fmt.Errorf("not authenticated — use an existing managed Enclii admin session")
		}
		registry, err := ecosystemoidc.LoadRegistry("")
		if err != nil {
			return fmt.Errorf("embedded OIDC registry unavailable")
		}
		profile, found := registry.Platforms["crea-sim"]
		if !found {
			return fmt.Errorf("embedded training UI profile unavailable")
		}
		plan, err := ecosystemoidc.NewJanuaClient(token, "").ConfigureSIMSSO(cmd.Context(), profile.JanuaClient, expected, apply, disable, reason)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(plan)
	}}
	command.Flags().StringVar(&expected, "expect-metadata-sha256", "", "Exact allowlisted metadata fingerprint from the reviewed plan; required for apply")
	command.Flags().BoolVar(&apply, "apply", false, "Apply only the reviewed scope change, then verify all public metadata")
	command.Flags().BoolVar(&disable, "disable", false, "Plan removal of only the SSO trust scope; disable the SIM bootstrap first")
	command.Flags().StringVar(&reason, "reason", "", "Bounded audit reason; required for apply")
	return command
}
