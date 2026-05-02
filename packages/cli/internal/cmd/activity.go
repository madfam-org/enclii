package cmd

import (
	"context"
	"fmt"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

// NewActivityCommand creates the `enclii activity` subtree — lifecycle event
// feed mirroring the /activity page in switchyard-ui (deploy started/succeeded,
// build failed, env-var changed, etc.). Distinct from `enclii audit`: activity
// is the curated lifecycle stream, audit is the full forensic log.
func NewActivityCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "activity",
		Short: "Stream lifecycle events (deploys, builds, env-var changes)",
		Long: `Stream and filter platform lifecycle events.

Examples:
  enclii activity list --limit 100
  enclii activity list --action deploy.succeeded
  enclii activity list --resource-type service
  enclii activity actions
  enclii activity resource-types
`,
	}
	cmd.AddCommand(newActivityListCommand(cfg))
	cmd.AddCommand(newActivityActionsCommand(cfg))
	cmd.AddCommand(newActivityResourceTypesCommand(cfg))
	return cmd
}

// ----------------------------------------------------------------------------
// activity list
// ----------------------------------------------------------------------------

func newActivityListCommand(cfg *config.Config) *cobra.Command {
	var (
		action       string
		resourceType string
		limit        int
		jsonOut      bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List recent lifecycle events",
		RunE: func(cmd *cobra.Command, _ []string) error {
			params := map[string]string{
				"action":        action,
				"resource_type": resourceType,
				"limit":         strconv.Itoa(limit),
			}
			var resp struct {
				Events []struct {
					ID           string    `json:"id"`
					Timestamp    time.Time `json:"timestamp"`
					Action       string    `json:"action"`
					ResourceType string    `json:"resource_type"`
					Resource     string    `json:"resource"`
					Actor        string    `json:"actor"`
				} `json:"events"`
			}
			path := "/v1/activity" + queryString(params)
			if err := apiRequest(context.Background(), cfg, "GET", path, nil, &resp); err != nil {
				return err
			}

			if jsonOut {
				return emitJSON(resp)
			}

			if len(resp.Events) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No activity events match the given filters.")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "TIMESTAMP\tACTION\tRESOURCE_TYPE\tRESOURCE\tACTOR")
			for _, e := range resp.Events {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
					e.Timestamp.Format("2006-01-02 15:04"),
					e.Action, e.ResourceType, e.Resource, e.Actor)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&action, "action", "", "Filter by action name")
	cmd.Flags().StringVar(&resourceType, "resource-type", "", "Filter by resource type")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum number of events to return")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

// ----------------------------------------------------------------------------
// activity actions
// ----------------------------------------------------------------------------

func newActivityActionsCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "actions",
		Short: "List valid action filter values",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var resp struct {
				Actions []string `json:"actions"`
			}
			if err := apiRequest(context.Background(), cfg, "GET", "/v1/activity/actions", nil, &resp); err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(resp)
			}
			for _, a := range resp.Actions {
				fmt.Fprintln(cmd.OutOrStdout(), a)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

// ----------------------------------------------------------------------------
// activity resource-types
// ----------------------------------------------------------------------------

func newActivityResourceTypesCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "resource-types",
		Short: "List valid resource type filter values",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var resp struct {
				ResourceTypes []string `json:"resource_types"`
			}
			if err := apiRequest(context.Background(), cfg, "GET", "/v1/activity/resource-types", nil, &resp); err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(resp)
			}
			for _, t := range resp.ResourceTypes {
				fmt.Fprintln(cmd.OutOrStdout(), t)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}
