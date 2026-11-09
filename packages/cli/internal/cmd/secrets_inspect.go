package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/ecosystemoidc"
	"github.com/spf13/cobra"
)

func newOIDCInspectCommand(cfg *config.Config) *cobra.Command {
	var platform string
	var jsonOut bool
	command := &cobra.Command{
		Use: "inspect", Short: "Read public Janua client metadata without reconciliation or credential access",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(platform) == "" {
				return fmt.Errorf("--platform is required")
			}
			token := cfg.APIToken
			if cfg.Credentials != nil && cfg.Credentials.AccessToken != "" {
				token = cfg.Credentials.AccessToken
			}
			if strings.TrimSpace(token) == "" {
				return fmt.Errorf("not authenticated — use an existing managed Enclii admin session")
			}
			registry, err := ecosystemoidc.LoadRegistry("")
			if err != nil {
				return fmt.Errorf("embedded OIDC registry unavailable")
			}
			profile, found := registry.Platforms[platform]
			if !found {
				return fmt.Errorf("unknown embedded OIDC platform")
			}
			metadata, err := ecosystemoidc.NewJanuaClient(token, "").InspectClient(cmd.Context(), profile.JanuaClient)
			if err != nil {
				return err
			}
			if jsonOut {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(metadata)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "client_id=%s name=%s active=%t confidential=%t\n", metadata.ClientID, metadata.Name, metadata.IsActive, metadata.IsConfidential)
			return err
		},
	}
	command.Flags().StringVar(&platform, "platform", "", "One platform from the embedded OIDC registry")
	command.Flags().BoolVar(&jsonOut, "json", false, "Allowlisted public metadata as JSON")
	return command
}
