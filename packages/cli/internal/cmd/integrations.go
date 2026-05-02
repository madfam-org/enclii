package cmd

import (
	"context"
	"fmt"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
)

// NewIntegrationsCommand creates the `enclii integrations` subtree. Today
// the platform exposes only the GitHub integration server-side, so the only
// child group is `integrations github …`. New providers will slot in here.
func NewIntegrationsCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "integrations",
		Short: "Manage third-party integrations (GitHub, …)",
		Long: `Manage third-party integrations.

Examples:
  enclii integrations github status
  enclii integrations github repos
  enclii integrations github branches owner repo
  enclii integrations github link --installation-id 12345
  enclii integrations github analyze owner repo
`,
	}
	cmd.AddCommand(newIntegrationsGitHubCommand(cfg))
	return cmd
}

// ----------------------------------------------------------------------------
// integrations github
// ----------------------------------------------------------------------------

func newIntegrationsGitHubCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "github",
		Short: "GitHub App integration commands",
	}
	cmd.AddCommand(newGitHubStatusCommand(cfg))
	cmd.AddCommand(newGitHubReposCommand(cfg))
	cmd.AddCommand(newGitHubBranchesCommand(cfg))
	cmd.AddCommand(newGitHubLinkCommand(cfg))
	cmd.AddCommand(newGitHubAnalyzeCommand(cfg))
	return cmd
}

func newGitHubStatusCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show GitHub App connection status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var resp struct {
				Connected      bool     `json:"connected"`
				Account        string   `json:"account,omitempty"`
				InstallationID int64    `json:"installation_id,omitempty"`
				Scopes         []string `json:"scopes,omitempty"`
			}
			if err := apiRequest(context.Background(), cfg, "GET", "/v1/integrations/github/status", nil, &resp); err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(resp)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Connected:       %t\n", resp.Connected)
			if resp.Account != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Account:         %s\n", resp.Account)
			}
			if resp.InstallationID != 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Installation ID: %d\n", resp.InstallationID)
			}
			if len(resp.Scopes) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Scopes:          %v\n", resp.Scopes)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

func newGitHubReposCommand(cfg *config.Config) *cobra.Command {
	var (
		page    int
		jsonOut bool
	)
	cmd := &cobra.Command{
		Use:   "repos",
		Short: "List repositories accessible to the GitHub App installation",
		RunE: func(cmd *cobra.Command, _ []string) error {
			params := map[string]string{}
			if page > 0 {
				params["page"] = strconv.Itoa(page)
			}
			var resp struct {
				Repos []struct {
					FullName      string `json:"full_name"`
					DefaultBranch string `json:"default_branch"`
					Private       bool   `json:"private"`
				} `json:"repos"`
			}
			path := "/v1/integrations/github/repos" + queryString(params)
			if err := apiRequest(context.Background(), cfg, "GET", path, nil, &resp); err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(resp)
			}
			if len(resp.Repos) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No repositories accessible.")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "FULL_NAME\tDEFAULT_BRANCH\tPRIVATE")
			for _, r := range resp.Repos {
				fmt.Fprintf(tw, "%s\t%s\t%t\n", r.FullName, r.DefaultBranch, r.Private)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().IntVar(&page, "page", 0, "Page number (1-based)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

func newGitHubBranchesCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "branches <owner> <repo>",
		Short: "List branches for a repository",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			owner, repo := args[0], args[1]
			var resp struct {
				Branches []struct {
					Name      string `json:"name"`
					Protected bool   `json:"protected"`
					SHA       string `json:"sha,omitempty"`
				} `json:"branches"`
			}
			path := fmt.Sprintf("/v1/integrations/github/repos/%s/%s/branches", owner, repo)
			if err := apiRequest(context.Background(), cfg, "GET", path, nil, &resp); err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(resp)
			}
			if len(resp.Branches) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No branches found.")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tPROTECTED\tSHA")
			for _, b := range resp.Branches {
				sha := b.SHA
				if len(sha) > 8 {
					sha = sha[:8]
				}
				fmt.Fprintf(tw, "%s\t%t\t%s\n", b.Name, b.Protected, sha)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

func newGitHubLinkCommand(cfg *config.Config) *cobra.Command {
	var (
		installationID int64
		jsonOut        bool
	)
	cmd := &cobra.Command{
		Use:   "link",
		Short: "Link a GitHub App installation to the current account",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if installationID == 0 {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--installation-id is required")}
			}
			payload := map[string]interface{}{"installation_id": installationID}
			var resp map[string]interface{}
			if err := apiRequest(context.Background(), cfg, "POST", "/v1/integrations/github/link", payload, &resp); err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(resp)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Linked installation %d.\n", installationID)
			return nil
		},
	}
	cmd.Flags().Int64Var(&installationID, "installation-id", 0, "GitHub App installation ID (required)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

func newGitHubAnalyzeCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "analyze <owner> <repo>",
		Short: "Analyze a repository for services and Dockerfiles",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			owner, repo := args[0], args[1]
			path := fmt.Sprintf("/v1/integrations/github/repos/%s/%s/analyze", owner, repo)
			var resp struct {
				Services []struct {
					Name       string `json:"name"`
					Path       string `json:"path"`
					Dockerfile string `json:"dockerfile,omitempty"`
					Language   string `json:"language,omitempty"`
				} `json:"services"`
				Dockerfiles []string `json:"dockerfiles,omitempty"`
			}
			if err := apiRequest(context.Background(), cfg, "POST", path, struct{}{}, &resp); err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(resp)
			}
			if len(resp.Services) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No services detected.")
			} else {
				tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
				fmt.Fprintln(tw, "NAME\tPATH\tLANGUAGE\tDOCKERFILE")
				for _, s := range resp.Services {
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", s.Name, s.Path, s.Language, s.Dockerfile)
				}
				_ = tw.Flush()
			}
			if len(resp.Dockerfiles) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "\nDockerfiles: %v\n", resp.Dockerfiles)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}
