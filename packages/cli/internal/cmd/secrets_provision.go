package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/ecosystemoidc"
)

func newSecretsProvisionCommand(cfg *config.Config) *cobra.Command {
	var (
		platform        string
		all             bool
		reason          string
		registryPath    string
		rotateIfMissing bool
		dryRun          bool
		jsonOut         bool
	)

	cmd := &cobra.Command{
		Use:   "provision",
		Short: "Auto-provision ecosystem credentials (Janua OIDC → Vault)",
		Long: `Provision inter-platform OIDC credentials using your Enclii admin session.

Uses the Janua access token from ` + "`enclii login`" + ` (admin@madfam.io) to register or
reconcile OAuth clients, then writes OIDC material into Vault via Switchyard
secret intake. Secret values are never printed.

Examples:
  export ENCLII_API_ENDPOINT=https://api.enclii.dev
  enclii login
  enclii secrets provision oidc --platform dhanam --reason "post-rebuild oidc"
  enclii secrets provision oidc --all --reason "ecosystem oidc sweep"
  enclii secrets provision oidc --platform dhanam --dry-run`,
	}

	oidc := &cobra.Command{
		Use:   "oidc",
		Short: "Register Janua OAuth clients and intake OIDC secrets to Vault",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(reason) == "" {
				return fmt.Errorf("--reason is required")
			}
			if !all && strings.TrimSpace(platform) == "" {
				return fmt.Errorf("specify --platform NAME or --all")
			}
			if cfg.APIToken == "" {
				return fmt.Errorf("not authenticated — run `enclii login` as admin@madfam.io")
			}

			reg, err := ecosystemoidc.LoadRegistry(registryPath)
			if err != nil {
				return err
			}

			januaToken := cfg.APIToken
			if cfg.Credentials != nil && cfg.Credentials.AccessToken != "" {
				januaToken = cfg.Credentials.AccessToken
			}
			janua := ecosystemoidc.NewJanuaClient(januaToken, os.Getenv("JANUA_INTERNAL_API_KEY"))

			apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)
			submitter := ecosystemoidc.HTTPIntakeSubmitter{
				Post: func(ctx context.Context, path string, body []byte) ([]byte, error) {
					return apiClient.PostRaw(ctx, path, bytes.NewReader(body))
				},
			}

			targets := []string{strings.TrimSpace(platform)}
			if all {
				targets = reg.PlatformIDs()
			}

			var results []ecosystemoidc.ProvisionResult
			for _, id := range targets {
				result, err := ecosystemoidc.ProvisionPlatform(cmd.Context(), reg, janua, submitter, ecosystemoidc.ProvisionOptions{
					PlatformID:      id,
					Reason:          reason,
					RotateIfMissing: rotateIfMissing,
					DryRun:          dryRun,
				})
				if err != nil {
					return fmt.Errorf("%s: %w", id, err)
				}
				results = append(results, result)
				if jsonOut {
					continue
				}
				fmt.Fprintf(os.Stderr, "✓ %s client_id=%s intake=%s", id, result.JanuaClientID, result.IntakeID)
				if result.SessionIntakeID != "" {
					fmt.Fprintf(os.Stderr, " session_intake=%s", result.SessionIntakeID)
				}
				fmt.Fprintln(os.Stderr)
			}

			if jsonOut {
				fmt.Println(ecosystemoidc.FormatResultsJSON(results))
				return nil
			}
			if dryRun {
				fmt.Fprintln(os.Stderr, "dry-run only — no Vault writes")
			} else {
				fmt.Fprintln(os.Stderr, "Vault intake complete — poll with: enclii secrets intake status <intake_id>")
			}
			return nil
		},
	}

	oidc.Flags().StringVar(&platform, "platform", "", "Platform id from ecosystem registry (e.g. dhanam)")
	oidc.Flags().BoolVar(&all, "all", false, "Provision every platform in the registry")
	oidc.Flags().StringVar(&reason, "reason", "", "Audit reason (required)")
	oidc.Flags().StringVar(&registryPath, "registry", "", "Override ecosystem OIDC registry YAML")
	oidc.Flags().BoolVar(&rotateIfMissing, "rotate-secret", true, "Rotate Janua client secret when an existing client has no retrievable secret")
	oidc.Flags().BoolVar(&dryRun, "dry-run", false, "Plan Janua reconcile without Vault intake")
	oidc.Flags().BoolVar(&jsonOut, "json", false, "JSON output (no secret values)")
	cmd.AddCommand(oidc)
	cmd.AddCommand(newSecretsProvisionKalyaFeedCommand(cfg))
	return cmd
}

// newSecretsProvisionKalyaFeedCommand sits beside `provision oidc` because that
// is where an operator looks for "get the ecosystem credential and file it".
//
// Unlike `provision oidc`, which drives Janua from the CLI using the operator's
// own session, every step of this one runs inside Switchyard: the CLI sends a
// tenant, a consumer list, and a reason, and gets back a plan or a result. The
// token is minted and filed server-side and is never on this machine.
func newSecretsProvisionKalyaFeedCommand(cfg *config.Config) *cobra.Command {
	var flags operationFlags
	var tenant string
	var consumers []string
	var kalyaOrigin string
	var rotate bool

	cmd := &cobra.Command{
		Use:   "kalya-feed",
		Short: "Mint a kalya standing-feed token and file it into its consumers' Vault paths",
		Long: `Provision the kalya occupancy/capacity standing-feed credential.

Switchyard reads kalya's internal API key from Vault, asks kalya to mint an
unscoped feed token for the tenant, and writes the consumer properties:

  secret/crea-map  kalya_occupancy_feed_url, kalya_capacity_feed_url
  secret/nauta     kalya_feed_tokens (merged, so other tenants survive)

The token is never returned, never logged, and never reaches this machine.
Idempotent: consumers that already carry this tenant's properties are skipped
and nothing is minted. Pass --rotate to replace a live token deliberately.

Examples:
  # Plan
  enclii secrets provision kalya-feed --tenant crea --consumers crea-map,nauta

  # Apply
  enclii secrets provision kalya-feed --tenant crea --consumers crea-map,nauta \
    --apply --reason "wire the crea standing feed"`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			extra := map[string]string{}
			if strings.TrimSpace(tenant) != "" {
				extra["tenant"] = strings.TrimSpace(tenant)
			}
			if len(consumers) > 0 {
				extra["consumers"] = strings.Join(consumers, ",")
			}
			if strings.TrimSpace(kalyaOrigin) != "" {
				extra["kalya_origin"] = strings.TrimSpace(kalyaOrigin)
			}
			if rotate {
				extra["rotate"] = "true"
			}
			return runOperation(cmd, cfg, opsPath("secrets", "provision-kalya-feed"), "ops.secrets.provision-kalya-feed", flags, extra)
		},
	}

	addOperationFlags(cmd, &flags)
	cmd.Flags().StringVar(&tenant, "tenant", "", "kalya tenant slug (e.g. crea); required")
	cmd.Flags().StringSliceVar(&consumers, "consumers", nil, "Consumers to provision: crea-map, nauta")
	cmd.Flags().StringVar(&kalyaOrigin, "kalya-origin", "", "kalya origin (default: kalya's verified service domain, else https://kalya.app)")
	cmd.Flags().BoolVar(&rotate, "rotate", false, "Mint a replacement token even when the consumers are already provisioned")
	return cmd
}
