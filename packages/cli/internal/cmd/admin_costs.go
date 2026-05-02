package cmd

import (
	"context"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
)

type adminCostAllocation struct {
	ID          string `json:"id"`
	ResourceID  string `json:"resource_id"`
	TenantID    string `json:"tenant_id"`
	AmountCents int64  `json:"amount_cents"`
	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`
}

type adminCostAllocationsResponse struct {
	Allocations []adminCostAllocation `json:"allocations"`
}

func newAdminCostsCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "costs",
		Short: "Inspect platform-level cost allocations and summaries",
	}
	cmd.AddCommand(newAdminCostsAllocationsCommand(cfg))
	cmd.AddCommand(newAdminCostsSummaryCommand(cfg))
	cmd.AddCommand(newAdminCostsAllocateCommand(cfg))
	return cmd
}

func newAdminCostsAllocationsCommand(cfg *config.Config) *cobra.Command {
	var from, to string
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "allocations",
		Short: "List cost allocations",
		RunE: func(cmd *cobra.Command, _ []string) error {
			path := "/v1/admin/costs" + queryString(map[string]string{"from": from, "to": to})
			var resp adminCostAllocationsResponse
			if err := apiRequest(context.Background(), cfg, "GET", path, nil, &resp); err != nil {
				return fmt.Errorf("list cost allocations: %w", err)
			}
			if jsonOut {
				return emitJSON(resp.Allocations)
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tRESOURCE\tTENANT\tAMOUNT\tFROM\tTO")
			for _, a := range resp.Allocations {
				fmt.Fprintf(tw, "%s\t%s\t%s\t$%.2f\t%s\t%s\n",
					a.ID, a.ResourceID, a.TenantID, float64(a.AmountCents)/100, a.PeriodStart, a.PeriodEnd)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "Period start (RFC3339 or YYYY-MM-DD)")
	cmd.Flags().StringVar(&to, "to", "", "Period end (RFC3339 or YYYY-MM-DD)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

func newAdminCostsSummaryCommand(cfg *config.Config) *cobra.Command {
	var from, to string
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "summary",
		Short: "Show platform cost summary",
		RunE: func(cmd *cobra.Command, _ []string) error {
			path := "/v1/admin/costs/summary" + queryString(map[string]string{"from": from, "to": to})
			var resp map[string]interface{}
			if err := apiRequest(context.Background(), cfg, "GET", path, nil, &resp); err != nil {
				return fmt.Errorf("get cost summary: %w", err)
			}
			if jsonOut {
				return emitJSON(resp)
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			for k, v := range resp {
				fmt.Fprintf(tw, "%s\t%v\n", k, v)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "Period start")
	cmd.Flags().StringVar(&to, "to", "", "Period end")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

func newAdminCostsAllocateCommand(cfg *config.Config) *cobra.Command {
	var resource, tenant string
	var amountCents int64
	var force bool
	cmd := &cobra.Command{
		Use:   "allocate",
		Short: "Manually allocate cost to a tenant",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if resource == "" || tenant == "" || amountCents <= 0 {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--resource, --tenant and positive --amount-cents are required")}
			}
			if !force {
				return &exitcodes.ValidationError{Err: fmt.Errorf("re-run with --force to allocate cost")}
			}
			payload := map[string]interface{}{
				"resource_id":  resource,
				"tenant_id":    tenant,
				"amount_cents": amountCents,
			}
			var resp map[string]interface{}
			if err := apiRequest(context.Background(), cfg, "POST", "/v1/admin/costs/allocate", payload, &resp); err != nil {
				return fmt.Errorf("allocate cost: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Cost allocated (id=%v)\n", resp["id"])
			return nil
		},
	}
	cmd.Flags().StringVar(&resource, "resource", "", "Resource id (required)")
	cmd.Flags().StringVar(&tenant, "tenant", "", "Tenant id (required)")
	cmd.Flags().Int64Var(&amountCents, "amount-cents", 0, "Amount in minor currency units (required)")
	cmd.Flags().BoolVar(&force, "force", false, "Confirm allocation")
	return cmd
}
