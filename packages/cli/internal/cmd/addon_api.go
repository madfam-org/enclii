package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
)

// newAddonAPICommand builds `enclii addon api` — the auto-generated REST API
// (PostgREST) over a managed Postgres addon. This is the Supabase data-API
// equivalent (parity gap C1). See docs/architecture/data-api-postgrest.md.
//
//	enclii addon api enable  <addon_id> [--schemas public] [--anon-role anon]
//	enclii addon api disable <addon_id> [--yes]
//	enclii addon api info    <addon_id>
//	enclii addon api token   <addon_id> [--role authenticated] [--ttl 3600] [--claim k=v]
func newAddonAPICommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "api",
		Short: "Manage the auto-generated REST API (PostgREST) over a Postgres addon",
		Long: `Manage the auto-generated REST API for a managed Postgres addon.

Enabling turns your database schema into a REST API served at
https://<addon>.<...>.data.enclii.dev, exactly like Supabase's data API
(PostgREST under the hood). Authorization is enforced by row-level security
in YOUR database — enclii creates deny-by-default anon/authenticated roles and
wires the JWT signing secret; you own the RLS policies.

Examples:
  enclii addon api enable 123e4567-...                  # enable with defaults (schema: public)
  enclii addon api enable 123e4567-... --schemas public,api
  enclii addon api info 123e4567-...                    # show status + host
  enclii addon api token 123e4567-... --role authenticated --ttl 3600
  enclii addon api disable 123e4567-... --yes
`,
	}
	cmd.AddCommand(newAddonAPIEnableCommand(cfg))
	cmd.AddCommand(newAddonAPIDisableCommand(cfg))
	cmd.AddCommand(newAddonAPIInfoCommand(cfg))
	cmd.AddCommand(newAddonAPITokenCommand(cfg))
	return cmd
}

// ----------------------------------------------------------------------------
// api enable
// ----------------------------------------------------------------------------

func newAddonAPIEnableCommand(cfg *config.Config) *cobra.Command {
	var (
		schemas    string
		anonRole   string
		outputJSON bool
	)
	cmd := &cobra.Command{
		Use:   "enable <addon_id>",
		Short: "Enable the REST API for a Postgres addon",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			addonID := args[0]
			ctx := context.Background()

			payload := map[string]interface{}{}
			if schemas != "" {
				payload["schemas"] = schemas
			}
			if anonRole != "" {
				payload["anon_role"] = anonRole
			}

			var resp struct {
				DataAPI struct {
					Status  string `json:"status"`
					Host    string `json:"host"`
					Schemas string `json:"schemas"`
				} `json:"data_api"`
				Message string `json:"message"`
			}
			path := fmt.Sprintf("/v1/addons/%s/data-api", url.PathEscape(addonID))
			if err := addonRequest(ctx, cfg, "POST", path, payload, &resp); err != nil {
				return err
			}

			if outputJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			}

			fmt.Printf("Data API enabling for addon %s\n", addonID)
			fmt.Printf("  Status:  %s\n", resp.DataAPI.Status)
			fmt.Printf("  Schemas: %s\n", resp.DataAPI.Schemas)
			if resp.DataAPI.Host != "" {
				fmt.Printf("  URL:     https://%s\n", resp.DataAPI.Host)
			}
			fmt.Println("\nProvisioning PostgREST — poll with 'enclii addon api info " + addonID + "'.")
			fmt.Println("Reminder: enable row-level security on exposed tables — the API is")
			fmt.Println("closed by default until you GRANT + CREATE POLICY (see the docs).")
			return nil
		},
	}
	cmd.Flags().StringVar(&schemas, "schemas", "", "Comma-separated schemas to expose (default: public)")
	cmd.Flags().StringVar(&anonRole, "anon-role", "", "Role for unauthenticated requests (default: anon)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "JSON output")
	return cmd
}

// ----------------------------------------------------------------------------
// api disable
// ----------------------------------------------------------------------------

