package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
)

// NewBillingCommand creates the `enclii billing` subtree — view spend,
// manage budgets, inspect threshold alerts (P2.2).
//
// The CLI talks to the Switchyard API, which proxies to Waybill internally.
// Endpoints are under /v1/projects/:slug/billing/* in switchyard — the proxy
// resolves the slug to a UUID before forwarding.
func NewBillingCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "billing",
		Short: "View spend, manage budgets, and inspect threshold alerts",
		Long: `View current-period spend and manage per-project budgets.

Alerts at 50/80/100% of a budget are delivered to Dhanam for email /
Slack notification; 100% crossings additionally block deploys in
non-production environments until the operator clears the throttle.

Examples:
  enclii billing show --project my-api
  enclii billing budgets create --project my-api --amount 50000 --period monthly
  enclii billing alerts --project my-api
`,
	}
	cmd.AddCommand(newBillingShowCommand(cfg))
	cmd.AddCommand(newBillingBudgetsCommand(cfg))
	cmd.AddCommand(newBillingAlertsCommand(cfg))
	return cmd
}

// ----------------------------------------------------------------------------
// billing show
// ----------------------------------------------------------------------------

func newBillingShowCommand(cfg *config.Config) *cobra.Command {
	var project, period string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show current-period spend vs. budgets",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if project == "" {
				project = cfg.Project
			}
			if project == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--project is required")}
			}

			ctx := context.Background()
			var cost struct {
				ProjectID   string `json:"project_id"`
				PeriodStart string `json:"period_start"`
				PeriodEnd   string `json:"period_end"`
				TotalCents  int64  `json:"total_cents"`
				Breakdown   []struct {
					Key       string `json:"key"`
					CostCents int64  `json:"cost_cents"`
				} `json:"breakdown"`
			}
			path := fmt.Sprintf("/v1/projects/%s/billing/cost?period=%s", project, period)
			if err := billingRequest(ctx, cfg, "GET", path, nil, &cost); err != nil {
				return err
			}

			var budgets struct {
				Budgets []struct {
					ID              string `json:"id"`
					AmountCents     int64  `json:"amount_cents"`
					Currency        string `json:"currency"`
					Period          string `json:"period"`
					AlertThresholds []int  `json:"alert_thresholds"`
				} `json:"budgets"`
			}
			if err := billingRequest(ctx, cfg, "GET", fmt.Sprintf("/v1/projects/%s/billing/budgets", project), nil, &budgets); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Project: %s\n", project)
			fmt.Fprintf(cmd.OutOrStdout(), "Period:  %s → %s\n", cost.PeriodStart, cost.PeriodEnd)
			fmt.Fprintf(cmd.OutOrStdout(), "Spend:   $%.2f\n", float64(cost.TotalCents)/100)
			if len(budgets.Budgets) > 0 {
				b := budgets.Budgets[0]
				pct := 0
				if b.AmountCents > 0 {
					pct = int((cost.TotalCents * 100) / b.AmountCents)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Budget:  $%.2f (%s) — %d%% consumed\n",
					float64(b.AmountCents)/100, b.Period, pct)
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Budget:  (none — run `enclii billing budgets create`)")
			}

			if len(cost.Breakdown) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "\nTop drivers:")
				tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
				for i, b := range cost.Breakdown {
					if i >= 5 {
						break
					}
					fmt.Fprintf(tw, "  %s\t$%.2f\n", b.Key, float64(b.CostCents)/100)
				}
				_ = tw.Flush()
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project slug")
	cmd.Flags().StringVar(&period, "period", "30d", "Period window: 7d|14d|30d|90d|1y|mtd")
	return cmd
}

// ----------------------------------------------------------------------------
// billing budgets …
// ----------------------------------------------------------------------------

func newBillingBudgetsCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "budgets",
		Short: "Manage per-project budgets (create, list, update, delete)",
	}
	cmd.AddCommand(newBudgetsListCommand(cfg))
	cmd.AddCommand(newBudgetsCreateCommand(cfg))
	cmd.AddCommand(newBudgetsUpdateCommand(cfg))
	cmd.AddCommand(newBudgetsDeleteCommand(cfg))
	return cmd
}

func newBudgetsListCommand(cfg *config.Config) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List budgets for a project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if project == "" {
				project = cfg.Project
			}
			if project == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--project is required")}
			}
			var resp struct {
				Budgets []struct {
					ID              string `json:"id"`
					AmountCents     int64  `json:"amount_cents"`
					Currency        string `json:"currency"`
					Period          string `json:"period"`
					AlertThresholds []int  `json:"alert_thresholds"`
					HardThrottle    bool   `json:"hard_throttle"`
					CreatedAt       string `json:"created_at"`
				} `json:"budgets"`
			}
			if err := billingRequest(context.Background(), cfg, "GET", fmt.Sprintf("/v1/projects/%s/billing/budgets", project), nil, &resp); err != nil {
				return err
			}
			if len(resp.Budgets) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No budgets configured.")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tAMOUNT\tPERIOD\tTHRESHOLDS\tTHROTTLE\tCREATED")
			for _, b := range resp.Budgets {
				fmt.Fprintf(tw, "%s\t$%.2f %s\t%s\t%s\t%t\t%s\n",
					b.ID,
					float64(b.AmountCents)/100, b.Currency, b.Period,
					joinInts(b.AlertThresholds), b.HardThrottle, b.CreatedAt)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project slug")
	return cmd
}

