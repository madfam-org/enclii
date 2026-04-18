package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/frameworks"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// starterTemplateRepo returns the GitHub repo slug for a starter
// template by framework slug. The actual template repos live under
// madfam-org/<framework-slug>-starter and are populated by P3.4.
// Returns "" when the slug has no starter convention (e.g. "unknown").
func starterTemplateRepo(slug string) string {
	if slug == "" || slug == "unknown" || slug == "auto" {
		return ""
	}
	return fmt.Sprintf("madfam-org/%s-starter", slug)
}

// knownTemplates returns the list of slugs a caller may pass to
// `enclii init --template`. Includes the Go catalog slugs plus two
// convenience aliases ("auto" / "docker").
func knownTemplates() []string {
	out := []string{"auto"}
	for _, fw := range frameworks.All() {
		if fw.Slug == "unknown" {
			continue
		}
		out = append(out, fw.Slug)
	}
	sort.Strings(out)
	return out
}

func NewInitCommand(cfg *config.Config) *cobra.Command {
	var templateName string

	cmd := &cobra.Command{
		Use:   "init [name]",
		Short: "Initialize a new Enclii project",
		Long: "Create a new service.yaml configuration file and optionally scaffold project structure. " +
			"Template slugs map to madfam-org/<slug>-starter repos and are sourced from the framework catalog.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validate template against the canonical catalog. "auto"
			// stays a valid sentinel for detection-time resolution.
			if templateName != "auto" && templateName != "" {
				if fw := frameworks.Get(templateName); fw == nil {
					return fmt.Errorf(
						"unknown template %q. Known templates:\n  %s",
						templateName,
						strings.Join(knownTemplates(), ", "),
					)
				}
			}

			var serviceName string
			if len(args) > 0 {
				serviceName = args[0]
			} else {
				// Default to current directory name
				wd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("failed to get working directory: %w", err)
				}
				serviceName = filepath.Base(wd)
			}

			// Get project name (could be different from service name)
			projectName := serviceName // For MVP, project and service names are the same

			return initializeService(serviceName, projectName, templateName)
		},
	}

	cmd.Flags().StringVarP(&templateName, "template", "t", "auto",
		"Framework slug (auto, nextjs, fastapi, go-fiber, …). Run `enclii init --help` to see the full catalog.")

	return cmd
}

func initializeService(serviceName, projectName, templateName string) error {
	fmt.Printf("🚂 Initializing Enclii service '%s'...\n", serviceName)

	// Check if service.yaml already exists
	serviceYamlPath := "service.yaml"
	if _, err := os.Stat(serviceYamlPath); err == nil {
		return fmt.Errorf("service.yaml already exists in current directory")
	}

	// Create service spec
	spec := &types.ServiceSpec{
		APIVersion: "enclii.dev/v1alpha",
		Kind:       "Service",
		Metadata: types.ServiceMetadata{
			Name:    serviceName,
			Project: projectName,
		},
		Spec: types.ServiceSpecConfig{
			Build: types.BuildSpec{
				Type: templateName,
			},
			Runtime: types.RuntimeSpec{
				Port:        detectPort(templateName),
				Replicas:    2,
				HealthCheck: "/health",
			},
			Env: []types.EnvVar{
				{
					Name:  "NODE_ENV",
					Value: "production",
				},
			},
		},
	}

	// Customize based on template
	switch templateName {
	case "node", "javascript", "typescript":
		spec.Spec.Runtime.Port = 3000
		spec.Spec.Env = []types.EnvVar{
			{Name: "NODE_ENV", Value: "production"},
			{Name: "PORT", Value: "3000"},
		}
	case "go":
		spec.Spec.Runtime.Port = 8080
		spec.Spec.Env = []types.EnvVar{
			{Name: "GO_ENV", Value: "production"},
			{Name: "PORT", Value: "8080"},
		}
	case "python":
		spec.Spec.Runtime.Port = 8000
		spec.Spec.Env = []types.EnvVar{
			{Name: "PYTHONENV", Value: "production"},
			{Name: "PORT", Value: "8000"},
		}
	default:
		// Auto-detect or use defaults
		spec.Spec.Runtime.Port = 8080
	}

	// Write service.yaml
	yamlData, err := yaml.Marshal(spec)
	if err != nil {
		return fmt.Errorf("failed to marshal service spec: %w", err)
	}

	if err := os.WriteFile(serviceYamlPath, yamlData, 0644); err != nil {
		return fmt.Errorf("failed to write service.yaml: %w", err)
	}

	fmt.Printf("✅ Created %s\n", serviceYamlPath)

	// Report the starter template hint when a catalog slug was provided.
	if repo := starterTemplateRepo(templateName); repo != "" {
		fmt.Printf("📦 Starter template: https://github.com/%s\n", repo)
	}

	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Printf("  1. Review and customize %s\n", serviceYamlPath)
	fmt.Println("  2. Run 'enclii deploy' to deploy to development")
	fmt.Println("  3. Run 'enclii deploy --env prod' to deploy to production")
	fmt.Println()
	fmt.Printf("💡 Learn more at https://enclii.dev/docs\n")

	return nil
}

// detectPort returns the idiomatic container port for a framework slug.
// Accepts both the new catalog slugs ("nextjs", "go-fiber", "fastapi", …)
// and legacy aliases ("node", "go", "gin", …) for backwards compatibility.
func detectPort(template string) int {
	slug := strings.ToLower(template)
	// Normalize legacy aliases to catalog slugs.
	switch slug {
	case "node", "javascript", "typescript", "react", "next":
		slug = "react"
	case "nuxt":
		slug = "nuxtjs"
	case "svelte":
		slug = "sveltekit"
	case "go":
		slug = "go-stdlib"
	case "gin":
		slug = "go-gin"
	case "echo":
		slug = "go-echo"
	case "fiber":
		slug = "go-fiber"
	case "chi":
		slug = "go-chi"
	case "python":
		slug = "fastapi"
	case "ruby":
		slug = "rails"
	case "sinatra":
		slug = "rails"
	}

	switch slug {
	case "nextjs", "remix", "nuxtjs", "sveltekit", "astro", "react", "vue",
		"express", "fastify", "nestjs", "rails":
		return 3000
	case "vite":
		return 4173
	case "angular":
		return 4200
	case "fastapi", "django":
		return 8000
	case "flask":
		return 5000
	case "go-stdlib", "go-gin", "go-fiber", "go-chi", "go-echo",
		"rust-actix", "rust-axum", "phoenix":
		return 8080
	default:
		return 8080 // Default port (java, php, unrecognized)
	}
}
