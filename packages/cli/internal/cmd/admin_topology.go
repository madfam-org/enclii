package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func newAdminTopologyCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "topology",
		Short: "Show fleet/cluster topology graph (JSON)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var resp map[string]interface{}
			if err := apiRequest(context.Background(), cfg, "GET", "/v1/admin/topology", nil, &resp); err != nil {
				return fmt.Errorf("get topology: %w", err)
			}
			return emitJSON(resp)
		},
	}
	return cmd
}
