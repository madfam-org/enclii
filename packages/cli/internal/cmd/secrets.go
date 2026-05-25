package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/spec"
)

// NewSecretsCommand creates the secrets management command with subcommands
func NewSecretsCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "secrets",
		Aliases: []string{"secret", "env"},
		Short:   "Manage service secrets and environment variables",
		Long: `Manage secrets and environment variables for your services.

Secrets are encrypted at rest and masked in API responses.
Environment variables are visible but can be marked as secrets.

Examples:
  # Set a secret
  enclii secrets set API_KEY=sk-xxx --secret

  # Set a regular environment variable
  enclii secrets set DEBUG=true

  # List all secrets for a service
  enclii secrets list

  # Delete a secret
  enclii secrets delete API_KEY

  # Set multiple variables at once
  enclii secrets set API_KEY=xxx DB_URL=postgres://... --secret`,
	}

	cmd.AddCommand(newSecretsSetCommand(cfg))
	cmd.AddCommand(newSecretsListCommand(cfg))
	cmd.AddCommand(newSecretsDeleteCommand(cfg))
	cmd.AddCommand(newSecretsGetCommand(cfg))
	cmd.AddCommand(newSecretsSyncCommand(cfg))
	cmd.AddCommand(newSecretsRotateCommand(cfg))

	return cmd
}

func newSecretsSyncCommand(cfg *config.Config) *cobra.Command {
	var flags operationFlags
	cmd := &cobra.Command{
		Use:   "sync EXTERNAL_SECRET",
		Short: "Refresh an ExternalSecret through Enclii",
		Long: `Refresh an ExternalSecret through Enclii's audited operator layer.

This replaces routine kubectl force-sync annotation patches. Without --apply,
the command requests a dry-run plan. With --apply, --reason is required and the
Switchyard API patches only reconciliation annotations; secret values are never
read or written by this command.

Examples:
  enclii secrets sync forgesight-secrets --namespace forgesight
  enclii secrets sync forgesight-secrets --namespace forgesight --apply --reason "provider path populated"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			extra := map[string]string{"target": strings.TrimSpace(args[0])}
			return runOperation(cmd, cfg, opsPath("secrets", "sync"), "ops.secrets.sync", flags, extra)
		},
	}
	addOperationFlags(cmd, &flags)
	return cmd
}

func newSecretsRotateCommand(cfg *config.Config) *cobra.Command {
	var flags operationFlags
	cmd := &cobra.Command{
		Use:   "rotate TARGET",
		Short: "Plan a secret rotation through Enclii",
		Long: `Plan a secret rotation through Enclii's audited operator layer.

Rotation is intentionally plan-first until the Vault writer and dual-consumer
cutover path are fully enabled server-side. Use this command to record the
target, scope, reason, and idempotency key in the Enclii operation contract
instead of documenting ad-hoc provider UI or kubectl steps.

Examples:
  enclii secrets rotate npm-madfam-token --namespace npm-registry
  enclii secrets rotate janua-jwt-signing-key --project janua --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			extra := map[string]string{"target": strings.TrimSpace(args[0])}
			return runOperation(cmd, cfg, opsPath("secrets", "rotate"), "ops.secrets.rotate", flags, extra)
		},
	}
	addOperationFlags(cmd, &flags)
	return cmd
}

// newSecretsSetCommand creates the 'secrets set' subcommand
func newSecretsSetCommand(cfg *config.Config) *cobra.Command {
	var isSecret bool
	var envName string
	var specFile string

	cmd := &cobra.Command{
		Use:   "set KEY=VALUE [KEY2=VALUE2 ...]",
		Short: "Set one or more secrets or environment variables",
		Long: `Set one or more secrets or environment variables for a service.

By default, variables are stored as plain environment variables.
Use --secret to mark them as secrets (encrypted at rest, masked in responses).

Examples:
  enclii secrets set API_KEY=sk-xxx --secret
  enclii secrets set DEBUG=true LOG_LEVEL=info
  enclii secrets set DATABASE_URL=postgres://... --secret --env production`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSecretsSet(cfg, args, isSecret, envName, specFile)
		},
	}

	cmd.Flags().BoolVarP(&isSecret, "secret", "s", false, "Mark as secret (encrypted, masked)")
	cmd.Flags().StringVarP(&envName, "env", "e", "", "Target environment (default: all environments)")
	cmd.Flags().StringVarP(&specFile, "file", "f", "service.yaml", "Path to service.yaml specification file")

	return cmd
}

