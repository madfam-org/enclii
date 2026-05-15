package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// RawServiceSpec is used to parse the full YAML structure including source.git fields
type RawServiceSpec struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name    string `yaml:"name"`
		Project string `yaml:"project"`
	} `yaml:"metadata"`
	Spec struct {
		Build struct {
			Type       string            `yaml:"type"`
			Dockerfile string            `yaml:"dockerfile"`
			Context    string            `yaml:"context"`
			Buildpack  string            `yaml:"buildpack"`
			BuildArgs  map[string]string `yaml:"buildArgs"`
			Target     string            `yaml:"target"`
			Source     struct {
				Git struct {
					Repository string `yaml:"repository"`
					Branch     string `yaml:"branch"`
					Path       string `yaml:"path"`
					AutoDeploy bool   `yaml:"autoDeploy"`
				} `yaml:"git"`
			} `yaml:"source"`
		} `yaml:"build"`
	} `yaml:"spec"`
}

func NewServicesSyncCommand(cfg *config.Config) *cobra.Command {
	var dir string
	var dryRun bool
	var projectSlug string
	var ignoreProjectMismatch bool
	var reconcileExisting bool

	cmd := &cobra.Command{
		Use:   "services-sync",
		Short: "Sync service definitions from YAML files to Enclii",
		Long: `Reads service YAML files from a directory and registers missing services in Enclii.

This command respects the metadata.project field in service specs. Only services
whose metadata.project matches the --project flag will be synced. Services without
a metadata.project field will use the --project flag as default.

This command is useful for bootstrapping services from spec files or
maintaining service definitions as code.

By default, existing services are left untouched. Use --reconcile-existing when
the checked-in service specs should repair persisted metadata drift such as an
empty git_repo, stale app_path, or stale build_config.

Examples:
  # Sync services from a specs directory (only services matching project)
  enclii services-sync --dir specs/ --project enclii

  # Dry run to see what would be created
  enclii services-sync --dir specs/ --project enclii --dry-run

  # Force sync all services regardless of metadata.project
  enclii services-sync --dir specs/ --project enclii --ignore-project-mismatch

  # Repair existing service metadata drift from checked-in specs
  enclii services-sync --dir apps/app --project forgesight --reconcile-existing`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServicesSync(cfg, dir, projectSlug, dryRun, ignoreProjectMismatch, reconcileExisting)
		},
	}

	cmd.Flags().StringVar(&dir, "dir", ".", "Directory containing service YAML files")
	cmd.Flags().StringVar(&projectSlug, "project", "enclii", "Project slug to register services under")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be done without making changes")
	cmd.Flags().BoolVar(&ignoreProjectMismatch, "ignore-project-mismatch", false, "Sync all services regardless of metadata.project field")
	cmd.Flags().BoolVar(&reconcileExisting, "reconcile-existing", false, "Update existing service metadata when it differs from the checked-in spec")

	return cmd
}

