package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
)

type adminCluster struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Region   string `json:"region"`
	Provider string `json:"provider"`
}

type adminClusterListResponse struct {
	Clusters []adminCluster `json:"clusters"`
}

func newAdminClustersCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clusters",
		Short: "Manage Kubernetes clusters registered with the platform",
	}
	cmd.AddCommand(newAdminClustersListCommand(cfg))
	cmd.AddCommand(newAdminClustersGetCommand(cfg))
	cmd.AddCommand(newAdminClustersRegisterCommand(cfg))
	cmd.AddCommand(newAdminClustersUpdateCommand(cfg))
	cmd.AddCommand(newAdminClustersDeregisterCommand(cfg))
	return cmd
}

func newAdminClustersListCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List registered clusters",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var resp adminClusterListResponse
			if err := apiRequest(context.Background(), cfg, "GET", "/v1/admin/clusters", nil, &resp); err != nil {
				return fmt.Errorf("list clusters: %w", err)
			}
			if jsonOut {
				return emitJSON(resp.Clusters)
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tNAME\tSTATUS\tREGION\tPROVIDER")
			for _, c := range resp.Clusters {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", c.ID, c.Name, c.Status, c.Region, c.Provider)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

func newAdminClustersGetCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Show cluster detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var resp map[string]interface{}
			path := fmt.Sprintf("/v1/admin/clusters/%s", args[0])
			if err := apiRequest(context.Background(), cfg, "GET", path, nil, &resp); err != nil {
				return fmt.Errorf("get cluster: %w", err)
			}
			return emitJSON(resp)
		},
	}
	return cmd
}

func newAdminClustersRegisterCommand(cfg *config.Config) *cobra.Command {
	var name, kubeconfigFile string
	var force bool
	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register a new cluster from a kubeconfig file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if name == "" || kubeconfigFile == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--name and --kubeconfig-file are required")}
			}
			if !force {
				return &exitcodes.ValidationError{Err: fmt.Errorf("re-run with --force to register cluster")}
			}
			data, err := os.ReadFile(kubeconfigFile)
			if err != nil {
				return fmt.Errorf("read kubeconfig: %w", err)
			}
			payload := map[string]string{"name": name, "kubeconfig": string(data)}
			var resp map[string]interface{}
			if err := apiRequest(context.Background(), cfg, "POST", "/v1/admin/clusters", payload, &resp); err != nil {
				return fmt.Errorf("register cluster: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Registered cluster %v\n", resp["id"])
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Cluster name (required)")
	cmd.Flags().StringVar(&kubeconfigFile, "kubeconfig-file", "", "Path to kubeconfig file (required)")
	cmd.Flags().BoolVar(&force, "force", false, "Confirm registration")
	return cmd
}

func newAdminClustersUpdateCommand(cfg *config.Config) *cobra.Command {
	var name string
	var force bool
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update cluster metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--name is required")}
			}
			if !force {
				return &exitcodes.ValidationError{Err: fmt.Errorf("re-run with --force to update cluster")}
			}
			path := fmt.Sprintf("/v1/admin/clusters/%s", args[0])
			if err := apiRequest(context.Background(), cfg, "PUT", path, map[string]string{"name": name}, nil); err != nil {
				return fmt.Errorf("update cluster: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Cluster updated.")
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "New cluster name (required)")
	cmd.Flags().BoolVar(&force, "force", false, "Confirm update")
	return cmd
}

func newAdminClustersDeregisterCommand(cfg *config.Config) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "deregister <id>",
		Short: "Deregister a cluster",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !force {
				ok, err := confirmDestructive(os.Stdin, cmd.OutOrStdout(),
					fmt.Sprintf("Deregister cluster %s? [y/N]: ", args[0]))
				if err != nil {
					return err
				}
				if !ok {
					fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
					return nil
				}
			}
			path := fmt.Sprintf("/v1/admin/clusters/%s", args[0])
			if err := apiRequest(context.Background(), cfg, "DELETE", path, nil, nil); err != nil {
				return fmt.Errorf("deregister cluster: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Cluster deregistered.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation")
	return cmd
}
