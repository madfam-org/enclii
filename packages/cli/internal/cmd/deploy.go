package cmd

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
	"github.com/madfam-org/enclii/packages/cli/internal/spec"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func NewDeployCommand(cfg *config.Config) *cobra.Command {
	var environment string
	var wait bool
	var specFile string

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Build and deploy service",
		Long:  "Build the current service and deploy it to the specified environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			return deployService(cfg, environment, wait, specFile)
		},
	}

	cmd.Flags().StringVarP(&environment, "env", "e", "dev", "Environment to deploy to (dev, staging, prod)")
	cmd.Flags().BoolVarP(&wait, "wait", "w", false, "Wait for deployment to complete")
	cmd.Flags().StringVarP(&specFile, "file", "f", "service.yaml", "Path to service.yaml specification file")

	return cmd
}

func deployService(cfg *config.Config, environment string, wait bool, specFile string) error {
	ctx := context.Background()

	fmt.Printf("🚂 Deploying to %s environment...\n", environment)

	// Check if we're in a git repository and get current commit
	gitSHA, err := getCurrentGitSHA()
	if err != nil {
		return fmt.Errorf("failed to get git SHA: %w", err)
	}

	fmt.Printf("📦 Building from commit: %s\n", gitSHA[:8])

	// 1. Parse service.yaml
	parser := spec.NewParser()
	serviceSpec, err := parser.ParseServiceSpec(specFile)
	if err != nil {
		return fmt.Errorf("failed to parse %s: %w", specFile, err)
	}

	fmt.Printf("🔧 Service: %s (project: %s)\n", serviceSpec.Metadata.Name, serviceSpec.Metadata.Project)

	// 2. Create API client
	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)

	// 3. Ensure project exists
	project, err := ensureProject(ctx, apiClient, serviceSpec.Metadata.Project)
	if err != nil {
		return fmt.Errorf("failed to ensure project: %w", err)
	}

	// 4. Ensure service exists
	service, err := ensureService(ctx, apiClient, project, serviceSpec)
	if err != nil {
		return fmt.Errorf("failed to ensure service: %w", err)
	}

	// 5. Ensure environment exists
	if err := ensureEnvironment(ctx, apiClient, project.Slug, environment); err != nil {
		return fmt.Errorf("failed to ensure environment: %w", err)
	}

	// 6. Trigger build
	fmt.Println("🏗️  Building service...")
	release, err := apiClient.BuildService(ctx, service.ID.String(), gitSHA)
	if err != nil {
		return &exitcodes.BuildError{Err: fmt.Errorf("failed to build service: %w", err)}
	}

	fmt.Printf("📦 Build initiated: %s\n", release.Version)

	// 7. Wait for build completion (simplified polling)
	if err := waitForBuild(ctx, apiClient, service.ID.String(), release.ID.String()); err != nil {
		return &exitcodes.BuildError{Err: fmt.Errorf("build failed: %w", err)}
	}

	// 8. Deploy to environment
	fmt.Println("🚀 Deploying to Kubernetes...")
	deployReq := client.DeployRequest{
		ReleaseID:       release.ID.String(),
		EnvironmentName: environment, // e.g., "dev", "staging", "production"
		Replicas:        1,
	}

	_, err = apiClient.DeployService(ctx, service.ID.String(), deployReq)
	if err != nil {
		return &exitcodes.DeployError{Err: fmt.Errorf("failed to deploy service: %w", err)}
	}

	if wait {
		fmt.Println("⏳ Waiting for deployment...")
		if err := waitForDeployment(ctx, apiClient, service.ID.String()); err != nil {
			return &exitcodes.DeployError{Err: fmt.Errorf("deployment failed: %w", err)}
		}
		fmt.Println("✅ Deployment successful!")
		fmt.Printf("🌐 Service available at: https://%s.%s.%s.enclii.dev\n",
			serviceSpec.Metadata.Name, serviceSpec.Metadata.Project, environment)
	} else {
		fmt.Println("✅ Deployment initiated")
		fmt.Printf("📊 Monitor progress: enclii logs %s -f\n", serviceSpec.Metadata.Name)
	}

	return nil
}