func runServicesSync(cfg *config.Config, dir, projectSlug string, dryRun, ignoreProjectMismatch, reconcileExisting bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Create API client
	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)

	// Check API health
	fmt.Println("Checking API connection...")
	health, err := apiClient.Health(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to API: %w", err)
	}
	fmt.Printf("Connected to %s (version %s)\n\n", health.Service, health.Version)

	// Get existing services
	fmt.Printf("Fetching existing services for project '%s'...\n", projectSlug)
	existingServices, err := apiClient.ListServices(ctx, projectSlug)
	if err != nil {
		// If project doesn't exist, we might need to create it
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not found") {
			fmt.Printf("Warning: Project '%s' not found. Services will be created if project exists.\n", projectSlug)
			existingServices = []*types.Service{}
		} else {
			return fmt.Errorf("failed to list existing services: %w", err)
		}
	}

	existingMap := make(map[string]*types.Service)
	for _, svc := range existingServices {
		existingMap[svc.Name] = svc
	}
	fmt.Printf("   Found %d existing services\n\n", len(existingServices))

	// Read YAML files from directory
	fmt.Printf("Scanning '%s' for service YAML files...\n", dir)
	specs, err := readRawServiceSpecs(dir)
	if err != nil {
		return fmt.Errorf("failed to read service specs: %w", err)
	}
	fmt.Printf("   Found %d service specifications\n\n", len(specs))

	if len(specs) == 0 {
		fmt.Println("No service YAML files found. Nothing to sync.")
		return nil
	}

	// Filter specs by project (unless --ignore-project-mismatch is set)
	var filteredSpecs []*RawServiceSpec
	var skippedSpecs []*RawServiceSpec

	for _, s := range specs {
		specProject := s.Metadata.Project
		if specProject == "" {
			// No project specified in spec - use CLI flag as default
			filteredSpecs = append(filteredSpecs, s)
		} else if specProject == projectSlug || ignoreProjectMismatch {
			// Project matches or we're ignoring mismatches
			filteredSpecs = append(filteredSpecs, s)
		} else {
			// Project mismatch - skip this spec
			skippedSpecs = append(skippedSpecs, s)
		}
	}

	// Report skipped services
	if len(skippedSpecs) > 0 {
		fmt.Printf("Skipping %d services (project mismatch):\n", len(skippedSpecs))
		for _, s := range skippedSpecs {
			fmt.Printf("   - %s (metadata.project: %s != %s)\n", s.Metadata.Name, s.Metadata.Project, projectSlug)
		}
		fmt.Println()
		if !ignoreProjectMismatch {
			fmt.Println("Tip: Use --ignore-project-mismatch to sync these services anyway.")
			fmt.Println()
		}
	}

	specs = filteredSpecs
	if len(specs) == 0 {
		fmt.Println("No matching service specs found for this project. Nothing to sync.")
		return nil
	}

	fmt.Printf("Processing %d services for project '%s'...\n\n", len(specs), projectSlug)

	// Determine actions
	var toCreate []*RawServiceSpec
	var alreadyExists []*RawServiceSpec
	var toUpdate []*serviceReconcilePlan
	var driftedExisting []*serviceReconcilePlan

	for _, s := range specs {
		if existing, exists := existingMap[s.Metadata.Name]; exists {
			desired := rawSpecToService(s)
			changes := serviceReconcileChanges(existing, desired)
			if len(changes) > 0 {
				plan := &serviceReconcilePlan{Spec: s, Existing: existing, Desired: desired, Changes: changes}
				if reconcileExisting {
					toUpdate = append(toUpdate, plan)
				} else {
					driftedExisting = append(driftedExisting, plan)
				}
			}
			alreadyExists = append(alreadyExists, s)
		} else {
			toCreate = append(toCreate, s)
		}
	}

	// Print summary
	fmt.Println("Sync Summary:")
	fmt.Printf("   Already registered: %d\n", len(alreadyExists))
	for _, s := range alreadyExists {
		fmt.Printf("      - %s\n", s.Metadata.Name)
	}
	if len(driftedExisting) > 0 {
		fmt.Printf("   Drift detected: %d (--reconcile-existing not set)\n", len(driftedExisting))
		for _, plan := range driftedExisting {
			fmt.Printf("      - %s (%s)\n", plan.Spec.Metadata.Name, strings.Join(plan.Changes, ", "))
		}
	}
	fmt.Printf("   To be updated: %d\n", len(toUpdate))
	for _, plan := range toUpdate {
		fmt.Printf("      - %s (%s)\n", plan.Spec.Metadata.Name, strings.Join(plan.Changes, ", "))
	}
	fmt.Printf("   To be created: %d\n", len(toCreate))
	for _, s := range toCreate {
		fmt.Printf("      - %s\n", s.Metadata.Name)
	}
	fmt.Println()

	if len(toCreate) == 0 && len(toUpdate) == 0 {
		if len(driftedExisting) > 0 {
			fmt.Println("Existing service drift was detected. Re-run with --reconcile-existing to update persisted metadata.")
			return nil
		}
		fmt.Println("All services are already registered and aligned. Nothing to do.")
		return nil
	}

	if dryRun {
		fmt.Println("DRY RUN - No changes will be made")
		fmt.Println()
		for _, plan := range toUpdate {
			fmt.Printf("Would update service '%s':\n", plan.Spec.Metadata.Name)
			fmt.Printf("  ID: %s\n", plan.Existing.ID)
			fmt.Printf("  Changes: %s\n", strings.Join(plan.Changes, ", "))
			fmt.Printf("  Git Repo: %s\n", plan.Desired.GitRepo)
			fmt.Printf("  App Path: %s\n", plan.Desired.AppPath)
			fmt.Printf("  Build Type: %s\n", plan.Desired.BuildConfig.Type)
			fmt.Printf("  Auto Deploy: %t\n", plan.Desired.AutoDeploy)
			fmt.Println()
		}
		for _, s := range toCreate {
			fmt.Printf("Would create service '%s':\n", s.Metadata.Name)
			fmt.Printf("  Project: %s\n", projectSlug)
			fmt.Printf("  Git Repo: %s\n", getGitRepoFromSpec(s))
			fmt.Printf("  App Path: %s\n", s.Spec.Build.Source.Git.Path)
			fmt.Printf("  Build Type: %s\n", s.Spec.Build.Type)
			fmt.Printf("  Auto Deploy: %t\n", s.Spec.Build.Source.Git.AutoDeploy)
			fmt.Println()
		}
		return nil
	}

	if len(toUpdate) > 0 {
		fmt.Println("Updating existing services...")
		successCount := 0
		failCount := 0

		for _, plan := range toUpdate {
			fmt.Printf("   Updating '%s'... ", plan.Spec.Metadata.Name)
			_, err := apiClient.UpdateService(ctx, plan.Existing.ID.String(), plan.Desired)
			if err != nil {
				fmt.Printf("Failed: %v\n", err)
				failCount++
				continue
			}

			fmt.Printf("Updated (%s)\n", strings.Join(plan.Changes, ", "))
			successCount++
		}

		fmt.Println()
		fmt.Printf("Update results: %d updated, %d failed\n", successCount, failCount)

		if failCount > 0 {
			return fmt.Errorf("%d services failed to update", failCount)
		}
		fmt.Println()
	}

	// Create missing services
	successCount := 0
	failCount := 0

	if len(toCreate) > 0 {
		fmt.Println("Creating missing services...")

		for _, s := range toCreate {
			service := rawSpecToService(s)
			fmt.Printf("   Creating '%s'... ", s.Metadata.Name)

			createdService, err := apiClient.CreateService(ctx, projectSlug, service)
			if err != nil {
				fmt.Printf("Failed: %v\n", err)
				failCount++
				continue
			}

			fmt.Printf("Created (ID: %s)\n", createdService.ID)
			successCount++
		}

		fmt.Println()
		fmt.Printf("Create results: %d created, %d failed\n", successCount, failCount)
	}

	if failCount > 0 {
		return fmt.Errorf("%d services failed to create", failCount)
	}

	fmt.Println("\nSync complete! Services are now registered and ready for GitHub webhooks.")
	return nil
}

