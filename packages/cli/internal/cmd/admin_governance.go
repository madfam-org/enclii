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

type adminGovernanceResource struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	PolicyStatus string `json:"policy_status"`
}

type adminGovernanceListResponse struct {
	Resources []adminGovernanceResource `json:"resources"`
}

func newAdminGovernanceCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "governance",
		Short: "Manage governed resources and their policies",
	}
	cmd.AddCommand(newAdminGovernanceListResourcesCommand(cfg))
	cmd.AddCommand(newAdminGovernanceGetResourceCommand(cfg))
	cmd.AddCommand(newAdminGovernanceCreateResourceCommand(cfg))
	cmd.AddCommand(newAdminGovernanceUpdatePolicyCommand(cfg))
	cmd.AddCommand(newAdminGovernanceDeleteResourceCommand(cfg))
	return cmd
}

func newAdminGovernanceListResourcesCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "list-resources",
		Short: "List governed resources",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var resp adminGovernanceListResponse
			if err := apiRequest(context.Background(), cfg, "GET", "/v1/admin/resources", nil, &resp); err != nil {
				return fmt.Errorf("list resources: %w", err)
			}
			if jsonOut {
				return emitJSON(resp.Resources)
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tKIND\tNAME\tPOLICY_STATUS")
			for _, r := range resp.Resources {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.ID, r.Kind, r.Name, r.PolicyStatus)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

func newAdminGovernanceGetResourceCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get-resource <id>",
		Short: "Show governed resource detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var resp map[string]interface{}
			path := fmt.Sprintf("/v1/admin/resources/%s", args[0])
			if err := apiRequest(context.Background(), cfg, "GET", path, nil, &resp); err != nil {
				return fmt.Errorf("get resource: %w", err)
			}
			return emitJSON(resp)
		},
	}
	return cmd
}

func newAdminGovernanceCreateResourceCommand(cfg *config.Config) *cobra.Command {
	var kind, name, owner string
	var force bool
	cmd := &cobra.Command{
		Use:   "create-resource",
		Short: "Register a new governed resource",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if kind == "" || name == "" || owner == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--kind, --name and --owner are required")}
			}
			if !force {
				return &exitcodes.ValidationError{Err: fmt.Errorf("re-run with --force to create resource")}
			}
			payload := map[string]string{"kind": kind, "name": name, "owner": owner}
			var resp map[string]interface{}
			if err := apiRequest(context.Background(), cfg, "POST", "/v1/admin/resources", payload, &resp); err != nil {
				return fmt.Errorf("create resource: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created resource %v\n", resp["id"])
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "Resource kind (required)")
	cmd.Flags().StringVar(&name, "name", "", "Resource name (required)")
	cmd.Flags().StringVar(&owner, "owner", "", "Owning team or user (required)")
	cmd.Flags().BoolVar(&force, "force", false, "Confirm creation")
	return cmd
}

func newAdminGovernanceUpdatePolicyCommand(cfg *config.Config) *cobra.Command {
	var policyFile string
	var force bool
	cmd := &cobra.Command{
		Use:   "update-policy <id>",
		Short: "Replace the policy attached to a resource",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if policyFile == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--policy-file is required")}
			}
			if !force {
				return &exitcodes.ValidationError{Err: fmt.Errorf("re-run with --force to update policy")}
			}
			data, err := os.ReadFile(policyFile)
			if err != nil {
				return fmt.Errorf("read policy file: %w", err)
			}
			path := fmt.Sprintf("/v1/admin/resources/%s/policy", args[0])
			if err := apiRequest(context.Background(), cfg, "PUT", path, map[string]string{"policy": string(data)}, nil); err != nil {
				return fmt.Errorf("update policy: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Policy updated.")
			return nil
		},
	}
	cmd.Flags().StringVar(&policyFile, "policy-file", "", "Path to policy file (required)")
	cmd.Flags().BoolVar(&force, "force", false, "Confirm policy replacement")
	return cmd
}

func newAdminGovernanceDeleteResourceCommand(cfg *config.Config) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "delete-resource <id>",
		Short: "Delete a governed resource",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !force {
				ok, err := confirmDestructive(os.Stdin, cmd.OutOrStdout(),
					fmt.Sprintf("Delete resource %s? [y/N]: ", args[0]))
				if err != nil {
					return err
				}
				if !ok {
					fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
					return nil
				}
			}
			path := fmt.Sprintf("/v1/admin/resources/%s", args[0])
			if err := apiRequest(context.Background(), cfg, "DELETE", path, nil, nil); err != nil {
				return fmt.Errorf("delete resource: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Resource deleted.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation")
	return cmd
}
