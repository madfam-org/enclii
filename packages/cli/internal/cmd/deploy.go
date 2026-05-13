package cmd

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
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

	// Canary flags (P2.7). If --canary is set, the deploy runs a canary
	// release rather than a rolling update.
	var canarySpec string
	var validationWindow string
	var smokeEndpoint string
	var changeTicketURL string

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Build and deploy service (optionally as a canary)",
		Long: `Build the current service and deploy it to the specified environment.

Default strategy: rolling update.

Canary strategy (P2.7):
  enclii deploy --canary=20% [--validation-window=10m] [--smoke-endpoint=...]

  Routes N% of traffic to the new digest via replica proportion, holds for the
  validation window, then auto-promotes if healthy or auto-rolls-back if not.
  Range: 5% — 50%. Requires service to have >= 2 replicas. Not supported for
  StatefulSets.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if canarySpec != "" {
				return deployServiceCanary(cfg, environment, specFile, canarySpec, validationWindow, smokeEndpoint, changeTicketURL)
			}
			return deployService(cfg, environment, wait, specFile)
		},
	}

	cmd.Flags().StringVarP(&environment, "env", "e", "dev", "Environment to deploy to (dev, staging, prod)")
	cmd.Flags().BoolVarP(&wait, "wait", "w", false, "Wait for deployment to complete")
	cmd.Flags().StringVarP(&specFile, "file", "f", "service.yaml", "Path to service.yaml specification file")

	// P2.7 canary rollout flags on `enclii deploy`.
	cmd.Flags().StringVar(&canarySpec, "canary", "", "Deploy as canary at this percentage (e.g. \"20%\" or \"20\"). Range 5-50.")
	cmd.Flags().StringVar(&validationWindow, "validation-window", "10m", "How long the canary must stay healthy before auto-promote (e.g. 10m, 30m)")
	cmd.Flags().StringVar(&smokeEndpoint, "smoke-endpoint", "", "Optional http(s) URL to probe during validation (returns 200 = healthy)")
	cmd.Flags().StringVar(&changeTicketURL, "change-ticket", "", "Change ticket URL (required for production canary rollouts)")

	// P2.6 subcommands: `enclii deploy ls` and `enclii deploy show v42`.
	// These are read-only views of deployment history keyed by the new
	// Heroku-style v-numbers.
	cmd.AddCommand(newDeployListCommand(cfg))
	cmd.AddCommand(newDeployShowCommand(cfg))

	return cmd
}

// newDeployListCommand implements `enclii deploy ls [service]`. Shows the
// deployment history with v-number as the primary column and the deployment
// UUID shortsha as secondary.
func newDeployListCommand(cfg *config.Config) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "ls [service]",
		Short: "List deployments with v-numbers",
		Long: `List a service's deployment history with Heroku-style v-numbers.

Output columns: v-number, status, deployment shortsha, created-at.
If service is omitted, uses the service configured in this directory's
service.yaml (if any).`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			serviceName := ""
			if len(args) > 0 {
				serviceName = args[0]
			}
			return runDeployList(cfg, serviceName, limit)
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "Max number of deployments to show")
	return cmd
}

func runDeployList(cfg *config.Config, serviceName string, limit int) error {
	ctx := context.Background()
	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)

	service, err := resolveOperationalService(ctx, apiClient, cfg, serviceName, "")
	if err != nil {
		return err
	}

	deployments, err := apiClient.ListServiceDeployments(ctx, service.ID)
	if err != nil {
		return fmt.Errorf("list deployments: %w", err)
	}

	if len(deployments) == 0 {
		fmt.Printf("No deployments for %s yet.\n", service.Name)
		return nil
	}

	if limit > 0 && limit < len(deployments) {
		deployments = deployments[:limit]
	}

	// Fixed-width columns keep the output parseable by scripts.
	fmt.Printf("%-8s %-12s %-12s %s\n", "VERSION", "STATUS", "DEPLOY-ID", "CREATED")
	for _, d := range deployments {
		version := "-"
		if d.VersionNumber != nil {
			version = d.VersionLabel()
		}
		fmt.Printf("%-8s %-12s %-12s %s\n",
			version,
			string(d.Status),
			shortID(d.ID.String()),
			d.CreatedAt.Format("2006-01-02 15:04:05"),
		)
	}
	return nil
}

// newDeployShowCommand implements `enclii deploy show v42 [service]`.
func newDeployShowCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <v{n}|uuid> [service]",
		Short: "Show a specific deployment by v-number or UUID",
		Long: `Show details for one deployment.

Target can be:
  v{n}    Heroku-style semantic version (e.g. v42). Requires [service].
  <uuid>  Full deployment UUID. Service argument is optional.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]
			serviceName := ""
			if len(args) > 1 {
				serviceName = args[1]
			}
			return runDeployShow(cfg, target, serviceName)
		},
	}
	return cmd
}

func runDeployShow(cfg *config.Config, target, serviceName string) error {
	ctx := context.Background()
	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)

	// v-label path: look up by (service, version).
	if n, ok := types.ParseVersionLabel(target); ok {
		service, err := resolveOperationalService(ctx, apiClient, cfg, serviceName, "")
		if err != nil {
			return err
		}
		dep, err := apiClient.GetDeploymentByVersion(ctx, service.ID, n)
		if err != nil {
			return err
		}
		printDeploymentDetail(dep, service.Name)
		return nil
	}

	// UUID path.
	if _, err := uuid.Parse(target); err != nil {
		return fmt.Errorf("target must be a v-label (e.g. v42) or a deployment UUID; got %q", target)
	}
	dep, err := apiClient.GetDeployment(ctx, target)
	if err != nil {
		return err
	}
	printDeploymentDetail(dep, serviceName)
	return nil
}

