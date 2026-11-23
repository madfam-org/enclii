package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/ecosystemoidc"
)

// keychainGOOS and newKeychainWriter are what --keychain uses: runtime.GOOS
// and the login Keychain through /usr/bin/security. Only tests assign them, to
// reach the refusals after the macOS check and to fake the Keychain.
var (
	keychainGOOS      = runtime.GOOS
	newKeychainWriter = ecosystemoidc.NewMacOSKeychain
)

func newSecretsProvisionCommand(cfg *config.Config) *cobra.Command {
	var (
		platform        string
		all             bool
		reason          string
		registryPath    string
		organizationID  string
		rotateIfMissing bool
		dryRun          bool
		jsonOut         bool
		graceHours      int
		intakeProfile   string
		keychain        bool
	)

	cmd := &cobra.Command{
		Use:   "provision",
		Short: "Auto-provision ecosystem credentials (Janua OIDC → Vault)",
		Long: `Provision inter-platform OIDC credentials using your Enclii admin session.

Uses the Janua access token from ` + "`enclii login`" + ` (admin@madfam.io) to register or
reconcile OAuth clients, then writes OIDC material into Vault via Switchyard
secret intake. Secret values are never printed.

Since ADR-003 only the platform_admin operator may submit an intake, and that
is often not the Janua admin. --intake-profile names the stored login that
submits the intake while the active profile keeps the Janua calls. The run
checks the intake login first and changes nothing in Janua if it is refused.

Examples:
  export ENCLII_API_ENDPOINT=https://api.enclii.dev
  enclii login
  enclii secrets provision oidc --platform dhanam --reason "post-rebuild oidc"
  enclii secrets provision oidc --all --reason "ecosystem oidc sweep"
  enclii secrets provision oidc --platform dhanam --dry-run
  enclii secrets provision oidc --platform zavlo-cfdi-emitter --grace-hours 0 --reason "rotate"
  enclii --profile admin secrets provision oidc --platform cv-app --intake-profile default --reason "cv-app login"
  enclii --profile admin secrets provision oidc --platform ops-agents-karafiel-cogs --keychain --intake-profile default --reason "ops agents read karafiel cogs mcp"

--keychain (macOS, one --platform whose registry entry declares
local_delivery.macos_keychain) also files the client_id and client_secret in
this Mac's login Keychain after the Vault intake succeeds: generic-password
items <service>/client_id and <service>/client_secret, written through
/usr/bin/security on stdin and read back to verify. Only item names are
printed. If the Keychain write fails the pair is still in Vault, the command
exits non-zero, and the error names the recovery.`,
	}

	oidc := &cobra.Command{
		Use:   "oidc",
		Short: "Register Janua OAuth clients and intake OIDC secrets to Vault",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(reason) == "" {
				return fmt.Errorf("--reason is required")
			}
			if keychain {
				if all || strings.TrimSpace(platform) == "" {
					return fmt.Errorf("--keychain needs exactly one --platform")
				}
				if dryRun {
					return fmt.Errorf("--keychain is refused with --dry-run: a plan files nothing")
				}
				if err := ecosystemoidc.KeychainSupported(keychainGOOS); err != nil {
					return err
				}
			}
			if !all && strings.TrimSpace(platform) == "" {
				return fmt.Errorf("specify --platform NAME or --all")
			}
			if cmd.Flags().Changed("organization-id") && (all || strings.TrimSpace(registryPath) != "" || strings.TrimSpace(platform) == "") {
				return fmt.Errorf("--organization-id requires one --platform and the embedded registry; --all/--registry are refused")
			}
			// Unset keeps Janua's default grace (no body on the rotate call).
			var grace *int
			if cmd.Flags().Changed("grace-hours") {
				g := graceHours
				grace = &g
			}
			if err := ecosystemoidc.ValidateGraceHours(grace); err != nil {
				return err
			}
			if cfg.APIToken == "" {
				return fmt.Errorf("not authenticated — run `enclii login` as admin@madfam.io")
			}

			reg, err := ecosystemoidc.LoadRegistry(registryPath)
			if err != nil {
				return err
			}

			if cmd.Flags().Changed("organization-id") {
				reg, err = reg.WithOrganization(strings.TrimSpace(platform), organizationID)
				if err != nil {
					return err
				}
			}

			januaToken := cfg.APIToken
			if cfg.Credentials != nil && cfg.Credentials.AccessToken != "" {
				januaToken = cfg.Credentials.AccessToken
			}
			janua := ecosystemoidc.NewJanuaClient(januaToken, os.Getenv("JANUA_INTERNAL_API_KEY"))

			intakeToken := cfg.APIToken
			if cmd.Flags().Changed("intake-profile") && !dryRun {
				intakeToken, err = config.ProfileAPIToken(intakeProfile)
				if err != nil {
					return fmt.Errorf("--intake-profile: %w", err)
				}
			}
			apiClient := client.NewAPIClient(cfg.APIEndpoint, intakeToken)
			submitter := ecosystemoidc.HTTPIntakeSubmitter{
				Post: func(ctx context.Context, path string, body []byte) ([]byte, error) {
					return apiClient.PostRaw(ctx, path, bytes.NewReader(body))
				},
				Get: func(ctx context.Context, path string) ([]byte, error) { return apiClient.GetRaw(ctx, path) },
			}

			targets := []string{strings.TrimSpace(platform)}
			if all {
				targets = reg.PlatformIDs()
			}
			var keychainWriter ecosystemoidc.KeychainWriter
			if keychain {
				keychainWriter = newKeychainWriter()
			}

			// Validate the complete chosen set before the first provider effect.
			for _, id := range targets {
				if _, err := ecosystemoidc.ValidateProvision(reg, ecosystemoidc.ProvisionOptions{PlatformID: id, Reason: reason, DryRun: dryRun, GraceHours: grace, Keychain: keychainWriter}); err != nil {
					return err
				}
			}
			var results []ecosystemoidc.ProvisionResult
			for _, id := range targets {
				result, err := ecosystemoidc.ProvisionPlatform(cmd.Context(), reg, janua, submitter, ecosystemoidc.ProvisionOptions{
					PlatformID:      id,
					Reason:          reason,
					RotateIfMissing: rotateIfMissing,
					DryRun:          dryRun,
					GraceHours:      grace,
					Keychain:        keychainWriter,
				})
				if err != nil {
					var keychainErr *ecosystemoidc.KeychainDeliveryError
					if errors.As(err, &keychainErr) {
						// The intake was filed, not held: the error says so, says
						// what the Keychain holds, and names the recovery. --json
						// still gets the run's result (intake id, keychain_state).
						if jsonOut {
							fmt.Println(ecosystemoidc.FormatResultsJSON(append(results, result)))
						}
						return fmt.Errorf("%s: %w", id, err)
					}
					if result.IntakeID != "" {
						fmt.Fprintf(os.Stderr, "held intake_id=%s; no automatic retry\n", result.IntakeID)
					}
					if result.SessionIntakeID != "" {
						fmt.Fprintf(os.Stderr, "held session_intake_id=%s; no automatic retry\n", result.SessionIntakeID)
					}
					return fmt.Errorf("%s: %w", id, err)
				}
				results = append(results, result)
				if jsonOut {
					continue
				}
				intake := result.IntakeID
				if intake == "" && result.PublicLogin && result.IntakeTarget == "" {
					intake = "none (public login client — no secret to deliver)"
				}
				fmt.Fprintf(os.Stderr, "✓ %s client_id=%s created=%t intake=%s", id, result.JanuaClientID, result.Created, intake)
				if len(result.Reconciled) > 0 {
					fmt.Fprintf(os.Stderr, " reconciled=%s", strings.Join(result.Reconciled, ","))
				}
				if result.SessionIntakeID != "" {
					fmt.Fprintf(os.Stderr, " session_intake=%s", result.SessionIntakeID)
				}
				if len(result.KeychainItems) > 0 {
					fmt.Fprintf(os.Stderr, " keychain=%s", strings.Join(result.KeychainItems, ","))
				}
				fmt.Fprint(os.Stderr, rotationSummary(result))
				fmt.Fprintln(os.Stderr)
			}

			if jsonOut {
				fmt.Println(ecosystemoidc.FormatResultsJSON(results))
				return nil
			}
			intakes := 0
			for _, r := range results {
				if r.IntakeID != "" {
					intakes++
				}
			}
			switch {
			case dryRun:
				fmt.Fprintln(os.Stderr, "dry-run only — no Vault writes")
			case intakes == 0:
				fmt.Fprintln(os.Stderr, "no Vault intake — public login client(s) only; pin the client_id in the consumer's manifest")
			default:
				fmt.Fprintln(os.Stderr, "Vault intake complete — poll with: enclii secrets intake status <intake_id>")
			}
			return nil
		},
	}

	oidc.Flags().StringVar(&platform, "platform", "", "Platform id from ecosystem registry (e.g. dhanam)")
	oidc.Flags().StringVar(&organizationID, "organization-id", "", "Bind one embedded ordinary code profile to an operator-approved organization UUID; no membership or rebind")
	oidc.Flags().BoolVar(&all, "all", false, "Provision every platform in the registry")
	oidc.Flags().StringVar(&reason, "reason", "", "Audit reason (required)")
	oidc.Flags().StringVar(&registryPath, "registry", "", "Override ecosystem OIDC registry YAML")
	oidc.Flags().BoolVar(&rotateIfMissing, "rotate-secret", false, "Rotate Janua client secret when an existing client has no retrievable secret")
	oidc.Flags().BoolVar(&dryRun, "dry-run", false, "Plan Janua reconcile without Vault intake")
	oidc.Flags().BoolVar(&jsonOut, "json", false, "JSON output (no secret values)")
	oidc.Flags().StringVar(&intakeProfile, "intake-profile", "", "Stored login that submits the Vault intake (the platform_admin operator, ADR-003); the active profile keeps the Janua calls. Unset uses the active profile")
	oidc.Flags().BoolVar(&keychain, "keychain", false, "Also file the client_id and client_secret in this Mac's login Keychain after the Vault intake (one --platform declaring local_delivery.macos_keychain; macOS only; never with --dry-run)")
	oidc.Flags().IntVar(&graceHours, "grace-hours", 0, fmt.Sprintf("Grace period (0-%d hours) for previous secrets when a secret is rotated; 0 retires them immediately. Unset keeps Janua's default", ecosystemoidc.MaxGraceHours))
	oidc.AddCommand(newOIDCInspectCommand(cfg))
	oidc.AddCommand(newOIDCSIMSSOCommand(cfg))
	cmd.AddCommand(oidc)
	cmd.AddCommand(newSecretsProvisionKalyaFeedCommand(cfg))
	return cmd
}