// newSecretsListCommand creates the 'secrets list' subcommand
func newSecretsListCommand(cfg *config.Config) *cobra.Command {
	var envName string
	var specFile string
	var showAll bool
	var jsonOut bool
	var reveal bool

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List secrets and environment variables",
		Long: `List all secrets and environment variables for a service.

Secret values are masked by the API by default. Pass --reveal to call the
per-item reveal endpoint (logged for audit) and print actual values. Use
--json for stable machine-readable output (CI/CD, agents).

Examples:
  enclii secrets list
  enclii secrets list --env production
  enclii secrets list --json
  enclii secrets list --reveal       # values shown, audit-logged`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSecretsList(cfg, envName, specFile, showAll, jsonOut, reveal)
		},
	}

	cmd.Flags().StringVarP(&envName, "env", "e", "", "Filter by environment")
	cmd.Flags().StringVarP(&specFile, "file", "f", "service.yaml", "Path to service.yaml specification file")
	cmd.Flags().BoolVarP(&showAll, "all", "a", false, "Show all metadata")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	cmd.Flags().BoolVar(&reveal, "reveal", false, "Reveal secret values (calls audit-logged reveal endpoint per item)")

	return cmd
}

// newSecretsDeleteCommand creates the 'secrets delete' subcommand
func newSecretsDeleteCommand(cfg *config.Config) *cobra.Command {
	var specFile string
	var force bool

	cmd := &cobra.Command{
		Use:     "delete KEY [KEY2 ...]",
		Aliases: []string{"rm", "remove"},
		Short:   "Delete one or more secrets or environment variables",
		Long: `Delete one or more secrets or environment variables.

Examples:
  enclii secrets delete API_KEY
  enclii secrets delete API_KEY DATABASE_URL --force`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSecretsDelete(cfg, args, specFile, force)
		},
	}

	cmd.Flags().StringVarP(&specFile, "file", "f", "service.yaml", "Path to service.yaml specification file")
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")

	return cmd
}

// newSecretsGetCommand creates the 'secrets get' subcommand
func newSecretsGetCommand(cfg *config.Config) *cobra.Command {
	var specFile string
	var reveal bool

	cmd := &cobra.Command{
		Use:   "get KEY",
		Short: "Get a specific secret or environment variable",
		Long: `Get a specific secret or environment variable.

By default, secret values are masked. Use --reveal to show the actual value.
Revealing secrets is logged for audit purposes.

Examples:
  enclii secrets get API_KEY
  enclii secrets get API_KEY --reveal`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSecretsGet(cfg, args[0], specFile, reveal)
		},
	}

	cmd.Flags().StringVarP(&specFile, "file", "f", "service.yaml", "Path to service.yaml specification file")
	cmd.Flags().BoolVar(&reveal, "reveal", false, "Reveal secret value (logged for audit)")

	return cmd
}

// runSecretsSet implements the secrets set command
func runSecretsSet(cfg *config.Config, keyValues []string, isSecret bool, envName, specFile string) error {
	ctx := context.Background()

	// Parse service.yaml to get service info
	parser := spec.NewParser()
	serviceSpec, err := parser.ParseServiceSpec(specFile)
	if err != nil {
		return fmt.Errorf("failed to parse %s: %w", specFile, err)
	}

	// Create API client
	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)

	// Get service
	service, err := getServiceByName(ctx, apiClient, serviceSpec.Metadata.Project, serviceSpec.Metadata.Name)
	if err != nil {
		return fmt.Errorf("failed to find service: %w", err)
	}

	// Get environment ID if specified
	var envID *string
	if envName != "" {
		env, err := getEnvironmentByName(ctx, apiClient, serviceSpec.Metadata.Project, envName)
		if err != nil {
			return fmt.Errorf("failed to find environment %s: %w", envName, err)
		}
		id := env.ID.String()
		envID = &id
	}

	// Parse key=value pairs
	vars := make([]client.EnvVarRequest, 0, len(keyValues))
	for _, kv := range keyValues {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid format for %q, expected KEY=VALUE", kv)
		}
		vars = append(vars, client.EnvVarRequest{
			Key:      parts[0],
			Value:    parts[1],
			IsSecret: isSecret,
		})
	}

	// Create env vars (use bulk API)
	if len(vars) == 1 {
		_, err = apiClient.CreateEnvVar(ctx, service.ID.String(), vars[0], envID)
		if err != nil {
			return fmt.Errorf("failed to set %s: %w", vars[0].Key, err)
		}
		secretLabel := ""
		if isSecret {
			secretLabel = " (secret)"
		}
		fmt.Printf("✅ Set %s%s\n", vars[0].Key, secretLabel)
	} else {
		_, err = apiClient.BulkCreateEnvVars(ctx, service.ID.String(), vars, envID)
		if err != nil {
			return fmt.Errorf("failed to set environment variables: %w", err)
		}
		secretLabel := ""
		if isSecret {
			secretLabel = " as secrets"
		}
		fmt.Printf("✅ Set %d variables%s\n", len(vars), secretLabel)
	}

	// Hint about deployment
	fmt.Printf("💡 Run 'enclii deploy' to apply changes to your running service\n")

	return nil
}

