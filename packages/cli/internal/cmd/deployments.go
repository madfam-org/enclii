package cmd

import (
	"context"
	"fmt"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// NewDeploymentsCommand creates the `enclii deployments` subtree (alias
// `deps`) — read-only query surface complementing `enclii deploy` (the
// imperative action) and `enclii releases` (build artifacts). Mirrors the
// /deployments page in switchyard-ui.
func NewDeploymentsCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "deployments",
		Aliases: []string{"deps"},
		Short:   "Query deployment runs across services",
		Long: `Query deployment runs (the runtime side of releases).

Examples:
  enclii deployments list
  enclii deployments list --service svc_abc
  enclii deployments get dep_xyz
  enclii deployments latest --service svc_abc
  enclii deployments by-version --service svc_abc --version 42
`,
	}
	cmd.AddCommand(newDeploymentsListCommand(cfg))
	cmd.AddCommand(newDeploymentsGetCommand(cfg))
	cmd.AddCommand(newDeploymentsLatestCommand(cfg))
	cmd.AddCommand(newDeploymentsByVersionCommand(cfg))
	return cmd
}

// deploymentShortID returns the first 8 chars of a UUID-like string for compact tables.
// Centralized so deployments output stays visually consistent.
func deploymentShortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

// ----------------------------------------------------------------------------
// deployments list
// ----------------------------------------------------------------------------

func newDeploymentsListCommand(cfg *config.Config) *cobra.Command {
	var (
		serviceID string
		limit     int
		jsonOut   bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List deployments (cross-service, or scoped to one service)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := context.Background()

			if serviceID != "" {
				api := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)
				deployments, err := api.ListServiceDeployments(ctx, serviceID)
				if err != nil {
					return err
				}
				if jsonOut {
					return emitJSON(map[string]interface{}{"deployments": deployments})
				}
				return renderDeploymentTable(cmd, serviceID, deployments, limit)
			}

			var resp struct {
				Deployments []struct {
					ID            string    `json:"id"`
					ServiceID     string    `json:"service_id,omitempty"`
					ReleaseID     string    `json:"release_id"`
					Status        string    `json:"status"`
					VersionNumber *int      `json:"version_number,omitempty"`
					CreatedAt     time.Time `json:"created_at"`
					UpdatedAt     time.Time `json:"updated_at"`
				} `json:"deployments"`
			}
			path := "/v1/deployments" + queryString(map[string]string{"limit": strconv.Itoa(limit)})
			if err := apiRequest(ctx, cfg, "GET", path, nil, &resp); err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(resp)
			}
			if len(resp.Deployments) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No deployments found.")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tSERVICE\tVERSION\tSTATUS\tSTARTED\tFINISHED")
			for _, d := range resp.Deployments {
				version := "-"
				if d.VersionNumber != nil {
					version = fmt.Sprintf("v%d", *d.VersionNumber)
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
					deploymentShortID(d.ID), deploymentShortID(d.ServiceID), version, d.Status,
					d.CreatedAt.Format("2006-01-02 15:04"),
					d.UpdatedAt.Format("2006-01-02 15:04"))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&serviceID, "service", "", "Service ID (optional — omit for cross-service list)")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum number of deployments")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

// renderDeploymentTable shares output formatting between list (--service mode)
// and any future caller that gets typed deployments via APIClient.
func renderDeploymentTable(cmd *cobra.Command, serviceID string, deployments []*types.Deployment, limit int) error {
	if len(deployments) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No deployments found.")
		return nil
	}
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSERVICE\tVERSION\tSTATUS\tSTARTED\tFINISHED")
	for i, d := range deployments {
		if limit > 0 && i >= limit {
			break
		}
		version := "-"
		if d.VersionNumber != nil {
			version = fmt.Sprintf("v%d", *d.VersionNumber)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			deploymentShortID(d.ID.String()), deploymentShortID(serviceID), version, string(d.Status),
			d.CreatedAt.Format("2006-01-02 15:04"),
			d.UpdatedAt.Format("2006-01-02 15:04"))
	}
	return tw.Flush()
}

