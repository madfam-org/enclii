package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
)

// NewAddonCommand creates the `enclii addon` subtree — managed-database
// addons with per-addon isolated Postgres via CloudNativePG (P3.1 Sprint 1).
//
// Three subcommands:
//
//	enclii addon create <name> --plan standard-0 [--service <svc>]
//	enclii addon ls [--project <slug>]
//	enclii addon destroy <addon_id> [--yes]
//
// The CLI talks to switchyard-api; the service layer enforces plan validation,
// provisions a CloudNativePG Cluster in the project namespace, and returns
// a Kubernetes Secret reference for the credentials (never the raw password).
// See docs/architecture/managed-db-addon.md for the full design.
func NewAddonCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "addon",
		Aliases: []string{"addons", "db"},
		Short:   "Manage database addons (managed Postgres)",
		Long: `Manage database addons — fresh isolated Postgres instances
scoped to your service, credentials auto-injected as DATABASE_URL.

Examples:
  enclii addon plans                                 # list available plans
  enclii addon create my-db --plan standard-0        # create a 1 GB Postgres
  enclii addon ls --project my-api                   # list project addons
  enclii addon destroy 123e4567-e89b-12d3-...        # destroy (requires --yes)
`,
	}
	cmd.AddCommand(newAddonCreateCommand(cfg))
	cmd.AddCommand(newAddonListCommand(cfg))
	cmd.AddCommand(newAddonDestroyCommand(cfg))
	cmd.AddCommand(newAddonPlansCommand(cfg))
	return cmd
}

// ----------------------------------------------------------------------------
// addon create
// ----------------------------------------------------------------------------

func newAddonCreateCommand(cfg *config.Config) *cobra.Command {
	var (
		project    string
		plan       string
		engine     string
		serviceID  string
		envVar     string
		envID      string
		outputJSON bool
	)
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new managed database addon",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if project == "" {
				project = cfg.Project
			}
			if project == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--project is required (or set in project config)")}
			}
			if plan == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--plan is required (e.g. standard-0)")}
			}

			ctx := context.Background()
			payload := map[string]interface{}{
				"name": name,
				"type": engine,
				"plan": plan,
			}
			// serviceID is deliberately absent from this payload: the create
			// endpoint doesn't take service_id today. The addon is created
			// first and bound in a second call below, mirroring how the HTTP
			// API composes.
			if envID != "" {
				payload["environment_id"] = envID
			}

			var result struct {
				Addon struct {
					ID        string `json:"id"`
					Name      string `json:"name"`
					Plan      string `json:"plan"`
					Status    string `json:"status"`
					CreatedAt string `json:"created_at"`
				} `json:"addon"`
				Message string `json:"message"`
			}
			path := fmt.Sprintf("/v1/projects/%s/addons", url.PathEscape(project))
			if err := addonRequest(ctx, cfg, "POST", path, payload, &result); err != nil {
				return err
			}

			// Optional binding.
			if serviceID != "" {
				bindPath := fmt.Sprintf("/v1/addons/%s/bindings", result.Addon.ID)
				bindPayload := map[string]string{"service_id": serviceID}
				if envVar != "" {
					bindPayload["env_var_name"] = envVar
				}
				if err := addonRequest(ctx, cfg, "POST", bindPath, bindPayload, nil); err != nil {
					fmt.Fprintf(os.Stderr, "warning: addon created but binding failed: %v\n", err)
					// Don't fail the whole command — the addon exists.
				}
			}

			if outputJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			fmt.Printf("Addon created: %s\n", result.Addon.Name)
			fmt.Printf("  ID:     %s\n", result.Addon.ID)
			fmt.Printf("  Plan:   %s\n", result.Addon.Plan)
			fmt.Printf("  Status: %s  (provisioning — poll with 'enclii addon ls')\n", result.Addon.Status)
			if serviceID != "" {
				fmt.Printf("  Bound to service %s as %s\n", serviceID, defaultEnvVar(engine, envVar))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "Project slug (default: active project)")
	cmd.Flags().StringVar(&plan, "plan", "", "Plan code (required, e.g. standard-0)")
	cmd.Flags().StringVar(&engine, "engine", "postgres", "Engine (postgres|redis|mysql)")
	cmd.Flags().StringVar(&serviceID, "service", "", "Service ID to bind DATABASE_URL to (optional)")
	cmd.Flags().StringVar(&envVar, "env-var", "", "Env var name for the binding (default: DATABASE_URL)")
	cmd.Flags().StringVar(&envID, "environment-id", "", "Environment UUID (optional)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "JSON output")
	return cmd
}

func defaultEnvVar(engine, explicit string) string {
	if explicit != "" {
		return explicit
	}
	switch engine {
	case "redis":
		return "REDIS_URL"
	case "mysql":
		return "MYSQL_URL"
	default:
		return "DATABASE_URL"
	}
}

// ----------------------------------------------------------------------------
// addon ls
// ----------------------------------------------------------------------------

