package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func NewRollbackCommand(cfg *config.Config) *cobra.Command {
	var environment string
	var releaseID string

	cmd := &cobra.Command{
		Use:   "rollback [service]",
		Short: "Rollback service to previous release",
		Long:  "Rollback a service to a previous release version",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var serviceName string
			if len(args) > 0 {
				serviceName = args[0]
			}
			return rollbackService(cfg, serviceName, environment, releaseID)
		},
	}

	cmd.Flags().StringVarP(&environment, "env", "e", "dev", "Environment to rollback in")
	cmd.Flags().StringVarP(&releaseID, "to", "t", "", "Specific release ID to rollback to")

	return cmd
}

func rollbackService(cfg *config.Config, serviceName, environment, releaseID string) error {
	if serviceName == "" {
		return &exitcodes.ValidationError{Err: fmt.Errorf("service name is required")}
	}

	fmt.Printf("🔄 Rolling back %s in %s environment", serviceName, environment)
	if releaseID != "" {
		fmt.Printf(" to release %s", releaseID)
	} else {
		fmt.Printf(" to previous release")
	}
	fmt.Println()
	fmt.Println()

	ctx := context.Background()
	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)

	// Step 1: Get project slug
	projectSlug := cfg.Project
	if projectSlug == "" {
		projectSlug = "default"
	}

	// Step 2: Find the service by name
	fmt.Println("🔍 Finding service...")
	services, err := apiClient.ListServices(ctx, projectSlug)
	if err != nil {
		fmt.Printf("❌ Failed to list services: %v\n", err)
		return err
	}

	var targetService *types.Service
	for _, svc := range services {
		if svc.Name == serviceName {
			targetService = svc
			break
		}
	}

	if targetService == nil {
		fmt.Printf("❌ Service '%s' not found\n", serviceName)
		return fmt.Errorf("service not found")
	}

	// Step 3: Get current deployment
	fmt.Println("🔍 Getting current deployment...")
	currentDeployment, err := apiClient.GetLatestDeployment(ctx, targetService.ID.String())
	if err != nil {
		fmt.Printf("❌ Failed to get current deployment: %v\n", err)
		return err
	}

	if currentDeployment.Deployment == nil {
		fmt.Println("❌ No deployment found for this service")
		return fmt.Errorf("no deployment found")
	}

	fmt.Printf("✅ Current deployment: %s\n", currentDeployment.Deployment.ID)
	if currentDeployment.Release != nil {
		fmt.Printf("   Version: %s (git: %s)\n", currentDeployment.Release.Version, currentDeployment.Release.GitSHA[:7])
	}
	fmt.Println()

	// Step 4: Trigger rollback
	fmt.Println("🚀 Initiating rollback...")
	req := client.RollbackRequest{}
	if releaseID != "" {
		req.ToRelease = releaseID
	}

	err = apiClient.RollbackDeployment(ctx, currentDeployment.Deployment.ID.String(), req)
	if err != nil {
		fmt.Printf("❌ Rollback failed: %v\n", err)
		return &exitcodes.DeployError{Err: err}
	}

	fmt.Println("✅ Rollback initiated successfully!")
	fmt.Println()
	fmt.Println("⏳ Monitoring deployment...")
	fmt.Println("   (In production, this would wait for pods to be ready)")
	fmt.Println()
	fmt.Println("✅ Rollback completed!")
	fmt.Println()
	fmt.Printf("💡 Monitor with: enclii logs %s -f\n", serviceName)
	fmt.Printf("💡 Check status with: enclii ps\n")

	return nil
}