// ----------------------------------------------------------------------------
// deployments get
// ----------------------------------------------------------------------------

func newDeploymentsGetCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "get <deployment_id>",
		Short: "Show full details for a single deployment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			api := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)
			d, err := api.GetDeployment(context.Background(), args[0])
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(d)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "ID:         %s\n", d.ID)
			fmt.Fprintf(cmd.OutOrStdout(), "Release:    %s\n", d.ReleaseID)
			fmt.Fprintf(cmd.OutOrStdout(), "Env:        %s\n", d.EnvironmentID)
			fmt.Fprintf(cmd.OutOrStdout(), "Status:     %s\n", d.Status)
			fmt.Fprintf(cmd.OutOrStdout(), "Health:     %s\n", d.Health)
			fmt.Fprintf(cmd.OutOrStdout(), "Replicas:   %d\n", d.Replicas)
			if d.VersionNumber != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Version:    v%d\n", *d.VersionNumber)
			}
			if d.ErrorMessage != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Error:      %s\n", *d.ErrorMessage)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created:    %s\n", d.CreatedAt.Format(time.RFC3339))
			fmt.Fprintf(cmd.OutOrStdout(), "Updated:    %s\n", d.UpdatedAt.Format(time.RFC3339))
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

// ----------------------------------------------------------------------------
// deployments latest
// ----------------------------------------------------------------------------

func newDeploymentsLatestCommand(cfg *config.Config) *cobra.Command {
	var (
		serviceID string
		jsonOut   bool
	)
	cmd := &cobra.Command{
		Use:   "latest",
		Short: "Show the latest deployment for a service",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if serviceID == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--service is required")}
			}
			api := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)
			latest, err := api.GetLatestDeployment(context.Background(), serviceID)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(latest)
			}
			d := latest.Deployment
			fmt.Fprintf(cmd.OutOrStdout(), "ID:       %s\n", d.ID)
			fmt.Fprintf(cmd.OutOrStdout(), "Status:   %s\n", d.Status)
			fmt.Fprintf(cmd.OutOrStdout(), "Health:   %s\n", d.Health)
			if d.VersionNumber != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Version:  v%d\n", *d.VersionNumber)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created:  %s\n", d.CreatedAt.Format(time.RFC3339))
			if latest.Release != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Image:    %s\n", latest.Release.ImageURI)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&serviceID, "service", "", "Service ID (required)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

// ----------------------------------------------------------------------------
// deployments by-version
// ----------------------------------------------------------------------------

func newDeploymentsByVersionCommand(cfg *config.Config) *cobra.Command {
	var (
		serviceID string
		version   int
		jsonOut   bool
	)
	cmd := &cobra.Command{
		Use:   "by-version",
		Short: "Resolve a deployment by Heroku-style version (v1, v2, …)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if serviceID == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--service is required")}
			}
			if version <= 0 {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--version must be a positive integer")}
			}
			api := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)
			d, err := api.GetDeploymentByVersion(context.Background(), serviceID, version)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(d)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "ID:       %s\n", d.ID)
			fmt.Fprintf(cmd.OutOrStdout(), "Status:   %s\n", d.Status)
			fmt.Fprintf(cmd.OutOrStdout(), "Health:   %s\n", d.Health)
			fmt.Fprintf(cmd.OutOrStdout(), "Version:  v%d\n", version)
			fmt.Fprintf(cmd.OutOrStdout(), "Created:  %s\n", d.CreatedAt.Format(time.RFC3339))
			return nil
		},
	}
	cmd.Flags().StringVar(&serviceID, "service", "", "Service ID (required)")
	cmd.Flags().IntVar(&version, "version", 0, "Heroku-style version number (required)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}
