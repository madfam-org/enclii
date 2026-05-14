package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func NewReleasesCommand(cfg *config.Config) *cobra.Command {
	var limit int
	var showAll bool
	var serviceID string
	var projectSlug string

	cmd := &cobra.Command{
		Use:   "releases [service-name]",
		Short: "List releases (builds) for a service",
		Long: `Display releases/builds for a service, including build status and error messages.

This command shows the build history for a service, including:
- Build version and git SHA
- Build status (building, ready, failed)
- Error messages for failed builds

Examples:
  # Show releases for a service by name
  enclii releases switchyard-api

  # Show releases by service ID
  enclii releases --id 70c1bded-7f28-4438-87ff-393efffd3bad

  # Show all releases (not just last 10)
  enclii releases switchyard-api --all

  # Limit to specific number
  enclii releases switchyard-api -n 5

  # Scope name lookup to a specific project
  enclii releases switchyard-api --project enclii`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)

			var targetServiceID string

			// If service ID provided directly
			if serviceID != "" {
				targetServiceID = serviceID
			} else if len(args) > 0 {
				// Find service by name
				serviceName := args[0]
				lookupProject := projectSlug
				if lookupProject == "" {
					lookupProject = cfg.Project
				}

				resolvedID, resolvedLabel, err := resolveReleaseServiceID(ctx, apiClient, lookupProject, serviceName)
				if err != nil {
					return err
				}
				targetServiceID = resolvedID
				fmt.Printf("Resolved %s to %s\n", serviceName, resolvedLabel)
			} else {
				return fmt.Errorf("service name or --id required")
			}

			// Get releases
			releases, err := apiClient.ListReleases(ctx, targetServiceID)
			if err != nil {
				return fmt.Errorf("failed to list releases: %w", err)
			}

			if len(releases) == 0 {
				fmt.Println("No releases found for this service")
				return nil
			}

			// Apply limit
			displayReleases := releases
			if !showAll && limit > 0 && len(releases) > limit {
				displayReleases = releases[:limit]
			}

			fmt.Printf("Releases for service %s (%d total, showing %d):\n\n", targetServiceID, len(releases), len(displayReleases))

			for _, r := range displayReleases {
				// Format status with color
				statusIcon := ""
				statusColor := ""
				switch r.Status {
				case "ready":
					statusIcon = "✅"
					statusColor = "\033[32m" // Green
				case "failed":
					statusIcon = "❌"
					statusColor = "\033[31m" // Red
				case "building":
					statusIcon = "🔨"
					statusColor = "\033[33m" // Yellow
				default:
					statusIcon = "❓"
					statusColor = "\033[90m" // Gray
				}

				// Format time
				timeAgo := formatTimeAgo(r.CreatedAt)

				// Short SHA
				sha := r.GitSHA
				if len(sha) > 8 {
					sha = sha[:8]
				}

				fmt.Printf("%s %s%-8s\033[0m  %s  (git: %s)  %s\n",
					statusIcon,
					statusColor,
					r.Status,
					r.Version,
					sha,
					timeAgo,
				)

				// Show error message if failed
				if r.Status == "failed" && r.ErrorMessage != nil && *r.ErrorMessage != "" {
					// Indent and wrap error message
					errMsg := *r.ErrorMessage
					// Truncate long messages
					if len(errMsg) > 200 {
						errMsg = errMsg[:200] + "..."
					}
					// Replace newlines with indented newlines
					errMsg = strings.ReplaceAll(errMsg, "\n", "\n         ")
					fmt.Printf("         \033[31mError: %s\033[0m\n", errMsg)
				}
				fmt.Println()
			}

			// Summary
			var ready, failed, building int
			for _, r := range releases {
				switch r.Status {
				case "ready":
					ready++
				case "failed":
					failed++
				case "building":
					building++
				}
			}
			fmt.Printf("Summary: %d ready, %d failed, %d building\n", ready, failed, building)

			return nil
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "n", 10, "Number of releases to show")
	cmd.Flags().BoolVarP(&showAll, "all", "a", false, "Show all releases")
	cmd.Flags().StringVar(&serviceID, "id", "", "Service ID (alternative to name)")
	cmd.Flags().StringVarP(&projectSlug, "project", "p", "", "Project slug for service-name lookup (defaults to configured project; searches all projects if unset)")

	return cmd
}

func resolveReleaseServiceID(ctx context.Context, apiClient *client.APIClient, projectSlug, serviceName string) (string, string, error) {
	if projectSlug != "" {
		services, err := apiClient.ListServices(ctx, projectSlug)
		if err != nil {
			return "", "", fmt.Errorf("failed to list services for project %q: %w", projectSlug, err)
		}

		for _, svc := range services {
			if svc.Name == serviceName {
				return svc.ID.String(), fmt.Sprintf("%s/%s (%s)", projectSlug, svc.Name, svc.ID), nil
			}
		}

		return "", "", fmt.Errorf("service %q not found in project %q", serviceName, projectSlug)
	}

	projects, err := apiClient.ListProjects(ctx)
	if err != nil {
		return "", "", fmt.Errorf("failed to list projects for service lookup: %w", err)
	}

	type match struct {
		project string
		id      string
	}
	var matches []match
	for _, project := range projects {
		services, err := apiClient.ListServices(ctx, project.Slug)
		if err != nil {
			return "", "", fmt.Errorf("failed to list services for project %q: %w", project.Slug, err)
		}
		for _, svc := range services {
			if svc.Name == serviceName {
				matches = append(matches, match{project: project.Slug, id: svc.ID.String()})
			}
		}
	}

	switch len(matches) {
	case 0:
		return "", "", fmt.Errorf("service %q not found in any project", serviceName)
	case 1:
		return matches[0].id, fmt.Sprintf("%s/%s (%s)", matches[0].project, serviceName, matches[0].id), nil
	default:
		var labels []string
		for _, m := range matches {
			labels = append(labels, fmt.Sprintf("%s/%s (%s)", m.project, serviceName, m.id))
		}
		return "", "", fmt.Errorf("service name %q is ambiguous; use --project. Matches: %s", serviceName, strings.Join(labels, ", "))
	}
}

func formatTimeAgo(t time.Time) string {
	now := time.Now()
	diff := now.Sub(t)

	switch {
	case diff < time.Minute:
		return fmt.Sprintf("%ds ago", int(diff.Seconds()))
	case diff < time.Hour:
		return fmt.Sprintf("%dm ago", int(diff.Minutes()))
	case diff < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(diff.Hours()))
	default:
		days := int(diff.Hours() / 24)
		return fmt.Sprintf("%dd ago", days)
	}
}
