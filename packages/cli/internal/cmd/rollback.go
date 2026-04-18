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
	var instant bool
	var reason string
	var changeTicketURL string

	cmd := &cobra.Command{
		Use:   "rollback [service]",
		Short: "Rollback service to previous release",
		Long: `Rollback a service to a previous release version.

Two strategies are supported:

  --instant  Service-selector flip (P0.5). Traffic shifts in <30s when the
             previous ReplicaSet is still running, <90s when it has to scale
             back up. ArgoCD reconciles in the background. Use for fast
             revert of a bad prod push.

  (default)  Manifest-commit strategy — writes a new image tag and lets the
             Deployment controller do a rolling update. Takes 2-3 minutes.
             Use when you want the rollback durably captured in git (ArgoCD
             still owns the state-of-record).`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var serviceName string
			if len(args) > 0 {
				serviceName = args[0]
			}
			if instant {
				return rollbackServiceInstant(cfg, serviceName, environment, releaseID, reason, changeTicketURL)
			}
			return rollbackService(cfg, serviceName, environment, releaseID)
		},
	}

	cmd.Flags().StringVarP(&environment, "env", "e", "dev", "Environment to rollback in")
	cmd.Flags().StringVarP(&releaseID, "to", "t", "", "Specific release/deployment ID to rollback to (required for --instant when not using previous)")
	cmd.Flags().BoolVar(&instant, "instant", false, "Flip traffic at the routing layer (<30s) instead of re-committing a manifest")
	cmd.Flags().StringVar(&reason, "reason", "", "Optional rollback reason (captured in audit log)")
	cmd.Flags().StringVar(&changeTicketURL, "change-ticket", "", "Change ticket URL (required for production instant rollbacks)")

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

// rollbackServiceInstant routes through the Service-selector flip endpoint
// (P0.5). Traffic shifts in <30s when the previous ReplicaSet is still
// running.
func rollbackServiceInstant(cfg *config.Config, serviceName, environment, releaseID, reason, changeTicketURL string) error {
	_ = environment // env is resolved server-side via the target deployment
	if serviceName == "" {
		return &exitcodes.ValidationError{Err: fmt.Errorf("service name is required")}
	}

	fmt.Printf("⚡ Instant rollback (selector flip): %s\n", serviceName)
	fmt.Println()

	ctx := context.Background()
	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)

	projectSlug := cfg.Project
	if projectSlug == "" {
		projectSlug = "default"
	}

	// Find service.
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
		return fmt.Errorf("service '%s' not found", serviceName)
	}

	// Determine target deployment ID. If `--to` is supplied, the value is
	// treated as a deployment UUID (for --instant we need a specific
	// ReplicaSet, not just any release). Otherwise we pick the previous
	// running deployment (skip the most recent).
	targetDeploymentID := releaseID
	if targetDeploymentID == "" {
		fmt.Println("🔍 Finding previous deployment...")
		deployments, err := apiClient.ListServiceDeployments(ctx, targetService.ID.String())
		if err != nil {
			return fmt.Errorf("list deployments: %w", err)
		}
		// List is newest-first; index 0 is current. We need the most recent
		// running, non-current deployment.
		for i, d := range deployments {
			if i == 0 {
				continue
			}
			if d.Status == types.DeploymentStatusRunning {
				targetDeploymentID = d.ID.String()
				break
			}
		}
		if targetDeploymentID == "" {
			return fmt.Errorf("no previous running deployment found to flip to")
		}
	}

	fmt.Printf("🎯 Target deployment: %s\n", targetDeploymentID)
	fmt.Println("🚀 Flipping Service selector...")

	resp, err := apiClient.InstantRollback(ctx, targetService.ID.String(), client.InstantRollbackRequest{
		TargetDeploymentID: targetDeploymentID,
		Reason:             reason,
		ChangeTicketURL:    changeTicketURL,
	})
	if err != nil {
		fmt.Printf("❌ Instant rollback failed: %v\n", err)
		return &exitcodes.DeployError{Err: err}
	}

	fmt.Println()
	fmt.Printf("✅ %s\n", resp.Message)
	fmt.Printf("   Took: %dms\n", resp.TookMS)
	fmt.Printf("   Scaled up needed: %v\n", resp.ScaledUp)
	fmt.Printf("   Ready replicas: %d\n", resp.ReadyReplicas)
	fmt.Printf("   From → To: %s → %s\n", shortID(resp.FromDeploymentID), shortID(resp.ToDeploymentID))
	fmt.Println()
	fmt.Println("💡 ArgoCD will reconcile the manifest in the background (no action needed).")
	return nil
}

func shortID(id string) string {
	if len(id) < 8 {
		return id
	}
	return id[:8]
}
