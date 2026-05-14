package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
)

func newAdminStatusCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Operate the public status page source of truth",
	}
	cmd.AddCommand(newAdminStatusRegenerateCommand(cfg))
	return cmd
}

func newAdminStatusRegenerateCommand(cfg *config.Config) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "regenerate",
		Short: "Regenerate status configmaps from onboarded enclii.yaml declarations",
		Long: `Regenerate status configmaps from the Enclii source of truth.

This projects core platform services plus every onboarded project's
enclii.yaml status.entries[] into apps/status/k8s/{enclii,madfam}/configmap.yaml.
The Switchyard API commits any resulting diff to the Enclii repo for ArgoCD
reconciliation.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !force {
				return &exitcodes.ValidationError{Err: fmt.Errorf("re-run with --force to regenerate status configmaps")}
			}

			var resp map[string]interface{}
			if err := apiRequest(context.Background(), cfg, "POST", "/v1/admin/status/regenerate", nil, &resp); err != nil {
				return fmt.Errorf("regenerate status configmaps: %w", err)
			}
			return emitJSON(resp)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Confirm status configmap regeneration")
	return cmd
}
