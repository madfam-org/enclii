package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func NewOnboardEnsureCommand(cfg *config.Config) *cobra.Command {
	var (
		project      string
		manifestPath string
		namespace    string
		branch       string
		secretName   string
		dbName       string
		dbExtensions string
		gen          generatedCredentialFlags
	)

	cmd := &cobra.Command{
		Use:   "ensure --repo <org/repo>",
		Short: "Converge onboarding state for an already-onboarded project",
		Long: `Re-run high-value onboarding reconciliation for an existing project.

Use this to repair partial runtime state without raw kubectl:
  - namespace ensure
  - GHCR credential copy into the project namespace
  - ArgoCD application registration refresh
  - domain provisioning kick (from enclii.yaml)

It also converges generated credentials (see 'enclii onboard'): with
--generate-db-password, --app-role or --generate-secret, existing roles and
values are KEPT, missing ones are generated, and only --rotate-* replaces a
value. Nothing generated is printed. A partial or failed result exits non-zero.`,
		Example: `  enclii onboard ensure --repo madfam-org/coupler \
    --project coupler \
    --manifest-path k8s/overlays/production \
    --namespace coupler

  # Add a runtime app role to an existing project (generated password)
  enclii onboard ensure --repo madfam-org/pravara-mes --project pravara-mes \
    --secret-name pravara-secrets --db-name pravara \
    --app-role pravara_app --app-role-connection-limit 10`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, _ := cmd.Flags().GetString("repo")
			if repo == "" {
				return fmt.Errorf("--repo is required (e.g., madfam-org/coupler)")
			}
			return runOnboardEnsure(cfg, onboardEnsureOpts{
				repo:         repo,
				project:      project,
				manifestPath: manifestPath,
				namespace:    namespace,
				branch:       branch,
				secretName:   secretName, // pragma: allowlist secret -- flag/field names, not a value
				dbName:       dbName,
				dbExtensions: dbExtensions,
				gen:          gen,
			})
		},
	}

	cmd.Flags().String("repo", "", "GitHub repo in org/name format (required)")
	cmd.Flags().StringVar(&project, "project", "", "Project name (defaults to repo name)")
	cmd.Flags().StringVar(&manifestPath, "manifest-path", "k8s/overlays/production", "K8s manifest path in repo")
	cmd.Flags().StringVar(&namespace, "namespace", "", "Kubernetes namespace (defaults to project name)")
	cmd.Flags().StringVar(&branch, "branch", "main", "Branch to track")
	cmd.Flags().StringVar(&secretName, "secret-name", "", "K8s Secret name (default: <project>-credentials)")
	cmd.Flags().StringVar(&dbName, "db-name", "", "Postgres database (with --generate-db-password: provisioned; with --app-role: the role's database)") // pragma: allowlist secret -- flag/field names, not a value
	cmd.Flags().StringVar(&dbExtensions, "db-extensions", "", "Comma-separated Postgres extensions (with --generate-db-password)")
	gen.register(cmd.Flags())
	_ = cmd.MarkFlagRequired("repo")

	return cmd
}

type onboardEnsureOpts struct {
	repo         string
	project      string
	manifestPath string
	namespace    string
	branch       string
	secretName   string
	dbName       string
	dbExtensions string
	gen          generatedCredentialFlags
}

func runOnboardEnsure(cfg *config.Config, opts onboardEnsureOpts) error {
	ctx := context.Background()

	if err := opts.gen.validate(opts.dbName, false); err != nil {
		return err
	}

	if opts.project == "" {
		parts := strings.SplitN(opts.repo, "/", 2)
		if len(parts) == 2 {
			opts.project = parts[1]
		} else {
			opts.project = opts.repo
		}
	}
	if opts.namespace == "" {
		opts.namespace = opts.project
	}

	req := types.OnboardingRequest{
		RepoFullName: opts.repo,
		ProjectName:  opts.project,
		Namespace:    opts.namespace,
		ManifestPath: opts.manifestPath,
		SecretName:   opts.secretName, // pragma: allowlist secret -- flag/field names, not a value
	}
	if opts.branch != "" {
		req.Branch = &opts.branch
	}
	// ensure never prompts: the owner is provisioned here only with a
	// generated password.
	if opts.dbName != "" && opts.gen.generatesOwnerPassword() {
		req.ProvisionPostgres = &types.PostgresProvisionSpec{
			DatabaseName: opts.dbName,
			Extensions:   splitExtensions(opts.dbExtensions),
		}
	}
	if err := opts.gen.apply(&req, opts.dbName); err != nil {
		return err
	}

	fmt.Printf("Ensuring onboarding for %s (project %q, namespace %q)...\n", opts.repo, opts.project, opts.namespace)

	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)
	var result map[string]interface{}
	if err := apiClient.EnsureOnboarding(ctx, &req, &result); err != nil {
		return fmt.Errorf("onboard ensure failed: %w", err)
	}

	if !printOnboardEnsureResult(result) {
		return fmt.Errorf("onboard ensure did not complete (status %v) — see the failed steps above", result["status"])
	}
	return nil
}

// printOnboardEnsureResult renders the result and reports whether it
// completed. A partial result is not success: the caller exits non-zero, the
// same rule `enclii onboard` applies.
func printOnboardEnsureResult(result map[string]interface{}) bool {
	fmt.Println()
	if mode, ok := result["mode"]; ok {
		fmt.Printf("Mode:          %v\n", mode)
	}
	if status, ok := result["status"]; ok {
		fmt.Printf("Status:        %v\n", status)
	}
	if ns, ok := result["namespace"]; ok {
		fmt.Printf("Namespace:     %v\n", ns)
	}
	if app, ok := result["argocd_app"]; ok {
		fmt.Printf("ArgoCD app:    %v\n", app)
	}
	if commit, ok := result["argocd_commit"]; ok && commit != "" {
		fmt.Printf("ArgoCD commit: %v\n", commit)
	}

	if steps, ok := result["step_results"]; ok {
		if stepList, ok := steps.([]interface{}); ok && len(stepList) > 0 {
			fmt.Println()
			fmt.Println("Steps:")
			for _, s := range stepList {
				if m, ok := s.(map[string]interface{}); ok {
					line := fmt.Sprintf("  %v: %v", m["name"], m["status"])
					if detail, ok := m["detail"]; ok && detail != "" {
						line += fmt.Sprintf(" — %v", detail)
					}
					fmt.Println(line)
				}
			}
		}
	}

	if domains, ok := result["domains"]; ok {
		if domainList, ok := domains.([]interface{}); ok && len(domainList) > 0 {
			fmt.Println()
			fmt.Println("Domains:")
			for _, d := range domainList {
				fmt.Printf("  %v\n", d)
			}
		}
	}

	printGeneratedCredentials(os.Stdout, result)

	status := fmt.Sprintf("%v", result["status"])
	return status == "completed" || status == "<nil>"
}
