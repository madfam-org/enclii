package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
)

type adminVCluster struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	NodeID string `json:"node_id"`
	Status string `json:"status"`
}

type adminVClusterListResponse struct {
	VClusters []adminVCluster `json:"vclusters"`
}

func newAdminVClustersCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vclusters",
		Short: "Manage virtual clusters (storage/infrastructure tab)",
	}
	cmd.AddCommand(newAdminVClustersListCommand(cfg))
	cmd.AddCommand(newAdminVClustersGetCommand(cfg))
	cmd.AddCommand(newAdminVClustersProvisionCommand(cfg))
	cmd.AddCommand(newAdminVClustersTeardownCommand(cfg))
	cmd.AddCommand(newAdminVClustersKubeconfigCommand(cfg))
	return cmd
}

func newAdminVClustersListCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List virtual clusters",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var resp adminVClusterListResponse
			if err := apiRequest(context.Background(), cfg, "GET", "/v1/admin/vclusters", nil, &resp); err != nil {
				return fmt.Errorf("list vclusters: %w", err)
			}
			if jsonOut {
				return emitJSON(resp.VClusters)
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tNAME\tNODE\tSTATUS")
			for _, v := range resp.VClusters {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", v.ID, v.Name, v.NodeID, v.Status)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

func newAdminVClustersGetCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Show virtual cluster detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var resp map[string]interface{}
			path := fmt.Sprintf("/v1/admin/vclusters/%s", args[0])
			if err := apiRequest(context.Background(), cfg, "GET", path, nil, &resp); err != nil {
				return fmt.Errorf("get vcluster: %w", err)
			}
			return emitJSON(resp)
		},
	}
	return cmd
}

func newAdminVClustersProvisionCommand(cfg *config.Config) *cobra.Command {
	var name, node string
	var force bool
	cmd := &cobra.Command{
		Use:   "provision",
		Short: "Provision a new virtual cluster",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if name == "" || node == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--name and --node are required")}
			}
			if !force {
				return &exitcodes.ValidationError{Err: fmt.Errorf("re-run with --force to provision vcluster")}
			}
			payload := map[string]string{"name": name, "node_id": node}
			var resp map[string]interface{}
			if err := apiRequest(context.Background(), cfg, "POST", "/v1/admin/vclusters", payload, &resp); err != nil {
				return fmt.Errorf("provision vcluster: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Provisioned vcluster %v\n", resp["id"])
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "VCluster name (required)")
	cmd.Flags().StringVar(&node, "node", "", "Host node id (required)")
	cmd.Flags().BoolVar(&force, "force", false, "Confirm provisioning")
	return cmd
}

func newAdminVClustersTeardownCommand(cfg *config.Config) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "teardown <id>",
		Short: "Teardown a virtual cluster",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !force {
				ok, err := confirmDestructive(os.Stdin, cmd.OutOrStdout(),
					fmt.Sprintf("Teardown vcluster %s? [y/N]: ", args[0]))
				if err != nil {
					return err
				}
				if !ok {
					fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
					return nil
				}
			}
			path := fmt.Sprintf("/v1/admin/vclusters/%s", args[0])
			if err := apiRequest(context.Background(), cfg, "DELETE", path, nil, nil); err != nil {
				return fmt.Errorf("teardown vcluster: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "VCluster torn down.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation")
	return cmd
}

func newAdminVClustersKubeconfigCommand(cfg *config.Config) *cobra.Command {
	var outFile string
	cmd := &cobra.Command{
		Use:   "kubeconfig <id>",
		Short: "Fetch the kubeconfig for a virtual cluster",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			yaml, err := fetchVClusterKubeconfig(context.Background(), cfg, args[0])
			if err != nil {
				return err
			}
			if outFile == "" {
				_, err := fmt.Fprint(cmd.OutOrStdout(), yaml)
				return err
			}
			if err := os.WriteFile(outFile, []byte(yaml), 0o600); err != nil {
				return fmt.Errorf("write kubeconfig: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Kubeconfig written to %s\n", outFile)
			return nil
		},
	}
	cmd.Flags().StringVar(&outFile, "out", "", "Write kubeconfig to file (default: stdout)")
	return cmd
}

// fetchVClusterKubeconfig accepts either raw YAML or a JSON envelope of the
// form {"kubeconfig": "..."}; the server has historically returned both.
func fetchVClusterKubeconfig(ctx context.Context, cfg *config.Config, id string) (string, error) {
	endpoint := strings.TrimRight(cfg.APIEndpoint, "/") + fmt.Sprintf("/v1/admin/vclusters/%s/kubeconfig", id)
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("build kubeconfig request: %w", err)
	}
	req.Header.Set("Accept", "application/json, application/yaml, text/yaml, text/plain")
	req.Header.Set("User-Agent", "enclii-cli/"+Version)
	if cfg.APIToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
	}
	resp, err := httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("kubeconfig request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body := make([]byte, 0, 4096)
	buf := make([]byte, 4096)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			body = append(body, buf[:n]...)
		}
		if rerr != nil {
			break
		}
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("API error (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(trimmed, "{") {
		var wrapped struct {
			Kubeconfig string `json:"kubeconfig"`
		}
		if err := json.Unmarshal([]byte(trimmed), &wrapped); err == nil && wrapped.Kubeconfig != "" {
			return wrapped.Kubeconfig, nil
		}
	}
	return string(body), nil
}
