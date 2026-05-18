package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func NewOnboardCommand(cfg *config.Config) *cobra.Command {
	var (
		project      string
		manifestPath string
		branch       string
		secretName   string
		dbName       string
		dbPassword   string
		dbExtensions string
		secretsFile  string
		r2Bucket     string
		dryRun       bool
		preflight    bool
		skipPostgres bool
		skipSecrets  bool
		skipR2       bool
	)

	cmd := &cobra.Command{
		Use:   "onboard --repo <org/repo>",
		Short: "Onboard a new project with full provisioning",
		Long: `Onboard a new repository into the Enclii platform with automated provisioning.

This command handles the complete onboarding pipeline:
  - ArgoCD application registration (legacy Enclii config write until runtime reconciliation lands)
  - Namespace creation + GHCR credential copying
  - Domain provisioning (Cloudflare tunnel routes + DNS)
  - Postgres database + role creation (optional)
  - PgBouncer configuration update (optional)
  - K8s secret creation from .env file (optional)
  - R2 bucket creation (optional)`,
		Example: `  # Basic onboarding
  enclii onboard --repo madfam-org/karafiel --project karafiel

  # Full provisioning with database, secrets, and R2
  enclii onboard --repo madfam-org/karafiel \
    --project karafiel \
    --manifest-path infra/k8s/production \
    --db-name karafiel \
    --db-password "$(openssl rand -base64 32)" \
    --secrets-file ./karafiel.env \
    --r2-bucket karafiel-uploads

  # Dry run to preview what would be provisioned
  enclii onboard --repo madfam-org/karafiel \
    --db-name karafiel \
    --secrets-file ./karafiel.env \
    --dry-run`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, _ := cmd.Flags().GetString("repo")
			if repo == "" {
				return fmt.Errorf("--repo is required (e.g., madfam-org/karafiel)")
			}
			return runOnboard(cfg, onboardOpts{
				repo:         repo,
				project:      project,
				manifestPath: manifestPath,
				branch:       branch,
				secretName:   secretName,
				dbName:       dbName,
				dbPassword:   dbPassword,
				dbExtensions: dbExtensions,
				secretsFile:  secretsFile,
				r2Bucket:     r2Bucket,
				dryRun:       dryRun,
				preflight:    preflight,
				skipPostgres: skipPostgres,
				skipSecrets:  skipSecrets,
				skipR2:       skipR2,
			})
		},
	}

	cmd.Flags().String("repo", "", "GitHub repo in org/name format (required)")
	cmd.Flags().StringVar(&project, "project", "", "Project name (defaults to repo name)")
	cmd.Flags().StringVar(&manifestPath, "manifest-path", "k8s/production", "K8s manifest path in repo")
	cmd.Flags().StringVar(&branch, "branch", "main", "Branch to track")
	cmd.Flags().StringVar(&secretName, "secret-name", "", "K8s Secret name (default: <project>-credentials)")
	cmd.Flags().BoolVar(&preflight, "preflight", false, "Validate manifests against cluster before onboarding")
	cmd.Flags().StringVar(&dbName, "db-name", "", "Postgres database name to create")
	cmd.Flags().StringVar(&dbPassword, "db-password", "", "Postgres role password (prompted if --db-name set)")
	cmd.Flags().StringVar(&dbExtensions, "db-extensions", "", "Comma-separated Postgres extensions")
	cmd.Flags().StringVar(&secretsFile, "secrets-file", "", "Path to .env file with K8s secret entries")
	cmd.Flags().StringVar(&r2Bucket, "r2-bucket", "", "R2 bucket name to create")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be provisioned without executing")
	cmd.Flags().BoolVar(&skipPostgres, "skip-postgres", false, "Skip Postgres provisioning")
	cmd.Flags().BoolVar(&skipSecrets, "skip-secrets", false, "Skip secrets provisioning")
	cmd.Flags().BoolVar(&skipR2, "skip-r2", false, "Skip R2 provisioning")

	_ = cmd.MarkFlagRequired("repo")

	return cmd
}

