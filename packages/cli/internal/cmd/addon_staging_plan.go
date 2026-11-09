package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/spf13/cobra"
)

func newAddonStagingPlanCommand(cfg *config.Config) *cobra.Command {
	var project, plan, serviceID string
	var jsonOut bool
	command := &cobra.Command{
		Use:   "staging-plan",
		Short: "Read the closed Janua staging addon plan (no provisioning)",
		Long:  "Read persisted staging ownership, catalog allocation and namespace metadata. This command cannot create, apply, bind, migrate, copy credentials or activate workloads. A missing staging service remains an explicit gate; a production service is never retargeted.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if project != "janua" || plan == "" {
				return fmt.Errorf("--project janua and --plan are required for the closed staging preview")
			}
			query := url.Values{"plan": []string{plan}}
			if serviceID != "" {
				query.Set("service_id", serviceID)
			}
			body, err := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken).GetRaw(context.Background(), "/v1/admin/projects/janua/environments/staging/addon-plan?"+query.Encode())
			if err != nil {
				return err
			}
			if jsonOut {
				fmt.Fprintln(cmd.OutOrStdout(), string(body))
				return nil
			}
			var response struct {
				Classification string   `json:"classification"`
				ApplySupported bool     `json:"apply_supported"`
				Fingerprint    string   `json:"observation_fingerprint"`
				Gates          []string `json:"unresolved_gates"`
			}
			if err := json.Unmarshal(body, &response); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\napply_supported: %t\nobservation_fingerprint: %s\n", response.Classification, response.ApplySupported, response.Fingerprint)
			for _, gate := range response.Gates {
				fmt.Fprintf(cmd.OutOrStdout(), "gate: %s\n", gate)
			}
			return nil
		},
	}
	command.Flags().StringVar(&project, "project", "", "Closed project (must be janua)")
	command.Flags().StringVar(&plan, "plan", "", "Existing available PostgreSQL plan code to preview")
	command.Flags().StringVar(&serviceID, "service-id", "", "Optional persisted staging API service UUID; never a production service")
	command.Flags().BoolVar(&jsonOut, "json", false, "Print safe observation metadata as JSON")
	return command
}