func newAddonListCommand(cfg *config.Config) *cobra.Command {
	var (
		project    string
		outputJSON bool
	)
	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List database addons",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := context.Background()
			var path string
			if project == "" {
				project = cfg.Project
			}
			if project != "" {
				path = fmt.Sprintf("/v1/projects/%s/addons", url.PathEscape(project))
			} else {
				// No project scope → global listing (all addons the user can see).
				path = "/v1/addons"
			}

			var resp struct {
				Addons []struct {
					ID        string `json:"id"`
					Name      string `json:"name"`
					Type      string `json:"type"`
					Plan      string `json:"plan"`
					Status    string `json:"status"`
					Host      string `json:"host,omitempty"`
					CreatedAt string `json:"created_at"`
				} `json:"addons"`
				Count int `json:"count"`
			}
			if err := addonRequest(ctx, cfg, "GET", path, nil, &resp); err != nil {
				return err
			}

			if outputJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			}

			if len(resp.Addons) == 0 {
				fmt.Println("No addons found.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tNAME\tENGINE\tPLAN\tSTATUS\tCREATED")
			for _, a := range resp.Addons {
				created := a.CreatedAt
				if t, err := time.Parse(time.RFC3339, a.CreatedAt); err == nil {
					created = t.Format("2006-01-02 15:04")
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					shortAddonID(a.ID), a.Name, a.Type, a.Plan, a.Status, created)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "Project slug (default: list all accessible)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "JSON output")
	return cmd
}

// shortAddonID truncates a UUID for tabular rendering. Kept local to the
// addon package to avoid churning the existing shortID in rollback.go.
func shortAddonID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}

// ----------------------------------------------------------------------------
// addon destroy
// ----------------------------------------------------------------------------

func newAddonDestroyCommand(cfg *config.Config) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "destroy <addon_id>",
		Aliases: []string{"delete", "rm"},
		Short:   "Destroy a database addon (irreversible)",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			addonID := args[0]

			if !yes {
				fmt.Fprintf(os.Stderr,
					"Destroy addon %s?\n"+
						"This is IRREVERSIBLE. The Postgres cluster, PVC, and connection\n"+
						"Secret will all be deleted. Data is not recoverable without a\n"+
						"pre-existing backup.\n\n"+
						"Re-run with --yes to confirm.\n", addonID)
				return &exitcodes.ValidationError{Err: fmt.Errorf("confirmation required")}
			}

			ctx := context.Background()
			path := fmt.Sprintf("/v1/addons/%s", url.PathEscape(addonID))
			if err := addonRequest(ctx, cfg, "DELETE", path, nil, nil); err != nil {
				return err
			}
			fmt.Printf("Addon %s destroyed.\n", addonID)
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

// ----------------------------------------------------------------------------
// addon plans
// ----------------------------------------------------------------------------

func newAddonPlansCommand(cfg *config.Config) *cobra.Command {
	var (
		engine     string
		outputJSON bool
	)
	cmd := &cobra.Command{
		Use:   "plans",
		Short: "List available managed-database plans",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := context.Background()
			path := "/v1/addons/plans"
			if engine != "" {
				path = path + "?engine=" + url.QueryEscape(engine)
			}

			var resp struct {
				Plans []struct {
					Code            string `json:"code"`
					Engine          string `json:"engine"`
					DisplayName     string `json:"display_name"`
					Tier            string `json:"tier"`
					StorageGB       int    `json:"storage_gb"`
					CPURequest      string `json:"cpu_request"`
					MemoryRequest   string `json:"memory_request"`
					MaxConnections  int    `json:"max_connections"`
					HAEnabled       bool   `json:"ha_enabled"`
					PriceCentsMonth int64  `json:"price_cents_month"`
				} `json:"plans"`
			}
			if err := addonRequest(ctx, cfg, "GET", path, nil, &resp); err != nil {
				return err
			}
			if outputJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			}

			if len(resp.Plans) == 0 {
				fmt.Println("No plans available.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "CODE\tENGINE\tTIER\tSTORAGE\tCPU\tMEMORY\tMAX CONNS\tHA\tPRICE/MO")
			for _, p := range resp.Plans {
				ha := "-"
				if p.HAEnabled {
					ha = "yes"
				}
				price := "—"
				if p.PriceCentsMonth > 0 {
					price = fmt.Sprintf("$%.2f", float64(p.PriceCentsMonth)/100)
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%d GB\t%s\t%s\t%d\t%s\t%s\n",
					p.Code, p.Engine, p.Tier, p.StorageGB,
					p.CPURequest, p.MemoryRequest, p.MaxConnections, ha, price)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&engine, "engine", "", "Filter by engine (postgres|redis|mysql)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "JSON output")
	return cmd
}

// ----------------------------------------------------------------------------
// Shared HTTP helper — mirrors billingRequest in billing.go
// ----------------------------------------------------------------------------

func addonRequest(ctx context.Context, cfg *config.Config, method, path string, payload, out interface{}) error {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal payload: %w", err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, cfg.APIEndpoint+path, body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "enclii-cli-addon/1.0")
	if cfg.APIToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		// Try to extract structured error message.
		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
			fmt.Fprintf(os.Stderr, "API error (%d): %s\n", resp.StatusCode, errResp.Error)
			return fmt.Errorf("API returned %d: %s", resp.StatusCode, errResp.Error)
		}
		// Fallback to raw body if truncated.
		preview := string(respBody)
		if len(preview) > 512 {
			preview = preview[:512] + "…"
		}
		fmt.Fprintf(os.Stderr, "API error (%d): %s\n", resp.StatusCode, preview)
		return fmt.Errorf("API returned %d", resp.StatusCode)
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
