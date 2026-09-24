package cmd

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
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
			resp, err := fetchActivity(cmd.Context(), cfg, params)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(resp)
			}
			return renderActivity(cmd.OutOrStdout(), resp)
		},
	}
	cmd.Flags().StringVar(&action, "action", "", "Filter by action name")
	cmd.Flags().StringVar(&resourceType, "resource-type", "", "Filter by resource type")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum number of events to return")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

// activityListResponse is the body of GET /v1/activity (ActivityListResponse
// in apps/switchyard-api/internal/api/activity_handlers.go): the rows are
// types.AuditLog under "activities", with the count and the limit/offset the
// server applied. The CLI used to read an "events" array with "resource" and
// "actor" fields that the API never sent, so the list always came back empty.
type activityListResponse struct {
	Activities []types.AuditLog `json:"activities"`
	Count      int              `json:"count"`
	Limit      int              `json:"limit"`
	Offset     int              `json:"offset"`
}

func fetchActivity(ctx context.Context, cfg *config.Config, params map[string]string) (*activityListResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var resp activityListResponse
	if err := apiRequest(ctx, cfg, "GET", "/v1/activity"+queryString(params), nil, &resp); err != nil {
		return nil, err
	}
	if resp.Activities == nil {
		resp.Activities = []types.AuditLog{}
	}
	return &resp, nil
}

func renderActivity(w io.Writer, resp *activityListResponse) error {
	if len(resp.Activities) == 0 {
		fmt.Fprintln(w, "No activity events match the given filters.")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TIMESTAMP\tACTION\tRESOURCE_TYPE\tRESOURCE\tACTOR\tOUTCOME")
	for _, e := range resp.Activities {
		resource := e.ResourceName
		if resource == "" {
			resource = e.ResourceID
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			e.Timestamp.Format("2006-01-02 15:04"),
			e.Action, e.ResourceType, dashIfEmpty(resource), dashIfEmpty(e.ActorEmail), dashIfEmpty(e.Outcome))
	}
	return tw.Flush()
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
