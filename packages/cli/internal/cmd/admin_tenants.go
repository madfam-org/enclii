package cmd

// `enclii admin tenants ...` — master-admin tenant switcher (XC-2 follow-up).
// Mirrors the same scope-switcher behavior available in the web app, so an
// operator can list, enter, and exit tenants from CI / scripts. Works against
// the four endpoints registered in apps/switchyard-api/internal/api/admin_tenants_handlers.go.

import (
	"context"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

type adminTenantRow struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Slug         string  `json:"slug"`
	Description  *string `json:"description,omitempty"`
	ProjectCount int     `json:"project_count"`
	MemberCount  int     `json:"member_count"`
	CreatedAt    string  `json:"created_at"`
}

type adminTenantListResponse struct {
	Tenants []adminTenantRow `json:"tenants"`
}

type adminActiveSession struct {
	Active    bool            `json:"active"`
	Tenant    *adminTenantRow `json:"tenant,omitempty"`
	StartedAt *string         `json:"started_at,omitempty"`
	ExpiresAt *string         `json:"expires_at,omitempty"`
	Reason    *string         `json:"reason,omitempty"`
}

func newAdminTenantsCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tenants",
		Short: "Manage master-admin acting-as tenant sessions",
		Long: `Manage master-admin "acting as <tenant>" sessions. Mirrors the scope
switcher in the web app. Sessions are bounded (default 4h, max 24h) and
recorded in the admin_acting_sessions audit trail.

Read subcommands accept --json. Mutating subcommands (enter, exit) print
the resulting session metadata so callers can verify state.`,
	}
	cmd.AddCommand(newAdminTenantsListCommand(cfg))
	cmd.AddCommand(newAdminTenantsActiveCommand(cfg))
	cmd.AddCommand(newAdminTenantsEnterCommand(cfg))
	cmd.AddCommand(newAdminTenantsExitCommand(cfg))
	return cmd
}

func newAdminTenantsListCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List every tenant on the platform (admin-only)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var resp adminTenantListResponse
			if err := apiRequest(context.Background(), cfg, "GET", "/v1/admin/tenants", nil, &resp); err != nil {
				return fmt.Errorf("list tenants: %w", err)
			}
			if jsonOut {
				return emitJSON(resp.Tenants)
			}
			if len(resp.Tenants) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No tenants.")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "SLUG\tNAME\tPROJECTS\tCREATED")
			for _, t := range resp.Tenants {
				fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", t.Slug, t.Name, t.ProjectCount, t.CreatedAt[:10])
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

func newAdminTenantsActiveCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "active",
		Short: "Show the current acting-as session, if any",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var resp adminActiveSession
			if err := apiRequest(context.Background(), cfg, "GET", "/v1/admin/tenants/active", nil, &resp); err != nil {
				return fmt.Errorf("get active session: %w", err)
			}
			if jsonOut {
				return emitJSON(resp)
			}
			if !resp.Active || resp.Tenant == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "No active acting-as session.")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Acting as: %s (%s)\n", resp.Tenant.Name, resp.Tenant.Slug)
			if resp.StartedAt != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Started:   %s\n", *resp.StartedAt)
			}
			if resp.ExpiresAt != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Expires:   %s\n", *resp.ExpiresAt)
			}
			if resp.Reason != nil && *resp.Reason != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Reason:    %s\n", *resp.Reason)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

func newAdminTenantsEnterCommand(cfg *config.Config) *cobra.Command {
	var reason string
	var durationSeconds int
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "enter <slug>",
		Short: "Open an acting-as session for a tenant",
		Long: `Opens an "acting as <tenant>" session bound to the calling admin.
Subsequent CLI calls will be filtered to the tenant by the server when
the supporting middleware ships. The session is recorded in the audit
log.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]
			body := map[string]interface{}{}
			if reason != "" {
				body["reason"] = reason
			}
			if durationSeconds > 0 {
				body["duration_seconds"] = durationSeconds
			}
			var resp adminActiveSession
			path := "/v1/admin/tenants/" + slug + "/enter"
			if err := apiRequest(context.Background(), cfg, "POST", path, body, &resp); err != nil {
				return fmt.Errorf("enter tenant %q: %w", slug, err)
			}
			if jsonOut {
				return emitJSON(resp)
			}
			if resp.Tenant != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "✅ Acting as %s (%s)\n", resp.Tenant.Name, resp.Tenant.Slug)
			}
			if resp.ExpiresAt != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Session expires at %s.\n", *resp.ExpiresAt)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Run `enclii admin tenants exit` to end the session.")
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "Operator-supplied reason recorded in the audit log")
	cmd.Flags().IntVar(&durationSeconds, "duration-seconds", 0, "Session length in seconds (default 4h, capped at 24h server-side)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

func newAdminTenantsExitCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "exit",
		Short: "End every open acting-as session for the calling admin",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var resp adminActiveSession
			if err := apiRequest(context.Background(), cfg, "POST", "/v1/admin/tenants/exit", nil, &resp); err != nil {
				return fmt.Errorf("exit tenant: %w", err)
			}
			if jsonOut {
				return emitJSON(resp)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "✅ Acting-as session ended.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}
