package cmd

import (
	"context"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

// NewProjectsCommand creates the `enclii projects` subtree — manage the
// project resource itself (services-sync / services-delete handle services
// inside a project; this group handles the projects).
//
// Lookups go through the typed APIClient where it has methods (list, get,
// create); environments and the lightweight services view fall back to
// apiRequest because there is no dedicated client method that fits.
func NewProjectsCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "projects",
		Aliases: []string{"project"},
		Short:   "Manage projects",
		Long: `Manage projects — the top-level container for services, environments,
secrets, and budgets. To manage services inside a project, see
` + "`enclii services-sync`" + ` and ` + "`enclii services-delete`" + `.

Examples:
  enclii projects list
  enclii projects create --name "Storefront" --slug storefront
  enclii projects environments storefront
  enclii projects services storefront`,
	}

	cmd.AddCommand(newProjectsListCommand(cfg))
	cmd.AddCommand(newProjectsGetCommand(cfg))
	cmd.AddCommand(newProjectsCreateCommand(cfg))
	cmd.AddCommand(newProjectsDeleteCommand(cfg))
	cmd.AddCommand(newProjectsEnvironmentsCommand(cfg))
	cmd.AddCommand(newProjectsServicesCommand(cfg))
	cmd.AddCommand(newProjectsReconcileServicesCommand(cfg))

	return cmd
}

