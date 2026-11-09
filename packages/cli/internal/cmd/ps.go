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

func NewPsCommand(cfg *config.Config) *cobra.Command {
	var environment string
	var project string

	cmd := &cobra.Command{
		Use:   "ps",
		Short: "List services and their status",
		Long:  "Show runtime health in one registered environment. Missing environment proof or services that are not ready return an error.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return listServices(cfg, environment, project)
		},
	}

	cmd.Flags().StringVarP(&environment, "env", "e", "dev", "Environment to list services for")
	cmd.Flags().StringVarP(&project, "project", "p", "",
		"Project slug (default: ENCLII_PROJECT, local service.yaml/.enclii.yml, or your only project)")

	return cmd
}

func listServices(cfg *config.Config, environment, projectFlag string) error {
	ctx := context.Background()
	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)

	projectSlug, err := resolveProjectSlug(ctx, apiClient, cfg, projectFlag)
	if err != nil {
		return err
	}

	fmt.Printf("📊 Services in %s environment (project: %s)\n", environment, projectSlug)
	fmt.Println()

	// Fetch services from API
	fmt.Println("🔍 Fetching services...")
	services, err := apiClient.ListServices(ctx, projectSlug)
	if err != nil {
		fmt.Printf("❌ Failed to list services: %v\n", err)
		return fmt.Errorf("list services in project %q: %w (run `enclii projects list` for valid slugs, or pass --project)", projectSlug, err)
	}

	environments, err := apiClient.ListRegisteredEnvironments(ctx, projectSlug)
	if err != nil {
		return fmt.Errorf("resolve environment %q: %w", environment, err)
	}
	selectedEnvironment, err := resolvePsEnvironment(environments, environment)
	if err != nil {
		return err
	}
	services, err = psServicesInEnvironment(services, selectedEnvironment)
	if err != nil {
		return err
	}
	fmt.Printf("Verified environment namespace: %s\n\n", selectedEnvironment.KubeNamespace)

	// Only native observations explicitly bound to this namespace can report
	// health. Historical rows and unscoped/older API responses cannot substitute.
	var runtimeHealth []client.ServiceHealth
	if healthResp, err := apiClient.GetServiceHealth(ctx, ""); err == nil && healthResp != nil {
		runtimeHealth = healthResp.Services
	}

	// Fetch deployment status for each service
	var serviceStatuses []ServiceStatus
	for _, svc := range services {
		status := ServiceStatus{
			Name:     svc.Name,
			Status:   "unknown",
			Health:   "unknown",
			Replicas: "unavailable",
			Version:  "N/A",
			Uptime:   "N/A",
		}

		// The latest project service deployment can belong to another environment.
		// Its health is never proof, and its metadata is shown only after binding.
		deploymentResp, err := apiClient.GetLatestDeployment(ctx, svc.ID.String())
		if err == nil && deploymentResp != nil && deploymentResp.Deployment != nil && deploymentResp.Deployment.EnvironmentID == selectedEnvironment.ID && deploymentResp.Release != nil && deploymentResp.Release.ServiceID == svc.ID {
			deployment := deploymentResp.Deployment

			// Get version from release
			if deploymentResp.Release != nil {
				status.Version = deploymentResp.Release.Version
				if len(deploymentResp.Release.GitSHA) >= 7 {
					status.Version += fmt.Sprintf(" (%s)", deploymentResp.Release.GitSHA[:7])
				}
			}

			// Calculate uptime
			uptime := time.Since(deployment.CreatedAt)
			status.Uptime = formatDuration(uptime)
		}

		if observation, ok := psRuntimeHealth(runtimeHealth, svc, projectSlug, selectedEnvironment); ok {
			applyRuntimeHealth(&status, observation)
		}

		serviceStatuses = append(serviceStatuses, status)
	}

	// Print header
	fmt.Printf("%-15s %-12s %-12s %-12s %-30s %-10s\n",
		"NAME", "STATUS", "HEALTH", "REPLICAS", "VERSION", "UPTIME")
	fmt.Println(strings.Repeat("─", 95))

	// Print services
	for _, svc := range serviceStatuses {
		statusColor := getStatusColor(svc.Status)
		healthColor := getHealthColor(svc.Health)

		fmt.Printf("%-15s %s%-12s%s %s%-12s%s %-12s %-30s %-10s\n",
			svc.Name,
			statusColor, svc.Status, "\033[0m",
			healthColor, svc.Health, "\033[0m",
			svc.Replicas, svc.Version, svc.Uptime)
	}

	fmt.Println()
	fmt.Printf("Total: %d service(s)\n", len(serviceStatuses))
	fmt.Println()
	fmt.Println("💡 Use 'enclii logs <service>' to view logs")
	fmt.Println("💡 Use 'enclii deploy --env <env>' to deploy updates")

	for _, status := range serviceStatuses {
		if status.Health != "healthy" {
			return fmt.Errorf("environment %q has services that are not ready or lack scoped runtime proof", environment)
		}
	}

	return nil
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		minutes := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	return fmt.Sprintf("%dd %dh", days, hours)
}

type ServiceStatus struct {
	Name     string
	Status   string
	Health   string
	Replicas string
	Version  string
	Uptime   string
}

func applyRuntimeHealth(status *ServiceStatus, health client.ServiceHealth) {
	if status == nil {
		return
	}
	if health.Status != "" {
		status.Health = health.Status
		switch {
		case status.Status == "unknown" && health.Status == "healthy":
			status.Status = "running"
		case status.Status == "unknown" && health.Status == "degraded":
			status.Status = "pending"
		case status.Status == "unknown" && health.Status == "unhealthy":
			status.Status = "failed"
		}
	}
	if health.Status == "unknown" {
		status.Replicas = "unavailable"
	} else if health.Status == "healthy" || health.Status == "degraded" || health.Status == "unhealthy" || health.PodCount > 0 || health.ReadyPods > 0 {
		status.Replicas = fmt.Sprintf("%d/%d", health.ReadyPods, health.PodCount)
	}
}

func getStatusColor(status string) string {
	switch status {
	case "running":
		return "\033[32m" // Green
	case "pending":
		return "\033[33m" // Yellow
	case "failed":
		return "\033[31m" // Red
	default:
		return "\033[37m" // White
	}
}

func getHealthColor(health string) string {
	switch health {
	case "healthy":
		return "\033[32m" // Green
	case "unhealthy":
		return "\033[31m" // Red
	default:
		return "\033[33m" // Yellow
	}
}