type onboardOpts struct {
	repo         string
	project      string
	manifestPath string
	branch       string
	secretName   string
	dbName       string
	dbPassword   string
	dbExtensions string
	secretsFile  string
	r2Bucket     string
	dryRun       bool
	preflight    bool
	skipPostgres bool
	skipSecrets  bool
	skipR2       bool
}

func runOnboard(cfg *config.Config, opts onboardOpts) error {
	ctx := context.Background()

	// Derive project name from repo if not set
	if opts.project == "" {
		parts := strings.SplitN(opts.repo, "/", 2)
		if len(parts) == 2 {
			opts.project = parts[1]
		} else {
			opts.project = opts.repo
		}
	}

	// Build the onboarding request
	req := types.OnboardingRequest{
		RepoFullName: opts.repo,
		ProjectName:  opts.project,
		ManifestPath: opts.manifestPath,
		SecretName:   opts.secretName,
	}
	if opts.branch != "" {
		req.Branch = &opts.branch
	}

	// Postgres provisioning
	if opts.dbName != "" && !opts.skipPostgres {
		password := opts.dbPassword
		if password == "" {
			var err error
			password, err = promptPassword(fmt.Sprintf("Enter password for Postgres role %q: ", opts.dbName))
			if err != nil {
				return fmt.Errorf("failed to read password: %w", err)
			}
		}
		var extensions []string
		if opts.dbExtensions != "" {
			for _, ext := range strings.Split(opts.dbExtensions, ",") {
				ext = strings.TrimSpace(ext)
				if ext != "" {
					extensions = append(extensions, ext)
				}
			}
		}
		req.ProvisionPostgres = &types.PostgresProvisionSpec{
			DatabaseName: opts.dbName,
			RolePassword: password,
			Extensions:   extensions,
		}
	}

	// Secrets from .env file
	if opts.secretsFile != "" && !opts.skipSecrets {
		entries, err := parseEnvFile(opts.secretsFile)
		if err != nil {
			return fmt.Errorf("failed to parse secrets file: %w", err)
		}
		req.ProvisionSecrets = entries
	}

	// R2 bucket
	if opts.r2Bucket != "" && !opts.skipR2 {
		req.ProvisionR2 = &types.R2ProvisionSpec{
			BucketName: opts.r2Bucket,
		}
	}

	// Dry run
	if opts.dryRun {
		printDryRun(opts, req)
		return nil
	}

	// Execute
	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)

	// Preflight validation (opt-in)
	if opts.preflight {
		fmt.Printf("Running preflight validation for %s...\n", opts.repo)
		var pfResult types.PreflightResult
		if err := apiClient.PreflightOnboard(ctx, &req, &pfResult); err != nil {
			return fmt.Errorf("preflight check failed: %w", err)
		}
		if !pfResult.Pass {
			fmt.Printf("\nPreflight FAILED — %d violation(s):\n", len(pfResult.Violations))
			for _, v := range pfResult.Violations {
				fmt.Printf("  %s %s/%s: %s\n", v.File, v.Kind, v.Name, v.Message)
			}
			return fmt.Errorf("fix violations and retry")
		}
		fmt.Println("Preflight passed.")
	}

	fmt.Printf("Onboarding %s as project %q...\n", opts.repo, opts.project)

	var result map[string]interface{}
	if err := apiClient.OnboardProject(ctx, &req, &result); err != nil {
		return fmt.Errorf("onboarding failed: %w", err)
	}

	// Print summary
	printOnboardResult(result)

	return nil
}

