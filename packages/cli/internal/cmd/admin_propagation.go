package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
)

type adminPropagationPolicy struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	SourceCluster  string   `json:"source_cluster"`
	TargetClusters []string `json:"target_clusters"`
	ResourceKind   string   `json:"resource_kind"`
	Status         string   `json:"status"`
}

type adminPropagationListResponse struct {
	Policies []adminPropagationPolicy `json:"policies"`
}

func newAdminPropagationCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "propagation",
		Short: "Manage cross-cluster propagation policies",
	}
	cmd.AddCommand(newAdminPropagationListCommand(cfg))
	cmd.AddCommand(newAdminPropagationGetCommand(cfg))
	cmd.AddCommand(newAdminPropagationCreateCommand(cfg))
	cmd.AddCommand(newAdminPropagationDeleteCommand(cfg))
	return cmd
}

func newAdminPropagationListCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List propagation policies",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var resp adminPropagationListResponse
			if err := apiRequest(context.Background(), cfg, "GET", "/v1/admin/propagation", nil, &resp); err != nil {
				return fmt.Errorf("list propagation policies: %w", err)
			}
			if jsonOut {
				return emitJSON(resp.Policies)
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tNAME\tSOURCE\tTARGETS\tKIND\tSTATUS")
			for _, p := range resp.Policies {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
					p.ID, p.Name, p.SourceCluster, strings.Join(p.TargetClusters, ","), p.ResourceKind, p.Status)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

func newAdminPropagationGetCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Show propagation policy detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var resp map[string]interface{}
			path := fmt.Sprintf("/v1/admin/propagation/%s", args[0])
			if err := apiRequest(context.Background(), cfg, "GET", path, nil, &resp); err != nil {
				return fmt.Errorf("get propagation policy: %w", err)
			}
			return emitJSON(resp)
		},
	}
	return cmd
}

func newAdminPropagationCreateCommand(cfg *config.Config) *cobra.Command {
	var name, source, targets, kind string
	var force bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a propagation policy",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if name == "" || source == "" || targets == "" || kind == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--name, --source-cluster, --target-clusters and --resource-kind are required")}
			}
			if !force {
				return &exitcodes.ValidationError{Err: fmt.Errorf("re-run with --force to create policy")}
			}
			payload := map[string]interface{}{
				"name":            name,
				"source_cluster":  source,
				"target_clusters": splitCSV(targets),
				"resource_kind":   kind,
			}
			var resp map[string]interface{}
			if err := apiRequest(context.Background(), cfg, "POST", "/v1/admin/propagation", payload, &resp); err != nil {
				return fmt.Errorf("create propagation policy: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created propagation policy %v\n", resp["id"])
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Policy name (required)")
	cmd.Flags().StringVar(&source, "source-cluster", "", "Source cluster id (required)")
	cmd.Flags().StringVar(&targets, "target-clusters", "", "Comma-separated target cluster ids (required)")
	cmd.Flags().StringVar(&kind, "resource-kind", "", "Kind to propagate, e.g. Secret, ConfigMap (required)")
	cmd.Flags().BoolVar(&force, "force", false, "Confirm policy creation")
	return cmd
}

func newAdminPropagationDeleteCommand(cfg *config.Config) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a propagation policy",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !force {
				ok, err := confirmDestructive(os.Stdin, cmd.OutOrStdout(),
					fmt.Sprintf("Delete propagation policy %s? [y/N]: ", args[0]))
				if err != nil {
					return err
				}
				if !ok {
					fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
					return nil
				}
			}
			path := fmt.Sprintf("/v1/admin/propagation/%s", args[0])
			if err := apiRequest(context.Background(), cfg, "DELETE", path, nil, nil); err != nil {
				return fmt.Errorf("delete propagation policy: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Propagation policy deleted.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation")
	return cmd
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
