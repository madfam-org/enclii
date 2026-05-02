package cmd

import (
	"context"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
)

type adminDriftEvent struct {
	ID         string `json:"id"`
	Resource   string `json:"resource"`
	Severity   string `json:"severity"`
	DetectedAt string `json:"detected_at"`
	Status     string `json:"status"`
}

type adminDriftListResponse struct {
	Events []adminDriftEvent `json:"events"`
}

func newAdminDriftCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "drift",
		Short: "Inspect and resolve cluster drift events",
	}
	cmd.AddCommand(newAdminDriftListCommand(cfg))
	cmd.AddCommand(newAdminDriftGetCommand(cfg))
	cmd.AddCommand(newAdminDriftResolveCommand(cfg))
	return cmd
}

func newAdminDriftListCommand(cfg *config.Config) *cobra.Command {
	var status string
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List drift events",
		RunE: func(cmd *cobra.Command, _ []string) error {
			path := "/v1/admin/drift" + queryString(map[string]string{"status": status})
			var resp adminDriftListResponse
			if err := apiRequest(context.Background(), cfg, "GET", path, nil, &resp); err != nil {
				return fmt.Errorf("list drift events: %w", err)
			}
			if jsonOut {
				return emitJSON(resp.Events)
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tRESOURCE\tSEVERITY\tDETECTED\tSTATUS")
			for _, e := range resp.Events {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", e.ID, e.Resource, e.Severity, e.DetectedAt, e.Status)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "Filter by status")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

func newAdminDriftGetCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Show drift event detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var resp map[string]interface{}
			path := fmt.Sprintf("/v1/admin/drift/%s", args[0])
			if err := apiRequest(context.Background(), cfg, "GET", path, nil, &resp); err != nil {
				return fmt.Errorf("get drift event: %w", err)
			}
			return emitJSON(resp)
		},
	}
	return cmd
}

func newAdminDriftResolveCommand(cfg *config.Config) *cobra.Command {
	var reason string
	var force bool
	cmd := &cobra.Command{
		Use:   "resolve <id>",
		Short: "Mark a drift event as resolved",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if reason == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--reason is required")}
			}
			if !force {
				return &exitcodes.ValidationError{Err: fmt.Errorf("re-run with --force to resolve drift event")}
			}
			path := fmt.Sprintf("/v1/admin/drift/%s/resolve", args[0])
			if err := apiRequest(context.Background(), cfg, "POST", path, map[string]string{"reason": reason}, nil); err != nil {
				return fmt.Errorf("resolve drift: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Drift event resolved.")
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "Resolution reason (required)")
	cmd.Flags().BoolVar(&force, "force", false, "Confirm resolution")
	return cmd
}
