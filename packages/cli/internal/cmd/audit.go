package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
)

// NewAuditCommand creates the `enclii audit` subtree — query the consolidated
// audit log. Mirrors the /audit page in switchyard-ui. The server-side
// endpoint fans out across Janua (auth), Switchyard (deploys/secrets) and
// Selva (RFC ledgers); the CLI itself only renders the merged view.
func NewAuditCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Query the consolidated audit log (auth, deploys, secrets, RFCs)",
		Long: `Query the consolidated audit log across Janua, Switchyard, and Selva.

Examples:
  enclii audit list --limit 100
  enclii audit list --actor usr_123 --action deploy.start
  enclii audit list --resource-type service --resource-id svc_abc
  enclii audit export --from 2026-01-01 --to 2026-02-01 --out audit.csv
`,
	}
	cmd.AddCommand(newAuditListCommand(cfg))
	cmd.AddCommand(newAuditExportCommand(cfg))
	return cmd
}

// ----------------------------------------------------------------------------
// audit list
// ----------------------------------------------------------------------------

func newAuditListCommand(cfg *config.Config) *cobra.Command {
	var (
		actor        string
		action       string
		resourceType string
		resourceID   string
		from         string
		to           string
		limit        int
		page         int
		jsonOut      bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List audit log entries with optional filters",
		RunE: func(cmd *cobra.Command, _ []string) error {
			params := map[string]string{
				"actor":         actor,
				"action":        action,
				"resource_type": resourceType,
				"resource_id":   resourceID,
				"from":          from,
				"to":            to,
				"limit":         strconv.Itoa(limit),
			}
			if page > 0 {
				params["page"] = strconv.Itoa(page)
			}

			var resp struct {
				Entries []struct {
					ID           string                 `json:"id"`
					Timestamp    time.Time              `json:"timestamp"`
					Actor        string                 `json:"actor"`
					Action       string                 `json:"action"`
					ResourceType string                 `json:"resource_type"`
					ResourceID   string                 `json:"resource_id"`
					Source       string                 `json:"source"`
					Metadata     map[string]interface{} `json:"metadata,omitempty"`
				} `json:"entries"`
				Total int `json:"total"`
			}
			path := "/v1/audit" + queryString(params)
			if err := apiRequest(context.Background(), cfg, "GET", path, nil, &resp); err != nil {
				return err
			}

			if jsonOut {
				return emitJSON(resp)
			}

			if len(resp.Entries) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No audit entries match the given filters.")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "TIMESTAMP\tACTOR\tACTION\tRESOURCE\tSOURCE")
			for _, e := range resp.Entries {
				resource := e.ResourceType
				if e.ResourceID != "" {
					resource = e.ResourceType + "/" + e.ResourceID
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
					e.Timestamp.Format("2006-01-02 15:04"),
					e.Actor, e.Action, resource, e.Source)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&actor, "actor", "", "Filter by actor ID")
	cmd.Flags().StringVar(&action, "action", "", "Filter by action name")
	cmd.Flags().StringVar(&resourceType, "resource-type", "", "Filter by resource type")
	cmd.Flags().StringVar(&resourceID, "resource-id", "", "Filter by resource ID")
	cmd.Flags().StringVar(&from, "from", "", "ISO timestamp lower bound (inclusive)")
	cmd.Flags().StringVar(&to, "to", "", "ISO timestamp upper bound (exclusive)")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum number of entries to return")
	cmd.Flags().IntVar(&page, "page", 0, "Page number for pagination (1-based)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

// ----------------------------------------------------------------------------
// audit export
// ----------------------------------------------------------------------------

func newAuditExportCommand(cfg *config.Config) *cobra.Command {
	var (
		from   string
		to     string
		actor  string
		outArg string
	)
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export audit log entries as CSV (admin-only)",
		Long: `Export the audit log as CSV. Server-side admin-gated; non-admin
callers receive a 403 surfaced as a clean error.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			params := map[string]string{
				"from":  from,
				"to":    to,
				"actor": actor,
			}
			path := "/v1/audit/export" + queryString(params)

			endpoint := strings.TrimRight(cfg.APIEndpoint, "/") + path
			req, err := http.NewRequestWithContext(context.Background(), "GET", endpoint, nil)
			if err != nil {
				return fmt.Errorf("build request: %w", err)
			}
			req.Header.Set("Accept", "text/csv")
			req.Header.Set("User-Agent", "enclii-cli/"+Version)
			if cfg.APIToken != "" {
				req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
			}

			resp, err := httpClient().Do(req)
			if err != nil {
				return fmt.Errorf("request: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode == http.StatusUnauthorized {
				return fmt.Errorf("unauthorized — run `enclii login` to refresh credentials")
			}
			if resp.StatusCode == http.StatusForbidden {
				return &exitcodes.ValidationError{Err: fmt.Errorf("forbidden — audit export requires admin permission")}
			}
			if resp.StatusCode >= 400 {
				body, _ := io.ReadAll(resp.Body)
				return fmt.Errorf("API error (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
			}

			out := cmd.OutOrStdout()
			if outArg != "" {
				f, err := os.Create(outArg)
				if err != nil {
					return fmt.Errorf("create output file: %w", err)
				}
				defer func() { _ = f.Close() }()
				out = f
			}
			if _, err := io.Copy(out, resp.Body); err != nil {
				return fmt.Errorf("write CSV: %w", err)
			}
			if outArg != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s\n", outArg)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "ISO timestamp lower bound (inclusive)")
	cmd.Flags().StringVar(&to, "to", "", "ISO timestamp upper bound (exclusive)")
	cmd.Flags().StringVar(&actor, "actor", "", "Filter by actor ID")
	cmd.Flags().StringVar(&outArg, "out", "", "Output file path (default: stdout)")
	return cmd
}