// runSecretsList implements the secrets list command
func runSecretsList(cfg *config.Config, envName, specFile string, showAll, jsonOut, reveal bool) error {
	ctx := context.Background()

	// Parse service.yaml
	parser := spec.NewParser()
	serviceSpec, err := parser.ParseServiceSpec(specFile)
	if err != nil {
		return fmt.Errorf("failed to parse %s: %w", specFile, err)
	}

	// Create API client
	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)

	// Get service
	service, err := getServiceByName(ctx, apiClient, serviceSpec.Metadata.Project, serviceSpec.Metadata.Name)
	if err != nil {
		return fmt.Errorf("failed to find service: %w", err)
	}

	// Get environment ID if specified
	var envID *string
	if envName != "" {
		env, err := getEnvironmentByName(ctx, apiClient, serviceSpec.Metadata.Project, envName)
		if err != nil {
			return fmt.Errorf("failed to find environment %s: %w", envName, err)
		}
		id := env.ID.String()
		envID = &id
	}

	// List env vars
	envVars, err := apiClient.ListEnvVars(ctx, service.ID.String(), envID)
	if err != nil {
		return fmt.Errorf("failed to list environment variables: %w", err)
	}

	// If --reveal, replace masked values with real ones via the audit-logged endpoint.
	if reveal {
		for i := range envVars {
			if !envVars[i].IsSecret {
				continue
			}
			val, rerr := apiClient.RevealEnvVar(ctx, service.ID.String(), envVars[i].ID.String())
			if rerr != nil {
				return fmt.Errorf("failed to reveal %s: %w", envVars[i].Key, rerr)
			}
			envVars[i].Value = val
		}
		fmt.Fprintln(os.Stderr, "⚠️  Secret values revealed - this action has been logged")
	}

	if jsonOut {
		return emitSecretsJSON(envVars)
	}

	if len(envVars) == 0 {
		fmt.Println("No environment variables found")
		return nil
	}

	// Print as table
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if showAll {
		_, _ = fmt.Fprintln(w, "KEY\tVALUE\tSECRET\tENVIRONMENT\tUPDATED")
	} else {
		_, _ = fmt.Fprintln(w, "KEY\tVALUE\tSECRET")
	}

	for _, ev := range envVars {
		secretIcon := ""
		if ev.IsSecret {
			secretIcon = "🔒"
		}

		if showAll {
			envLabel := "all"
			if ev.EnvironmentID != nil {
				envLabel = ev.EnvironmentID.String()[:8] + "..."
			}
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				ev.Key, ev.Value, secretIcon, envLabel, ev.UpdatedAt.Format("2006-01-02 15:04"))
		} else {
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", ev.Key, ev.Value, secretIcon)
		}
	}

	_ = w.Flush()

	return nil
}

