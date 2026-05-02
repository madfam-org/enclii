package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

// NewTokensCommand creates the `enclii tokens` subtree — manage personal
// API tokens used by scripts and CI runners.
//
// Tokens authenticate non-interactive callers against Switchyard. Treat
// them like passwords: the full plaintext value is shown ONCE at creation
// and cannot be retrieved later. List/get only return metadata (id, name,
// created_at, last_used_at, expires_at) — never the secret itself.
func NewTokensCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "tokens",
		Aliases: []string{"token"},
		Short:   "Manage personal API tokens for CI and scripts",
		Long: `Manage personal API tokens used by CI pipelines and scripts.

API tokens authenticate non-interactive callers against the Switchyard
API. Treat them like passwords. The full token value is shown ONCE at
creation — there is no way to retrieve it later. If you lose a token,
revoke it and create a new one.

Examples:
  enclii tokens list
  enclii tokens create --name ci-deploy --expires-in 90d
  enclii tokens create --name short-lived --expires-in 24h --scopes deploy,logs
  enclii tokens revoke <token-id> --force`,
	}

	cmd.AddCommand(newTokensListCommand(cfg))
	cmd.AddCommand(newTokensGetCommand(cfg))
	cmd.AddCommand(newTokensCreateCommand(cfg))
	cmd.AddCommand(newTokensRevokeCommand(cfg))

	return cmd
}

type apiToken struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Scopes     []string   `json:"scopes,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

// apiTokenWithSecret is the response shape from POST /v1/user/tokens.
// The `token` field is plaintext and only ever populated on creation.
type apiTokenWithSecret struct {
	apiToken
	Token string `json:"token"`
}

// parseExpiresIn parses Go duration syntax extended with `d` for days.
// time.ParseDuration rejects "d" so we strip it ourselves before falling
// through. Returns the duration in seconds.
func parseExpiresIn(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if strings.HasSuffix(s, "d") {
		num, err := strconv.ParseInt(strings.TrimSuffix(s, "d"), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid day duration %q: %w", s, err)
		}
		if num <= 0 {
			return 0, fmt.Errorf("duration must be positive: %s", s)
		}
		return num * 86400, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q (use e.g. 24h, 30d, 90d): %w", s, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("duration must be positive: %s", s)
	}
	return int64(d.Seconds()), nil
}

// ----------------------------------------------------------------------------
// tokens list
// ----------------------------------------------------------------------------

func newTokensListCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List your API tokens",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var resp struct {
				Tokens []apiToken `json:"tokens"`
			}
			if err := apiRequest(context.Background(), cfg, "GET", "/v1/user/tokens", nil, &resp); err != nil {
				return fmt.Errorf("list tokens: %w", err)
			}
			if jsonOut {
				return emitJSON(resp.Tokens)
			}
			if len(resp.Tokens) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No tokens found. Create one with `enclii tokens create --name <name>`.")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tNAME\tCREATED\tLAST USED\tEXPIRES")
			for _, t := range resp.Tokens {
				lastUsed := "(never)"
				if t.LastUsedAt != nil {
					lastUsed = t.LastUsedAt.Format("2006-01-02 15:04")
				}
				expires := "(never)"
				if t.ExpiresAt != nil {
					expires = t.ExpiresAt.Format("2006-01-02 15:04")
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
					t.ID, t.Name,
					t.CreatedAt.Format("2006-01-02 15:04"),
					lastUsed, expires)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

// ----------------------------------------------------------------------------
// tokens get
// ----------------------------------------------------------------------------

func newTokensGetCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "get <token_id>",
		Short: "Get token metadata (never the secret value)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var t apiToken
			path := fmt.Sprintf("/v1/user/tokens/%s", args[0])
			if err := apiRequest(context.Background(), cfg, "GET", path, nil, &t); err != nil {
				return fmt.Errorf("get token: %w", err)
			}
			if jsonOut {
				return emitJSON(t)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "ID:        %s\n", t.ID)
			fmt.Fprintf(cmd.OutOrStdout(), "Name:      %s\n", t.Name)
			fmt.Fprintf(cmd.OutOrStdout(), "Created:   %s\n", t.CreatedAt.Format(time.RFC3339))
			if len(t.Scopes) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Scopes:    %s\n", strings.Join(t.Scopes, ","))
			}
			if t.LastUsedAt != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Last Used: %s\n", t.LastUsedAt.Format(time.RFC3339))
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Last Used: (never)")
			}
			if t.ExpiresAt != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Expires:   %s\n", t.ExpiresAt.Format(time.RFC3339))
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Expires:   (never)")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

// ----------------------------------------------------------------------------
// tokens create
// ----------------------------------------------------------------------------

func newTokensCreateCommand(cfg *config.Config) *cobra.Command {
	var name, expiresIn, scopesCSV string
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new API token (full value shown ONCE)",
		Long: `Create a new personal API token.