func readRawServiceSpecs(dir string) ([]*RawServiceSpec, error) {
	var specs []*RawServiceSpec

	// Check if directory exists
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("directory not found: %s", dir)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", dir)
	}

	// Walk directory looking for YAML files
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Only process .yaml and .yml files
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}

		// Skip non-service files (e.g., kustomization.yaml, etc.)
		filename := strings.ToLower(info.Name())
		if strings.Contains(filename, "kustomization") ||
			strings.Contains(filename, "patch") ||
			strings.Contains(filename, "secret") {
			return nil
		}

		// Try to parse as service spec
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Printf("   Warning: Skipping %s (cannot read file)\n", filepath.Base(path))
			return nil
		}

		decoder := yaml.NewDecoder(bytes.NewReader(data))
		for {
			var spec RawServiceSpec
			if err := decoder.Decode(&spec); err != nil {
				if err == io.EOF {
					break
				}
				fmt.Printf("   Warning: Skipping %s (not valid YAML)\n", filepath.Base(path))
				return nil
			}

			if spec.APIVersion == "" && spec.Kind == "" && spec.Metadata.Name == "" {
				continue
			}

			if !isEncliiServiceSpec(&spec) {
				continue
			}

			// Validate minimum required fields
			if spec.Metadata.Name == "" {
				fmt.Printf("   Warning: Skipping %s (missing metadata.name)\n", filepath.Base(path))
				continue
			}

			specCopy := spec
			specs = append(specs, &specCopy)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}

	return specs, nil
}

