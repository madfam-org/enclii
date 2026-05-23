package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

type previewEnvironment struct {
	ID               string `json:"id"`
	ServiceID        string `json:"service_id"`
	PRNumber         int    `json:"pr_number"`
	PRTitle          string `json:"pr_title"`
	PRURL            string `json:"pr_url"`
	PRBranch         string `json:"pr_branch"`
	CommitSHA        string `json:"commit_sha"`
	PreviewURL       string `json:"preview_url"`
	Status           string `json:"status"`
	StatusMessage    string `json:"status_message"`
	PreviewSubdomain string `json:"preview_subdomain"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

type previewListResponse struct {
	Previews []previewEnvironment `json:"previews"`
	Count    int                  `json:"count"`
}

type previewSingleResponse struct {
	Preview previewEnvironment `json:"preview"`
}

// NewPreviewsCommand creates the preview environments command group.
func NewPreviewsCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "previews",
		Aliases: []string{"preview"},
		Short:   "Manage PR preview environments",
		Long: `Manage PR-based preview environments for a service.

Previews are usually created automatically when a pull request is opened
(GitHub webhook). Use these commands to list, inspect, wake, close, or delete
preview deployments from the CLI.

Examples:
  enclii previews list
  enclii previews list --service my-api --pr 42
  enclii previews get <preview-id>
  enclii previews close <preview-id>
  enclii previews wake <preview-id>`,
	}

	cmd.AddCommand(newPreviewsListCommand(cfg))
	cmd.AddCommand(newPreviewsGetCommand(cfg))
	cmd.AddCommand(newPreviewsCloseCommand(cfg))
	cmd.AddCommand(newPreviewsWakeCommand(cfg))
	cmd.AddCommand(newPreviewsDeleteCommand(cfg))

	return cmd
}

func newPreviewsListCommand(cfg *config.Config) *cobra.Command {
	var serviceName string
	var specFile string
	var prNumber int
	var showAll bool

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List preview environments for a service",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPreviewsList(cfg, serviceName, specFile, prNumber, showAll)
		},
	}

	cmd.Flags().StringVarP(&serviceName, "service", "s", "", "Service name (uses service.yaml if not specified)")
	cmd.Flags().StringVarP(&specFile, "file", "f", "service.yaml", "Path to service.yaml")
	cmd.Flags().IntVar(&prNumber, "pr", 0, "Filter to a single PR number")
	cmd.Flags().BoolVarP(&showAll, "all", "a", false, "Show branch, commit, and status message")

	return cmd
}

func newPreviewsGetCommand(cfg *config.Config) *cobra.Command {
	var serviceName string
	var specFile string
	var prNumber int

	cmd := &cobra.Command{
		Use:   "get PREVIEW_ID",
		Short: "Get preview environment details",
		Long: `Get preview environment details by ID, or use --pr with service context.

Examples:
  enclii previews get 00000000-0000-4000-8000-000000000001
  enclii previews get --pr 42 --service my-api`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			previewID := ""
			if len(args) > 0 {
				previewID = args[0]
			}
			return runPreviewsGet(cfg, previewID, serviceName, specFile, prNumber)
		},
	}

	cmd.Flags().StringVarP(&serviceName, "service", "s", "", "Service name")
	cmd.Flags().StringVarP(&specFile, "file", "f", "service.yaml", "Path to service.yaml")
	cmd.Flags().IntVar(&prNumber, "pr", 0, "Look up preview by PR number (requires service context)")

	return cmd
}

func newPreviewsCloseCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "close PREVIEW_ID",
		Short:   "Close a preview environment",
		Aliases: []string{"stop"},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPreviewsClose(cfg, args[0])
		},
	}
	return cmd
}

func newPreviewsWakeCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wake PREVIEW_ID",
		Short: "Wake a sleeping preview environment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPreviewsWake(cfg, args[0])
		},
	}
	return cmd
}

func newPreviewsDeleteCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete PREVIEW_ID",
		Short:   "Permanently delete a preview environment",
		Aliases: []string{"rm", "remove"},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPreviewsDelete(cfg, args[0])
		},
	}
	return cmd
}

func runPreviewsList(cfg *config.Config, serviceName, specFile string, prNumber int, showAll bool) error {
	ctx := context.Background()
	service, _, err := resolveService(ctx, cfg, serviceName, specFile)
	if err != nil {
		return err
	}

	path := fmt.Sprintf("/v1/services/%s/previews", service.ID.String())
	if prNumber > 0 {
		path += "?pr_number=" + strconv.Itoa(prNumber)
	}

	var resp previewListResponse
	if err := apiRequest(ctx, cfg, "GET", path, nil, &resp); err != nil {
		return fmt.Errorf("failed to list previews: %w", err)
	}

	if len(resp.Previews) == 0 {
		fmt.Println("No preview environments found")
		fmt.Println("💡 Open a pull request against the linked repo to create a preview automatically")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if showAll {
		_, _ = fmt.Fprintln(w, "PR\tSTATUS\tURL\tBRANCH\tCOMMIT")
	} else {
		_, _ = fmt.Fprintln(w, "PR\tSTATUS\tURL")
	}

	for _, p := range resp.Previews {
		url := p.PreviewURL
		if url == "" {
			url = "-"
		}
		if showAll {
			branch := p.PRBranch
			if branch == "" {
				branch = "-"
			}
			commit := shortSHA(p.CommitSHA)
			_, _ = fmt.Fprintf(w, "#%d\t%s\t%s\t%s\t%s\n", p.PRNumber, p.Status, url, branch, commit)
		} else {
			_, _ = fmt.Fprintf(w, "#%d\t%s\t%s\n", p.PRNumber, p.Status, url)
		}
	}
	_ = w.Flush()

	return nil
}

func runPreviewsGet(cfg *config.Config, previewID, serviceName, specFile string, prNumber int) error {
	ctx := context.Background()

	if previewID == "" && prNumber <= 0 {
		return fmt.Errorf("preview ID or --pr is required")
	}

	if previewID == "" {
		service, _, err := resolveService(ctx, cfg, serviceName, specFile)
		if err != nil {
			return err
		}
		path := fmt.Sprintf("/v1/services/%s/previews?pr_number=%d", service.ID.String(), prNumber)
		var resp previewListResponse
		if err := apiRequest(ctx, cfg, "GET", path, nil, &resp); err != nil {
			return fmt.Errorf("failed to get preview for PR #%d: %w", prNumber, err)
		}
		if len(resp.Previews) == 0 {
			return fmt.Errorf("no preview found for PR #%d", prNumber)
		}
		printPreviewDetails(resp.Previews[0])
		return nil
	}

	var resp previewSingleResponse
	if err := apiRequest(ctx, cfg, "GET", "/v1/previews/"+previewID, nil, &resp); err != nil {
		return fmt.Errorf("failed to get preview: %w", err)
	}
	printPreviewDetails(resp.Preview)
	return nil
}

func runPreviewsClose(cfg *config.Config, previewID string) error {
	ctx := context.Background()
	if err := apiRequest(ctx, cfg, "POST", "/v1/previews/"+previewID+"/close", map[string]any{}, nil); err != nil {
		return fmt.Errorf("failed to close preview: %w", err)
	}
	fmt.Printf("✅ Preview %s closed\n", previewID)
	return nil
}

func runPreviewsWake(cfg *config.Config, previewID string) error {
	ctx := context.Background()
	if err := apiRequest(ctx, cfg, "POST", "/v1/previews/"+previewID+"/wake", map[string]any{}, nil); err != nil {
		return fmt.Errorf("failed to wake preview: %w", err)
	}
	fmt.Printf("✅ Preview %s waking up\n", previewID)
	return nil
}

func runPreviewsDelete(cfg *config.Config, previewID string) error {
	ctx := context.Background()
	if err := apiRequest(ctx, cfg, "DELETE", "/v1/previews/"+previewID, nil, nil); err != nil {
		return fmt.Errorf("failed to delete preview: %w", err)
	}
	fmt.Printf("✅ Preview %s deleted\n", previewID)
	return nil
}

func printPreviewDetails(p previewEnvironment) {
	fmt.Printf("ID:       %s\n", p.ID)
	fmt.Printf("PR:       #%d %s\n", p.PRNumber, p.PRTitle)
	if p.PRURL != "" {
		fmt.Printf("PR URL:   %s\n", p.PRURL)
	}
	fmt.Printf("Status:   %s\n", p.Status)
	if p.StatusMessage != "" {
		fmt.Printf("Message:  %s\n", p.StatusMessage)
	}
	if p.PRBranch != "" {
		fmt.Printf("Branch:   %s\n", p.PRBranch)
	}
	if p.CommitSHA != "" {
		fmt.Printf("Commit:   %s\n", p.CommitSHA)
	}
	if p.PreviewURL != "" {
		fmt.Printf("URL:      %s\n", p.PreviewURL)
	}
	if p.PreviewSubdomain != "" {
		fmt.Printf("Subdomain: %s\n", p.PreviewSubdomain)
	}
	if p.CreatedAt != "" {
		fmt.Printf("Created:  %s\n", p.CreatedAt)
	}
	if p.UpdatedAt != "" {
		fmt.Printf("Updated:  %s\n", p.UpdatedAt)
	}
}

func shortSHA(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) >= 7 {
		return sha[:7]
	}
	if sha == "" {
		return "-"
	}
	return sha
}
