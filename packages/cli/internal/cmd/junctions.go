package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// NewJunctionsCommand creates the junctions management command with subcommands
func NewJunctionsCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "junctions",
		Aliases: []string{"junction", "routes"},
		Short:   "Manage routing rules and ingress configuration",
		Long: `Manage routing rules, custom domains, and ingress configuration.

Junctions define how traffic reaches your services through domain mappings,
path-based routing, and TLS configuration.

Examples:
  # List all junctions
  enclii junctions list --project my-project

  # Add a custom domain route
  enclii junctions add api.example.com --service-id <uuid> \
    --project my-project --path /api --protocol https

  # Get junction details
  enclii junctions get <junction-id>

  # Delete a junction
  enclii junctions delete <junction-id>`,
	}

	cmd.AddCommand(newJunctionsListCommand(cfg))
	cmd.AddCommand(newJunctionsAddCommand(cfg))
	cmd.AddCommand(newJunctionsGetCommand(cfg))
	cmd.AddCommand(newJunctionsDeleteCommand(cfg))

	return cmd
}

// junctionsRequest makes an HTTP request to the Switchyard API.
// Used because the SDK client does not yet have Junction methods.
func junctionsRequest(cfg *config.Config, method, path string, body interface{}) (*http.Response, error) {
	return apiRequestResponse(context.Background(), cfg, method, path, body)
}

// junctionsDecodeOrError reads the response body and either decodes it into
// target or returns a formatted error if the status code indicates failure.
func junctionsDecodeOrError(resp *http.Response, target interface{}) error {
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(body))
	}

	if target != nil {
		return json.NewDecoder(resp.Body).Decode(target)
	}
	return nil
}

type junctionsListResponse struct {
	Junctions []types.Junction `json:"junctions"`
	Total     int              `json:"total"`
}

type junctionCreateResponse struct {
	Junction types.Junction `json:"junction"`
	Message  string         `json:"message"`
}

// --- junctions list ---

func newJunctionsListCommand(cfg *config.Config) *cobra.Command {
	var projectSlug string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all routing rules",
		Long: `List all junctions (routing rules) for a project.

Examples:
  enclii junctions list --project my-project`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJunctionsList(cfg, projectSlug, jsonOutput)
		},
	}

	cmd.Flags().StringVarP(&projectSlug, "project", "p", "", "Project slug (required)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Emit full machine-readable junction records")
	_ = cmd.MarkFlagRequired("project")

	return cmd
}

func runJunctionsList(cfg *config.Config, projectSlug string, jsonOutput bool) error {
	resp, err := junctionsRequest(cfg, http.MethodGet, fmt.Sprintf("/v1/projects/%s/junctions", projectSlug), nil)
	if err != nil {
		return fmt.Errorf("failed to list junctions: %w", err)
	}

	var listResp junctionsListResponse
	if err := junctionsDecodeOrError(resp, &listResp); err != nil {
		return err
	}
	junctions := listResp.Junctions

	if jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(listResp)
	}

	if len(junctions) == 0 {
		fmt.Println("No junctions found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tDOMAIN\tPATH\tPROTOCOL\tTLS\tCREATED")

	for _, j := range junctions {
		tlsStatus := "off"
		if j.TLS != nil && j.TLS.Enabled {
			tlsStatus = j.TLS.Issuer
			if tlsStatus == "" {
				tlsStatus = "on"
			}
		}

		path := j.Path
		if path == "" {
			path = "/"
		}

		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			j.ID.String(),
			j.Domain,
			path,
			j.Protocol,
			tlsStatus,
			j.CreatedAt.Format("2006-01-02"),
		)
	}

	_ = w.Flush()
	return nil
}

// --- junctions add ---

func newJunctionsAddCommand(cfg *config.Config) *cobra.Command {
	var (
		projectSlug string
		serviceID   string
		path        string
		protocol    string
	)

	cmd := &cobra.Command{
		Use:   "add <domain>",
		Short: "Add a routing rule for a domain",
		Long: `Add a junction (routing rule) to route a domain to a service.

Examples:
  enclii junctions add api.example.com --service-id <uuid> --project my-project
  enclii junctions add app.example.com --service-id <uuid> --project my-project \
    --path /app --protocol https`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJunctionsAdd(cfg, args[0], projectSlug, serviceID, path, protocol)
		},
	}

	cmd.Flags().StringVarP(&projectSlug, "project", "p", "", "Project slug (required)")
	cmd.Flags().StringVar(&serviceID, "service-id", "", "Service ID (required)")
	cmd.Flags().StringVar(&path, "path", "/", "Path prefix for routing")
	cmd.Flags().StringVar(&protocol, "protocol", "https", "Protocol: http, https, grpc")

	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("service-id")

	return cmd
}

