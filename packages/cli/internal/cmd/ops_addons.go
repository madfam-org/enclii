package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
)

func newOpsAddonsCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{Use: "addons", Short: "Managed database addon operations (platform rank)"}
	cmd.AddCommand(newOpsAddonsBackupCredentialCommand(cfg))
	return cmd
}

// newOpsAddonsBackupCredentialCommand provisions the object-store credential
// every managed-Postgres addon backs up with. Switchyard ensures the bucket,
// mints a token scoped to it only and merges it into Vault; no value is ever
// printed, and no human ever holds it.
func newOpsAddonsBackupCredentialCommand(cfg *config.Config) *cobra.Command {
	var flags operationFlags
	var expected string
	var rotate bool
	cmd := &cobra.Command{
		Use:   "backup-credential",
		Short: "Provision the addon backup bucket and its bucket-scoped R2 credential into Vault",
		Long: `Provision the object-store credential every managed-Postgres addon backs up with.

Switchyard ensures the bucket named by ENCLII_ADDON_BACKUP_DESTINATION_BASE
(adopting it when it exists), mints a Cloudflare token scoped to that bucket
only (Object Read & Write), merges the S3 pair into the Vault path of the
intake target enclii/addon-db-backups and force-syncs the
enclii-db-backup-credentials ExternalSecret. The credential is never returned
or printed.

Without --apply this is a dry run: it prints the plan (bucket create or adopt,
token mint, rotate or keep, the Vault keys) and a plan_hash, and writes nothing.
Apply with the reviewed plan, a reason and an idempotency key:

  enclii ops addons backup-credential --apply --expect-plan <plan_hash> \
    --idempotency-key <key> --reason "..."

When Vault already holds the credential the plan is "keep" and an apply writes
nothing. --rotate mints a new token; the previous access key id is reported for
revocation after the clusters have switched.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			expected = strings.TrimSpace(expected)
			if flags.apply && expected == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--apply requires --expect-plan with the plan_hash of the reviewed dry run")}
			}
			if !flags.apply && expected != "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--expect-plan requires --apply")}
			}
			if flags.apply && strings.TrimSpace(flags.idempotencyKey) == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--apply requires --idempotency-key")}
			}
			extra := map[string]string{}
			if expected != "" {
				extra["expected_plan"] = expected
			}
			if rotate {
				extra["rotate"] = "true"
			}
			return runOperation(cmd, cfg, opsPath("addons", "backup-credential"), "ops.addons.backup-credential", flags, extra)
		},
	}
	addOperationFlags(cmd, &flags)
	cmd.Flags().StringVar(&expected, "expect-plan", "", "plan_hash returned by the reviewed dry run (required with --apply)")
	cmd.Flags().BoolVar(&rotate, "rotate", false, "Mint a new token even though Vault already holds one (the dry run and the apply must both pass it)")
	return cmd
}