func getCurrentGitSHA() (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func ensureProject(ctx context.Context, apiClient *client.APIClient, projectName string) (*types.Project, error) {
	// Try to get existing project
	project, err := apiClient.GetProject(ctx, projectName)
	if err == nil {
		return project, nil
	}

	// Create new project if not found (use errors.As to unwrap wrapped errors)
	var apiErr client.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
		fmt.Printf("📁 Creating project: %s\n", projectName)
		return apiClient.CreateProject(ctx, projectName, projectName)
	}

	return nil, err
}

func ensureService(ctx context.Context, apiClient *client.APIClient, project *types.Project, serviceSpec *types.ServiceSpec) (*types.Service, error) {
	// List existing services
	services, err := apiClient.ListServices(ctx, project.Slug)
	if err != nil {
		return nil, err
	}

	// Check if service already exists
	for _, svc := range services {
		if svc.Name == serviceSpec.Metadata.Name {
			return svc, nil
		}
	}

	// Create new service
	fmt.Printf("Creating service: %s\n", serviceSpec.Metadata.Name)

	// Map build type from spec to SDK type
	buildType := types.BuildTypeBuildpack // Default
	if serviceSpec.Spec.Build.Type == "dockerfile" {
		buildType = types.BuildTypeDockerfile
	}

	newService := &types.Service{
		ProjectID: project.ID,
		Name:      serviceSpec.Metadata.Name,
		GitRepo:   getCurrentGitRepo(),
		BuildConfig: types.BuildConfig{
			Type:       buildType,
			Dockerfile: serviceSpec.Spec.Build.Dockerfile,
		},
	}

	return apiClient.CreateService(ctx, project.Slug, newService)
}

func ensureEnvironment(ctx context.Context, apiClient *client.APIClient, projectSlug, envName string) error {
	// Try to create the environment (will fail with 409 if it already exists, which is fine)
	_, err := apiClient.CreateEnvironment(ctx, projectSlug, envName)
	if err != nil {
		// If error contains "409" or "already exists", it's fine
		errStr := err.Error()
		if strings.Contains(errStr, "409") || strings.Contains(errStr, "already exists") || strings.Contains(errStr, "Conflict") {
			// Environment already exists, that's fine
			return nil
		}
		return err
	}
	fmt.Printf("📦 Created environment: %s\n", envName)
	return nil
}

func getCurrentGitRepo() string {
	cmd := exec.Command("git", "config", "--get", "remote.origin.url")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func waitForBuild(ctx context.Context, apiClient *client.APIClient, serviceID, releaseID string) error {
	timeout := time.After(10 * time.Minute)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return &exitcodes.TimeoutError{Err: fmt.Errorf("build timeout after 10 minutes")}
		case <-ticker.C:
			releases, err := apiClient.ListReleases(ctx, serviceID)
			if err != nil {
				continue
			}

			for _, release := range releases {
				if release.ID.String() == releaseID {
					switch release.Status {
					case types.ReleaseStatusReady:
						fmt.Println("✅ Build completed successfully")
						return nil
					case types.ReleaseStatusFailed:
						return fmt.Errorf("build failed")
					case types.ReleaseStatusBuilding:
						fmt.Print(".")
						continue
					}
				}
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func waitForDeployment(ctx context.Context, apiClient *client.APIClient, serviceID string) error {
	timeout := time.After(5 * time.Minute)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return &exitcodes.TimeoutError{Err: fmt.Errorf("deployment timeout after 5 minutes")}
		case <-ticker.C:
			status, err := apiClient.GetServiceStatus(ctx, serviceID)
			if err != nil {
				continue
			}

			switch status.Health {
			case types.HealthStatusHealthy:
				return nil
			case types.HealthStatusUnhealthy:
				fmt.Print("⚠")
			default:
				fmt.Print(".")
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