// emitSecretsJSON renders the env-var list as a stable JSON shape for agents.
func emitSecretsJSON(envVars []client.EnvVarResponse) error {
	type item struct {
		Key       string `json:"key"`
		Value     string `json:"value"`
		IsSecret  bool   `json:"is_secret"`
		UpdatedAt string `json:"updated_at"`
	}
	out := make([]item, 0, len(envVars))
	for _, ev := range envVars {
		out = append(out, item{
			Key:       ev.Key,
			Value:     ev.Value,
			IsSecret:  ev.IsSecret,
			UpdatedAt: ev.UpdatedAt.Format(time.RFC3339),
		})
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// runSecretsDelete implements the secrets delete command
func runSecretsDelete(cfg *config.Config, keys []string, specFile string, force bool) error {
	ctx := context.Background()

	// Parse service.yaml
	parser := spec.NewParser()
	serviceSpec, err := parser.ParseServiceSpec(specFile)
	if err != nil {
		return fmt.Errorf("failed to parse %s: %w", specFile, err)
	}

	// Create API client
	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)

	// Get service
	service, err := getServiceByName(ctx, apiClient, serviceSpec.Metadata.Project, serviceSpec.Metadata.Name)
	if err != nil {
		return fmt.Errorf("failed to find service: %w", err)
	}

	// Get all env vars to find IDs by key
	envVars, err := apiClient.ListEnvVars(ctx, service.ID.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to list environment variables: %w", err)
	}

	// Build key->ID map
	keyToID := make(map[string]string)
	for _, ev := range envVars {
		keyToID[ev.Key] = ev.ID.String()
	}

	// Confirm deletion if not forced
	if !force {
		fmt.Printf("About to delete: %s\n", strings.Join(keys, ", "))
		fmt.Print("Continue? [y/N]: ")
		var response string
		_, _ = fmt.Scanln(&response)
		if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
			fmt.Println("Aborted")
			return nil
		}
	}

	// Delete each key
	deletedCount := 0
	for _, key := range keys {
		id, found := keyToID[key]
		if !found {
			fmt.Printf("⚠️  %s not found, skipping\n", key)
			continue
		}

		err = apiClient.DeleteEnvVar(ctx, service.ID.String(), id)
		if err != nil {
			fmt.Printf("❌ Failed to delete %s: %v\n", key, err)
			continue
		}

		fmt.Printf("✅ Deleted %s\n", key)
		deletedCount++
	}

	if deletedCount > 0 {
		fmt.Printf("💡 Run 'enclii deploy' to apply changes to your running service\n")
	}

	return nil
}

// runSecretsGet implements the secrets get command
func runSecretsGet(cfg *config.Config, key, specFile string, reveal bool) error {
	ctx := context.Background()

	// Parse service.yaml
	parser := spec.NewParser()
	serviceSpec, err := parser.ParseServiceSpec(specFile)
	if err != nil {
		return fmt.Errorf("failed to parse %s: %w", specFile, err)
	}

	// Create API client
	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)

	// Get service
	service, err := getServiceByName(ctx, apiClient, serviceSpec.Metadata.Project, serviceSpec.Metadata.Name)
	if err != nil {
		return fmt.Errorf("failed to find service: %w", err)
	}

	// Get all env vars to find by key
	envVars, err := apiClient.ListEnvVars(ctx, service.ID.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to list environment variables: %w", err)
	}

	// Find the key
	var foundVar *client.EnvVarResponse
	for i, ev := range envVars {
		if ev.Key == key {
			foundVar = &envVars[i]
			break
		}
	}

	if foundVar == nil {
		return fmt.Errorf("environment variable %s not found", key)
	}

	// If secret and reveal requested, call reveal endpoint
	if foundVar.IsSecret && reveal {
		revealedValue, err := apiClient.RevealEnvVar(ctx, service.ID.String(), foundVar.ID.String())
		if err != nil {
			return fmt.Errorf("failed to reveal secret: %w", err)
		}
		fmt.Printf("%s=%s\n", key, revealedValue)
		fmt.Fprintf(os.Stderr, "⚠️  Secret revealed - this action has been logged\n")
	} else {
		fmt.Printf("%s=%s\n", key, foundVar.Value)
		if foundVar.IsSecret {
			fmt.Fprintf(os.Stderr, "💡 Use --reveal to see the actual value\n")
		}
	}

	return nil
}

// getServiceByName finds a service by project and name
func getServiceByName(ctx context.Context, apiClient *client.APIClient, projectSlug, serviceName string) (*client.ServiceInfo, error) {
	services, err := apiClient.ListServicesWithInfo(ctx, projectSlug)
	if err != nil {
		return nil, err
	}

	for _, svc := range services {
		if svc.Name == serviceName {
			return svc, nil
		}
	}

	return nil, fmt.Errorf("service %s not found in project %s", serviceName, projectSlug)
}

// getEnvironmentByName finds an environment by project and name
func getEnvironmentByName(ctx context.Context, apiClient *client.APIClient, projectSlug, envName string) (*client.EnvironmentInfo, error) {
	envs, err := apiClient.ListEnvironments(ctx, projectSlug)
	if err != nil {
		return nil, err
	}

	for _, env := range envs {
		if env.Name == envName {
			return env, nil
		}
	}

	return nil, fmt.Errorf("environment %s not found in project %s", envName, projectSlug)
}