type serviceReconcilePlan struct {
	Spec     *RawServiceSpec
	Existing *types.Service
	Desired  *types.Service
	Changes  []string
}

func serviceReconcileChanges(existing, desired *types.Service) []string {
	var changes []string
	if desired.GitRepo != "" && existing.GitRepo != desired.GitRepo {
		changes = append(changes, "git_repo")
	}
	if existing.AppPath != desired.AppPath {
		changes = append(changes, "app_path")
	}
	if existing.AutoDeploy != desired.AutoDeploy {
		changes = append(changes, "auto_deploy")
	}
	if desired.AutoDeployBranch != "" && existing.AutoDeployBranch != desired.AutoDeployBranch {
		changes = append(changes, "auto_deploy_branch")
	}
	if desired.AutoDeployEnv != "" && existing.AutoDeployEnv != desired.AutoDeployEnv {
		changes = append(changes, "auto_deploy_env")
	}
	if desired.Type != "" && existing.Type != desired.Type {
		changes = append(changes, "type")
	}
	if desired.Region != "" && existing.Region != desired.Region {
		changes = append(changes, "region")
	}
	if !reflect.DeepEqual(existing.BuildConfig, desired.BuildConfig) {
		changes = append(changes, "build_config")
	}
	return changes
}

func isEncliiServiceSpec(spec *RawServiceSpec) bool {
	if spec.Kind != "Service" {
		return false
	}

	switch spec.APIVersion {
	case "enclii.dev/v1", "enclii.dev/v1alpha":
		return true
	default:
		return false
	}
}

func getGitRepoFromSpec(s *RawServiceSpec) string {
	if s.Spec.Build.Source.Git.Repository != "" {
		return s.Spec.Build.Source.Git.Repository
	}
	// Default for enclii monorepo
	return "https://github.com/madfam-org/enclii"
}

func rawSpecToService(s *RawServiceSpec) *types.Service {
	gitRepo := getGitRepoFromSpec(s)
	appPath := s.Spec.Build.Source.Git.Path
	autoDeploy := s.Spec.Build.Source.Git.AutoDeploy
	autoDeployBranch := s.Spec.Build.Source.Git.Branch
	if autoDeployBranch == "" {
		autoDeployBranch = "main"
	}

	buildType := s.Spec.Build.Type
	if buildType == "" {
		buildType = "auto"
	}

	return &types.Service{
		Name:             s.Metadata.Name,
		GitRepo:          gitRepo,
		AppPath:          appPath,
		AutoDeploy:       autoDeploy,
		AutoDeployBranch: autoDeployBranch,
		AutoDeployEnv:    "production",
		BuildConfig: types.BuildConfig{
			Type:       types.BuildType(buildType),
			Dockerfile: s.Spec.Build.Dockerfile,
			Context:    s.Spec.Build.Context,
			Buildpack:  s.Spec.Build.Buildpack,
			BuildArgs:  s.Spec.Build.BuildArgs,
			Target:     s.Spec.Build.Target,
		},
	}
}
