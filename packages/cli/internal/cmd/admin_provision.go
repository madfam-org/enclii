package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func newAdminProvisionCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "provision",
		Short: "Provision operator-managed infrastructure",
	}
	cmd.AddCommand(newAdminProvisionSecretsCommand(cfg))
	return cmd
}

func newAdminProvisionSecretsCommand(cfg *config.Config) *cobra.Command {
	var namespace string
	var secretName string
	var secretsFile string
	var force bool
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Create or update a Kubernetes Secret through Enclii",
		Long: `Create or update a Kubernetes Secret through Enclii's audited provisioning API.

The input file uses KEY=VALUE lines. Values are sent to the API and never
printed by this command unless the server response includes non-secret
metadata.

Examples:
  enclii admin provision secrets --namespace tulana --secret-name tulana-secrets --secrets-file janua.env --force
  enclii admin provision secrets --namespace tulana --secrets-file app.env --json --force`,
		RunE: func(cmd *cobra.Command, args []string) error {
			namespace = strings.TrimSpace(namespace)
			secretName = strings.TrimSpace(secretName)
			secretsFile = strings.TrimSpace(secretsFile)
			if namespace == "" {
				return fmt.Errorf("--namespace is required")
			}
			if secretsFile == "" {
				return fmt.Errorf("--secrets-file is required")
			}
			entries, err := parseEnvFile(secretsFile)
			if err != nil {
				return fmt.Errorf("parse secrets file: %w", err)
			}
			if len(entries) == 0 {
				return fmt.Errorf("secrets file has no KEY=VALUE entries")
			}
			if !force {
				target := secretName
				if target == "" {
					target = namespace + "-credentials"
				}
				ok, err := confirmDestructive(
					cmd.InOrStdin(),
					cmd.OutOrStdout(),
					fmt.Sprintf("Provision %d secret entrie(s) into %s/%s? [y/N] ", len(entries), namespace, target),
				)
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("aborted")
				}
			}

			apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)
			req := &types.ProvisionSecretsRequest{
				Namespace:  namespace,
				SecretName: secretName,
				Secrets:    entries,
			}
			var result map[string]interface{}
			if err := apiClient.ProvisionSecrets(context.Background(), req, &result); err != nil {
				return err
			}
			if jsonOut {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			}
			target := secretName
			if target == "" {
				target = namespace + "-credentials"
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Provisioned %d secret entrie(s) into %s/%s\n", len(entries), namespace, target)
			return nil
		},
	}

	cmd.Flags().StringVar(&namespace, "namespace", "", "Kubernetes namespace")
	cmd.Flags().StringVar(&secretName, "secret-name", "", "Kubernetes Secret name (default: <namespace>-credentials)")
	cmd.Flags().StringVar(&secretsFile, "secrets-file", "", "Path to KEY=VALUE env file")
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	_ = cmd.MarkFlagRequired("namespace")
	_ = cmd.MarkFlagRequired("secrets-file")
	return cmd
}