func printDryRun(opts onboardOpts, req types.OnboardingRequest) {
	fmt.Println("=== DRY RUN — no changes will be made ===")
	fmt.Println()
	fmt.Printf("  Repo:          %s\n", opts.repo)
	fmt.Printf("  Project:       %s\n", opts.project)
	fmt.Printf("  Manifest path: %s\n", opts.manifestPath)
	fmt.Printf("  Branch:        %s\n", opts.branch)
	if opts.secretName != "" {
		fmt.Printf("  Secret name:   %s\n", opts.secretName)
	} else {
		fmt.Printf("  Secret name:   %s-credentials (default)\n", opts.project)
	}
	fmt.Println()

	fmt.Println("  Provisioning steps:")
	fmt.Println("    [1] Create project + ArgoCD registration (legacy Enclii config write)")
	fmt.Println("    [2] Create namespace + copy GHCR credentials")
	fmt.Println("    [3] Provision domains from enclii.yaml (if present)")

	if req.ProvisionPostgres != nil {
		fmt.Printf("    [4] Create Postgres database %q + role + PgBouncer update\n", req.ProvisionPostgres.DatabaseName)
		if len(req.ProvisionPostgres.Extensions) > 0 {
			fmt.Printf("        Extensions: %s\n", strings.Join(req.ProvisionPostgres.Extensions, ", "))
		}
	} else {
		fmt.Println("    [4] Postgres: skipped")
	}

	if len(req.ProvisionSecrets) > 0 {
		fmt.Printf("    [5] Create K8s secret with %d entries\n", len(req.ProvisionSecrets))
		for _, e := range req.ProvisionSecrets {
			fmt.Printf("        - %s\n", e.Key)
		}
	} else {
		fmt.Println("    [5] K8s secrets: skipped")
	}

	if req.ProvisionR2 != nil {
		fmt.Printf("    [6] Create R2 bucket %q\n", req.ProvisionR2.BucketName)
	} else {
		fmt.Println("    [6] R2 bucket: skipped")
	}

	fmt.Println()
	fmt.Println("Run without --dry-run to execute.")
}

func printOnboardResult(result map[string]interface{}) {
	fmt.Println()
	fmt.Println("Onboarding complete!")
	fmt.Println()

	if ns, ok := result["namespace"]; ok {
		fmt.Printf("  Namespace:     %v\n", ns)
	}
	if app, ok := result["argocd_app"]; ok {
		fmt.Printf("  ArgoCD app:    %v\n", app)
	}
	if commit, ok := result["argocd_commit"]; ok && commit != "" {
		fmt.Printf("  ArgoCD commit: %v\n", commit)
	}
	if db, ok := result["postgres_database"]; ok {
		fmt.Printf("  Database:      %v\n", db)
	}
	if count, ok := result["secrets_count"]; ok {
		fmt.Printf("  Secrets:       %v entries\n", count)
	}
	if bucket, ok := result["r2_bucket"]; ok {
		fmt.Printf("  R2 bucket:     %v\n", bucket)
	}

	if warnings, ok := result["provision_warnings"]; ok {
		if warnList, ok := warnings.([]interface{}); ok && len(warnList) > 0 {
			fmt.Println()
			fmt.Println("  Warnings:")
			for _, w := range warnList {
				fmt.Printf("    - %v\n", w)
			}
		}
	}

	if steps, ok := result["next_steps"]; ok {
		if stepList, ok := steps.([]interface{}); ok && len(stepList) > 0 {
			fmt.Println()
			fmt.Println("  Next steps:")
			for _, s := range stepList {
				fmt.Printf("    %v\n", s)
			}
		}
	}
}

// parseEnvFile reads a .env file and returns SecretEntry pairs.
// Supports KEY=VALUE format, skips comments (#) and blank lines.
func parseEnvFile(path string) ([]types.SecretEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var entries []types.SecretEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		// Strip surrounding quotes
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		if key != "" && value != "" {
			entries = append(entries, types.SecretEntry{Key: key, Value: value})
		}
	}
	return entries, scanner.Err()
}

// promptPassword reads a password from the terminal without echoing.
func promptPassword(prompt string) (string, error) {
	fmt.Print(prompt)
	password, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println() // newline after hidden input
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(password)), nil
}