// rotationSummary renders the non-secret rotation facts for the human line: the
// grace Janua applied and when the previous secrets expire, or, on a dry run,
// the grace that would be sent.
func rotationSummary(r ecosystemoidc.ProvisionResult) string {
	var b strings.Builder
	if r.RotatedSecret {
		b.WriteString(" rotated=true")
		if r.GracePeriodHours != nil {
			fmt.Fprintf(&b, " grace_hours=%d", *r.GracePeriodHours)
		}
		if r.OldSecretsExpireAt != "" {
			fmt.Fprintf(&b, " old_secrets_expire_at=%s", r.OldSecretsExpireAt)
		}
	}
	if r.PlannedGraceHours != nil {
		fmt.Fprintf(&b, " planned_grace_hours=%d", *r.PlannedGraceHours)
	}
	return b.String()
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

Switchyard generates independent 256-bit consumer credentials into Vault,
registers their hashes through kalya's native custody endpoint, and publishes:

  secret/crea-map  kalya_occupancy_feed_url, kalya_capacity_feed_url
  secret/nauta     kalya_feed_tokens (merged, so other tenants survive)

The token is never returned, never logged, and never reaches this machine.
Idempotent: consumers that already carry this tenant's properties are skipped
and nothing is generated. Pass --rotate with --idempotency-key to replace a
consumer credential deliberately. Failed phases resume from durable custody;
rotation retires the predecessor after the replacement has been observed.

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