func newBudgetsCreateCommand(cfg *config.Config) *cobra.Command {
	var project, period, thresholds, currency string
	var amountCents int64
	var hardThrottle bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a budget for a project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if project == "" {
				project = cfg.Project
			}
			if project == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--project is required")}
			}
			if amountCents <= 0 {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--amount (cents) must be positive")}
			}
			payload := map[string]interface{}{
				"amount_cents":  amountCents,
				"currency":      currency,
				"period":        period,
				"hard_throttle": hardThrottle,
			}
			if thresholds != "" {
				payload["alert_thresholds"] = parseThresholds(thresholds)
			}
			var resp map[string]interface{}
			if err := billingRequest(context.Background(), cfg, "POST", fmt.Sprintf("/v1/projects/%s/billing/budgets", project), payload, &resp); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created budget %v ($%.2f / %s)\n", resp["id"], float64(amountCents)/100, period)
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project slug")
	cmd.Flags().Int64Var(&amountCents, "amount", 0, "Budget amount in minor currency units (cents)")
	cmd.Flags().StringVar(&currency, "currency", "USD", "ISO currency code")
	cmd.Flags().StringVar(&period, "period", "monthly", "monthly|weekly|quarterly")
	cmd.Flags().StringVar(&thresholds, "thresholds", "", "Comma-separated percent thresholds (e.g. 50,80,100)")
	cmd.Flags().BoolVar(&hardThrottle, "hard-throttle", true, "Auto-throttle non-production deploys at 100%")
	return cmd
}

func newBudgetsUpdateCommand(cfg *config.Config) *cobra.Command {
	var project, thresholds string
	var amountCents int64
	var hardThrottle bool
	var hardThrottleSet bool
	cmd := &cobra.Command{
		Use:   "update <budget_id>",
		Short: "Update an existing budget",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.Project
			}
			if project == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--project is required")}
			}
			payload := map[string]interface{}{}
			if amountCents > 0 {
				payload["amount_cents"] = amountCents
			}
			if thresholds != "" {
				payload["alert_thresholds"] = parseThresholds(thresholds)
			}
			if hardThrottleSet {
				payload["hard_throttle"] = hardThrottle
			}
			if len(payload) == 0 {
				return &exitcodes.ValidationError{Err: fmt.Errorf("nothing to update")}
			}
			path := fmt.Sprintf("/v1/projects/%s/billing/budgets/%s", project, args[0])
			if err := billingRequest(context.Background(), cfg, "PATCH", path, payload, nil); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Updated.")
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project slug")
	cmd.Flags().Int64Var(&amountCents, "amount", 0, "New amount in cents")
	cmd.Flags().StringVar(&thresholds, "thresholds", "", "New percent thresholds")
	cmd.Flags().BoolVar(&hardThrottle, "hard-throttle", true, "Enable/disable hard throttle")
	cmd.PreRun = func(c *cobra.Command, _ []string) {
		hardThrottleSet = c.Flags().Changed("hard-throttle")
	}
	return cmd
}

func newBudgetsDeleteCommand(cfg *config.Config) *cobra.Command {
	var project string
	var force bool
	cmd := &cobra.Command{
		Use:   "delete <budget_id>",
		Short: "Delete a budget",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.Project
			}
			if project == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--project is required")}
			}
			if !force {
				return &exitcodes.ValidationError{Err: fmt.Errorf("re-run with --force to confirm")}
			}
			path := fmt.Sprintf("/v1/projects/%s/billing/budgets/%s", project, args[0])
			if err := billingRequest(context.Background(), cfg, "DELETE", path, nil, nil); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Deleted.")
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project slug")
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation")
	return cmd
}

// ----------------------------------------------------------------------------
// billing alerts
// ----------------------------------------------------------------------------

func newBillingAlertsCommand(cfg *config.Config) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "alerts",
		Short: "List recent budget threshold crossings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if project == "" {
				project = cfg.Project
			}
			if project == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--project is required")}
			}
			var resp struct {
				Alerts []struct {
					Threshold    int        `json:"threshold"`
					ActualCents  int64      `json:"actual_cents"`
					BudgetCents  int64      `json:"budget_cents"`
					DispatchedAt *time.Time `json:"dispatched_at,omitempty"`
					CreatedAt    time.Time  `json:"created_at"`
				} `json:"alerts"`
			}
			path := fmt.Sprintf("/v1/projects/%s/billing/budgets/alerts", project)
			if err := billingRequest(context.Background(), cfg, "GET", path, nil, &resp); err != nil {
				return err
			}
			if len(resp.Alerts) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No alerts yet.")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "CREATED\tTHRESHOLD\tACTUAL\tBUDGET\tDISPATCHED")
			for _, a := range resp.Alerts {
				dispatched := "(pending)"
				if a.DispatchedAt != nil {
					dispatched = a.DispatchedAt.Format(time.RFC3339)
				}
				fmt.Fprintf(tw, "%s\t%d%%\t$%.2f\t$%.2f\t%s\n",
					a.CreatedAt.Format(time.RFC3339),
					a.Threshold,
					float64(a.ActualCents)/100,
					float64(a.BudgetCents)/100,
					dispatched,
				)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project slug")
	return cmd
}

// ----------------------------------------------------------------------------
// helpers
// ----------------------------------------------------------------------------

// billingRequest delegates to the canonical apiRequest helper.
func billingRequest(ctx context.Context, cfg *config.Config, method, path string, payload, out interface{}) error {
	return apiRequest(ctx, cfg, method, path, payload, out)
}

func parseThresholds(s string) []int {
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil || n <= 0 {
			continue
		}
		out = append(out, n)
	}
	return out
}

func joinInts(xs []int) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = strconv.Itoa(x)
	}
	return strings.Join(parts, ",")
}