func runJunctionsAdd(cfg *config.Config, domain, projectSlug, serviceID, path, protocol string) error {
	payload := map[string]interface{}{
		"domain":     domain,
		"service_id": serviceID,
		"path":       path,
		"protocol":   protocol,
	}

	resp, err := junctionsRequest(cfg, http.MethodPost, fmt.Sprintf("/v1/projects/%s/junctions", projectSlug), payload)
	if err != nil {
		return fmt.Errorf("failed to add junction: %w", err)
	}

	var createResp junctionCreateResponse
	if err := junctionsDecodeOrError(resp, &createResp); err != nil {
		return err
	}
	junction := createResp.Junction

	fmt.Printf("Junction created:\n")
	fmt.Printf("  ID:       %s\n", junction.ID)
	fmt.Printf("  Domain:   %s\n", junction.Domain)
	fmt.Printf("  Path:     %s\n", junction.Path)
	fmt.Printf("  Protocol: %s\n", junction.Protocol)

	if junction.TLS != nil && junction.TLS.Enabled {
		fmt.Printf("  TLS:      %s\n", junction.TLS.Issuer)
	}

	return nil
}

// --- junctions get ---

func newJunctionsGetCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <junction-id>",
		Short: "Get junction details",
		Long: `Get detailed information about a junction.

Examples:
  enclii junctions get <junction-id>`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJunctionsGet(cfg, args[0])
		},
	}

	return cmd
}

func runJunctionsGet(cfg *config.Config, junctionID string) error {
	resp, err := junctionsRequest(cfg, http.MethodGet, fmt.Sprintf("/v1/junctions/%s", junctionID), nil)
	if err != nil {
		return fmt.Errorf("failed to get junction: %w", err)
	}

	var junction types.Junction
	if err := junctionsDecodeOrError(resp, &junction); err != nil {
		return err
	}

	fmt.Printf("ID:        %s\n", junction.ID)
	fmt.Printf("Domain:    %s\n", junction.Domain)
	fmt.Printf("Path:      %s\n", junction.Path)
	fmt.Printf("Protocol:  %s\n", junction.Protocol)
	fmt.Printf("Service:   %s\n", junction.ServiceID)
	fmt.Printf("Project:   %s\n", junction.ProjectID)
	fmt.Printf("Created:   %s\n", junction.CreatedAt.Format(time.RFC3339))
	fmt.Printf("Updated:   %s\n", junction.UpdatedAt.Format(time.RFC3339))

	if junction.TLS != nil {
		fmt.Printf("\nTLS Configuration:\n")
		fmt.Printf("  Enabled:        %t\n", junction.TLS.Enabled)
		fmt.Printf("  Issuer:         %s\n", junction.TLS.Issuer)
		fmt.Printf("  Min Version:    %s\n", junction.TLS.MinVersion)
		fmt.Printf("  Force Redirect: %t\n", junction.TLS.ForceRedirect)
	}

	return nil
}

// --- junctions delete ---

func newJunctionsDeleteCommand(cfg *config.Config) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:     "delete <junction-id>",
		Aliases: []string{"rm", "remove"},
		Short:   "Delete a junction",
		Long: `Delete a junction (routing rule).

This removes the domain routing and any associated TLS certificates.

Examples:
  enclii junctions delete <junction-id>
  enclii junctions delete <junction-id> --force`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJunctionsDelete(cfg, args[0], force)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")

	return cmd
}

func runJunctionsDelete(cfg *config.Config, junctionID string, force bool) error {
	if !force {
		fmt.Printf("Are you sure you want to delete junction '%s'? This removes domain routing. [y/N]: ", junctionID)
		var confirm string
		_, _ = fmt.Scanln(&confirm)
		if confirm != "y" && confirm != "Y" && confirm != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	resp, err := junctionsRequest(cfg, http.MethodDelete, fmt.Sprintf("/v1/junctions/%s", junctionID), nil)
	if err != nil {
		return fmt.Errorf("failed to delete junction: %w", err)
	}

	if err := junctionsDecodeOrError(resp, nil); err != nil {
		return err
	}

	fmt.Printf("Junction '%s' deleted.\n", junctionID)
	return nil
}