The full token value is printed to STDERR with a clear warning. Capture
it immediately — it cannot be retrieved later. Default expiry is 90 days.
Use Go duration syntax extended with 'd' for days: 24h, 30d, 90d.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if expiresIn == "" {
				expiresIn = "90d"
			}
			seconds, err := parseExpiresIn(expiresIn)
			if err != nil {
				return err
			}

			payload := map[string]interface{}{
				"name":               name,
				"expires_in_seconds": seconds,
			}
			if scopesCSV != "" {
				scopes := []string{}
				for _, s := range strings.Split(scopesCSV, ",") {
					s = strings.TrimSpace(s)
					if s != "" {
						scopes = append(scopes, s)
					}
				}
				payload["scopes"] = scopes
			}

			var created apiTokenWithSecret
			if err := apiRequest(context.Background(), cfg, "POST", "/v1/user/tokens", payload, &created); err != nil {
				return fmt.Errorf("create token: %w", err)
			}

			// Stderr warning preserves clean stdout for piping into CI configs.
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "===================================================================")
			fmt.Fprintln(os.Stderr, "  STORE THIS TOKEN NOW. YOU WILL NOT SEE IT AGAIN.")
			fmt.Fprintln(os.Stderr, "===================================================================")
			fmt.Fprintf(os.Stderr, "  Token: %s\n", created.Token)
			fmt.Fprintln(os.Stderr, "===================================================================")
			fmt.Fprintln(os.Stderr, "")

			if jsonOut {
				return emitJSON(created)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "ID:      %s\n", created.ID)
			fmt.Fprintf(cmd.OutOrStdout(), "Name:    %s\n", created.Name)
			fmt.Fprintf(cmd.OutOrStdout(), "Created: %s\n", created.CreatedAt.Format(time.RFC3339))
			if created.ExpiresAt != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Expires: %s\n", created.ExpiresAt.Format(time.RFC3339))
			}
			if len(created.Scopes) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Scopes:  %s\n", strings.Join(created.Scopes, ","))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Human-readable token name (required)")
	cmd.Flags().StringVar(&expiresIn, "expires-in", "90d", "Token lifetime: 24h, 30d, 90d (default 90d)")
	cmd.Flags().StringVar(&scopesCSV, "scopes", "", "Comma-separated scope list (default: full account access)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON to stdout")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

// ----------------------------------------------------------------------------
// tokens revoke
// ----------------------------------------------------------------------------

func newTokensRevokeCommand(cfg *config.Config) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:     "revoke <token_id>",
		Aliases: []string{"rm", "delete"},
		Short:   "Revoke an API token",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !force && !confirm(fmt.Sprintf("Revoke token '%s'? Any CI runs using it will start failing immediately.", args[0])) {
				fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
				return nil
			}
			path := fmt.Sprintf("/v1/user/tokens/%s", args[0])
			if err := apiRequest(context.Background(), cfg, "DELETE", path, nil, nil); err != nil {
				return fmt.Errorf("revoke token: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Token '%s' revoked.\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	return cmd
}