func newAddonAPIDisableCommand(cfg *config.Config) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "disable <addon_id>",
		Short: "Disable the REST API for a Postgres addon",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			addonID := args[0]
			if !yes {
				fmt.Fprintf(os.Stderr,
					"Disable the data API for addon %s?\n"+
						"The PostgREST deployment, service, ingress, and JWT secret will be\n"+
						"removed. Your data and tables are untouched; the anon/authenticated\n"+
						"roles are left in place so re-enabling reuses them.\n\n"+
						"Re-run with --yes to confirm.\n", addonID)
				return &exitcodes.ValidationError{Err: fmt.Errorf("confirmation required")}
			}
			ctx := context.Background()
			path := fmt.Sprintf("/v1/addons/%s/data-api", url.PathEscape(addonID))
			if err := addonRequest(ctx, cfg, "DELETE", path, nil, nil); err != nil {
				return err
			}
			fmt.Printf("Data API disable requested for addon %s.\n", addonID)
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

// ----------------------------------------------------------------------------
// api info
// ----------------------------------------------------------------------------

func newAddonAPIInfoCommand(cfg *config.Config) *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "info <addon_id>",
		Short: "Show the data-API status for a Postgres addon",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			addonID := args[0]
			ctx := context.Background()

			var resp struct {
				Enabled bool `json:"enabled"`
				DataAPI *struct {
					Status        string `json:"status"`
					StatusMessage string `json:"status_message"`
					Host          string `json:"host"`
					Schemas       string `json:"schemas"`
					AnonRole      string `json:"anon_role"`
				} `json:"data_api"`
			}
			path := fmt.Sprintf("/v1/addons/%s/data-api", url.PathEscape(addonID))
			if err := addonRequest(ctx, cfg, "GET", path, nil, &resp); err != nil {
				return err
			}

			if outputJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			}

			if !resp.Enabled || resp.DataAPI == nil {
				fmt.Printf("Data API is not enabled for addon %s.\n", addonID)
				fmt.Println("Enable it with 'enclii addon api enable " + addonID + "'.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintf(w, "Status:\t%s\n", resp.DataAPI.Status)
			if resp.DataAPI.StatusMessage != "" {
				fmt.Fprintf(w, "Detail:\t%s\n", resp.DataAPI.StatusMessage)
			}
			fmt.Fprintf(w, "URL:\thttps://%s\n", resp.DataAPI.Host)
			fmt.Fprintf(w, "Schemas:\t%s\n", resp.DataAPI.Schemas)
			fmt.Fprintf(w, "Anon role:\t%s\n", resp.DataAPI.AnonRole)
			return w.Flush()
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "JSON output")
	return cmd
}

// ----------------------------------------------------------------------------
// api token
// ----------------------------------------------------------------------------

func newAddonAPITokenCommand(cfg *config.Config) *cobra.Command {
	var (
		role       string
		ttlSeconds int
		claims     []string
		outputJSON bool
	)
	cmd := &cobra.Command{
		Use:   "token <addon_id>",
		Short: "Mint a JWT for the data-API (signed with the addon's secret)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			addonID := args[0]
			ctx := context.Background()

			claimMap := map[string]string{}
			for _, kv := range claims {
				parts := strings.SplitN(kv, "=", 2)
				if len(parts) != 2 {
					return &exitcodes.ValidationError{Err: fmt.Errorf("invalid --claim %q, expected key=value", kv)}
				}
				claimMap[parts[0]] = parts[1]
			}

			payload := map[string]interface{}{}
			if role != "" {
				payload["role"] = role
			}
			if ttlSeconds > 0 {
				payload["ttl_seconds"] = ttlSeconds
			}
			if len(claimMap) > 0 {
				payload["claims"] = claimMap
			}

			var resp struct {
				Token     string `json:"token"`
				Role      string `json:"role"`
				ExpiresAt string `json:"expires_at"`
			}
			path := fmt.Sprintf("/v1/addons/%s/data-api/token", url.PathEscape(addonID))
			if err := addonRequest(ctx, cfg, "POST", path, payload, &resp); err != nil {
				return err
			}

			if outputJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			}

			// Print only the token to stdout so it can be piped/captured; the
			// human-readable context goes to stderr.
			fmt.Fprintf(os.Stderr, "role=%s expires=%s\n", resp.Role, resp.ExpiresAt)
			fmt.Println(resp.Token)
			return nil
		},
	}
	cmd.Flags().StringVar(&role, "role", "", "JWT role claim (default: authenticated)")
	cmd.Flags().IntVar(&ttlSeconds, "ttl", 0, "Token lifetime in seconds (default: 3600, max: 86400)")
	cmd.Flags().StringArrayVar(&claims, "claim", nil, "Extra JWT claim key=value (repeatable)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "JSON output")
	return cmd
}