// projectServiceLite is the trimmed shape returned by `projects services`.
// We don't want the full Service type here — the table view only needs the
// triplet plus timestamp.
type projectServiceLite struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind,omitempty"`
	Type      string    `json:"type,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type projectEnvironment struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	KubeNamespace string    `json:"kube_namespace"`
	CreatedAt     time.Time `json:"created_at"`
}

// ----------------------------------------------------------------------------
// projects list
// ----------------------------------------------------------------------------

func newProjectsListCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all projects",
		RunE: func(cmd *cobra.Command, _ []string) error {
			api := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)
			projects, err := api.ListProjects(context.Background())
			if err != nil {
				return fmt.Errorf("list projects: %w", err)
			}
			if jsonOut {
				return emitJSON(projects)
			}
			if len(projects) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No projects found.")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "SLUG\tNAME\tCREATED")
			for _, p := range projects {
				fmt.Fprintf(tw, "%s\t%s\t%s\n",
					p.Slug, p.Name, p.CreatedAt.Format("2006-01-02 15:04"))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

// ----------------------------------------------------------------------------
// projects get
// ----------------------------------------------------------------------------

func newProjectsGetCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "get <slug>",
		Short: "Get project details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			api := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)
			project, err := api.GetProject(context.Background(), args[0])
			if err != nil {
				return fmt.Errorf("get project: %w", err)
			}
			if jsonOut {
				return emitJSON(project)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "ID:        %s\n", project.ID)
			fmt.Fprintf(cmd.OutOrStdout(), "Slug:      %s\n", project.Slug)
			fmt.Fprintf(cmd.OutOrStdout(), "Name:      %s\n", project.Name)
			fmt.Fprintf(cmd.OutOrStdout(), "CI Runner: %s\n", project.CIRunnerMode)
			fmt.Fprintf(cmd.OutOrStdout(), "Created:   %s\n", project.CreatedAt.Format(time.RFC3339))
			fmt.Fprintf(cmd.OutOrStdout(), "Updated:   %s\n", project.UpdatedAt.Format(time.RFC3339))
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

// ----------------------------------------------------------------------------
// projects create
// ----------------------------------------------------------------------------

func newProjectsCreateCommand(cfg *config.Config) *cobra.Command {
	var name, slug string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			api := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)
			project, err := api.CreateProject(context.Background(), name, slug)
			if err != nil {
				return fmt.Errorf("create project: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Project created: %s (%s)\n", project.Name, project.Slug)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Project display name (required)")
	cmd.Flags().StringVar(&slug, "slug", "", "Project URL slug (required)")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("slug")
	return cmd
}

// ----------------------------------------------------------------------------
// projects delete
// ----------------------------------------------------------------------------

func newProjectsDeleteCommand(cfg *config.Config) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:     "delete <slug>",
		Aliases: []string{"rm"},
		Short:   "Delete a project",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !force && !confirm(fmt.Sprintf("Delete project '%s'? This will remove all services, environments, and secrets.", args[0])) {
				fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
				return nil
			}
			path := fmt.Sprintf("/v1/projects/%s", args[0])
			if err := apiRequest(context.Background(), cfg, "DELETE", path, nil, nil); err != nil {
				return fmt.Errorf("delete project: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Project '%s' deleted.\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	return cmd
}

// ----------------------------------------------------------------------------
// projects environments
// ----------------------------------------------------------------------------

func newProjectsEnvironmentsCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:     "environments <slug>",
		Aliases: []string{"envs", "env"},
		Short:   "List environments for a project",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var resp struct {
				Environments []projectEnvironment `json:"environments"`
			}
			path := fmt.Sprintf("/v1/projects/%s/environments", args[0])
			if err := apiRequest(context.Background(), cfg, "GET", path, nil, &resp); err != nil {
				return fmt.Errorf("list environments: %w", err)
			}
			if jsonOut {
				return emitJSON(resp.Environments)
			}
			if len(resp.Environments) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No environments found.")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tNAMESPACE\tCREATED")
			for _, e := range resp.Environments {
				fmt.Fprintf(tw, "%s\t%s\t%s\n",
					e.Name, e.KubeNamespace, e.CreatedAt.Format("2006-01-02 15:04"))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

// ----------------------------------------------------------------------------
// projects services
// ----------------------------------------------------------------------------

func newProjectsServicesCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "services <slug>",
		Short: "List services in a project (lightweight)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var resp struct {
				Services []projectServiceLite `json:"services"`
			}
			path := fmt.Sprintf("/v1/projects/%s/services", args[0])
			if err := apiRequest(context.Background(), cfg, "GET", path, nil, &resp); err != nil {
				return fmt.Errorf("list services: %w", err)
			}
			if jsonOut {
				return emitJSON(resp.Services)
			}
			if len(resp.Services) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No services found.")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tKIND\tCREATED")
			for _, s := range resp.Services {
				kind := s.Kind
				if kind == "" {
					kind = s.Type
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\n",
					s.Name, kind, s.CreatedAt.Format("2006-01-02 15:04"))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

// ----------------------------------------------------------------------------
// projects reconcile-services (admin)
// ----------------------------------------------------------------------------

type reconcileServicesResponse struct {
	ProjectSlug string `json:"project_slug"`
	Namespace   string `json:"namespace"`
	Discovered  int    `json:"discovered"`
	Inserted    int    `json:"inserted"`
	Updated     int    `json:"updated"`
	AlreadyOK   int    `json:"already_ok"`
	Refreshed   int    `json:"refreshed"`
	Services    []struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
		Action    string `json:"action"`
	} `json:"services"`
}

func newProjectsReconcileServicesCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "reconcile-services <slug>",
		Short: "Register cluster Deployments as Enclii services (admin)",
		Long: `Discover Deployments in a project's Kubernetes namespace and ensure
the services table reflects them. Idempotent recovery path for GitOps-
onboarded projects missing service rows or k8s_namespace values.

Requires admin API token.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var resp reconcileServicesResponse
			path := fmt.Sprintf("/v1/admin/projects/%s/reconcile-services", args[0])
			if err := apiRequest(cmd.Context(), cfg, "POST", path, map[string]any{}, &resp); err != nil {
				return fmt.Errorf("reconcile services: %w", err)
			}
			if jsonOut {
				return emitJSON(resp)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Project:    %s\n", resp.ProjectSlug)
			fmt.Fprintf(cmd.OutOrStdout(), "Namespace:  %s\n", resp.Namespace)
			fmt.Fprintf(cmd.OutOrStdout(), "Discovered: %d  Inserted: %d  Updated: %d  Already OK: %d\n",
				resp.Discovered, resp.Inserted, resp.Updated, resp.AlreadyOK)
			if len(resp.Services) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No deployment changes recorded.")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "SERVICE\tNAMESPACE\tACTION")
			for _, s := range resp.Services {
				fmt.Fprintf(tw, "%s\t%s\t%s\n", s.Name, s.Namespace, s.Action)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}