func printDeploymentDetail(d *types.Deployment, serviceName string) {
	version := "(no version allocated)"
	if d.VersionNumber != nil {
		version = d.VersionLabel()
	}
	fmt.Println("Deployment", version)
	if serviceName != "" {
		fmt.Printf("  Service:       %s\n", serviceName)
	}
	fmt.Printf("  ID:            %s\n", d.ID)
	fmt.Printf("  Status:        %s\n", d.Status)
	fmt.Printf("  Health:        %s\n", d.Health)
	fmt.Printf("  Replicas:      %d\n", d.Replicas)
	fmt.Printf("  Release:       %s\n", d.ReleaseID)
	fmt.Printf("  Environment:   %s\n", d.EnvironmentID)
	fmt.Printf("  Created:       %s\n", d.CreatedAt.Format(time.RFC3339))
	if d.ErrorMessage != nil && *d.ErrorMessage != "" {
		fmt.Printf("  Error:         %s\n", *d.ErrorMessage)
	}
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

// deployServiceCanary runs the canary-release deploy path. Builds the service
// as usual, then starts a canary rollout via POST /v1/services/:id/canary and
// tails the rollout until terminal (following the CLI `canary status -f`
// convention).
func deployServiceCanary(cfg *config.Config, environment, specFile, canarySpec, validationWindow, smokeEndpoint, changeTicketURL string) error {
	// Parse `20%` or `20` into int.
	pct, err := parseCanaryPercentage(canarySpec)
	if err != nil {
		return &exitcodes.ValidationError{Err: err}
	}

	// Parse validation window duration (e.g. "10m", "30m").
	win, err := time.ParseDuration(validationWindow)
	if err != nil {
		return &exitcodes.ValidationError{Err: fmt.Errorf("invalid --validation-window: %w", err)}
	}
	winMinutes := int(win.Minutes())
	if winMinutes < 1 {
		winMinutes = 1
	}

	ctx := context.Background()
	fmt.Printf("Canary deploy to %s: %d%% traffic, validation window %s\n", environment, pct, win)

	// Build the service to get a fresh digest (reuses the existing build flow).
	gitSHA, err := getCurrentGitSHA()
	if err != nil {
		return fmt.Errorf("get git sha: %w", err)
	}
	parser := spec.NewParser()
	serviceSpec, err := parser.ParseServiceSpec(specFile)
	if err != nil {
		return fmt.Errorf("parse %s: %w", specFile, err)
	}
	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)
	project, err := ensureProject(ctx, apiClient, serviceSpec.Metadata.Project)
	if err != nil {
		return err
	}
	service, err := ensureService(ctx, apiClient, project, serviceSpec)
	if err != nil {
		return err
	}
	if err := ensureEnvironment(ctx, apiClient, project.Slug, environment); err != nil {
		return err
	}

	fmt.Printf("Building candidate release from %s...\n", gitSHA[:8])
	release, err := apiClient.BuildService(ctx, service.ID.String(), gitSHA)
	if err != nil {
		return &exitcodes.BuildError{Err: fmt.Errorf("build: %w", err)}
	}
	if err := waitForBuild(ctx, apiClient, service.ID.String(), release.ID.String()); err != nil {
		return &exitcodes.BuildError{Err: err}
	}
	fmt.Printf("Build ready: release %s\n", release.ID)

	// Start the canary rollout.
	startReq := client.StartCanaryRequest{
		Digest:                  release.ID.String(), // API accepts release UUID or image digest
		Percentage:              pct,
		ValidationWindowMinutes: winMinutes,
		SmokeEndpoint:           smokeEndpoint,
		EnvironmentName:         environment,
		ChangeTicketURL:         changeTicketURL,
	}
	resp, err := apiClient.StartCanary(ctx, service.ID.String(), startReq)
	if err != nil {
		return &exitcodes.DeployError{Err: err}
	}
	fmt.Printf("Canary rollout started: %s (actual traffic share %.1f%%)\n", resp.ID, resp.ActualPercentage)
	fmt.Println("Tailing rollout until terminal state...")

	// Tail the rollout.
	rolloutID := resp.ID.String()
	serviceID := service.ID.String()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	var last types.CanaryRolloutState
	for {
		ro, err := apiClient.GetCanary(ctx, serviceID, rolloutID)
		if err != nil {
			return err
		}
		if ro.State != last {
			fmt.Printf("  [%s] state=%s canary=%d/%d\n", time.Now().Format("15:04:05"), ro.State, ro.CanaryReplicas, ro.TotalReplicas)
			last = ro.State
		}
		if ro.State.IsTerminal() {
			switch ro.State {
			case types.CanaryStateSucceeded:
				fmt.Println("Canary succeeded.")
				return nil
			case types.CanaryStateAutoRolledBack:
				return &exitcodes.DeployError{Err: fmt.Errorf("canary auto-rolled-back: %s", ro.RollbackReason)}
			case types.CanaryStateManualRolledBack:
				return &exitcodes.DeployError{Err: fmt.Errorf("canary manually rolled back: %s", ro.RollbackReason)}
			case types.CanaryStateFailed:
				return &exitcodes.DeployError{Err: fmt.Errorf("canary failed: %s", ro.LastError)}
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// parseCanaryPercentage accepts "20", "20%", or " 20 % " and returns the int.
func parseCanaryPercentage(s string) (int, error) {
	trimmed := strings.TrimSpace(s)
	trimmed = strings.TrimSuffix(trimmed, "%")
	trimmed = strings.TrimSpace(trimmed)
	pct, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, fmt.Errorf("invalid --canary percentage %q (use e.g. \"20%%\")", s)
	}
	if pct < 5 || pct > 50 {
		return 0, fmt.Errorf("--canary must be 5-50%% (got %d%%)", pct)
	}
	return pct, nil
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
