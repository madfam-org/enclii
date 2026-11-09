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
	cmd.AddCommand(newOpsAddonsCPURequestCommand(cfg))
	return cmd
}

// newOpsAddonsCPURequestCommand lowers one existing managed-Postgres addon's
// CPU request to the provisioning default and keeps its plan CPU as the limit.
// CNPG restarts the instance, so it is one addon per apply, at a time the
// operator chooses.
func newOpsAddonsCPURequestCommand(cfg *config.Config) *cobra.Command {
	var flags operationFlags
	var expected string
	cmd := &cobra.Command{
		Use:   "cpu-request <addon-uuid>",
		Short: "Lower one managed-Postgres addon's CPU request to the default, keeping its plan CPU as the limit",
		Long: `Lower one existing managed-Postgres addon's CPU request to the provisioning
default (100m) and keep its plan CPU as the limit. Addons created since
2026-11-04 already have that shape; this moves one created earlier, whose
request equals its limit.

CNPG restarts the instance when its resources change. A single-instance addon
is unavailable until the new pod is Ready, so run it per addon, at a quiet
time. The dry run says which restart applies (single instance, replicas and
switchover, supervised, or hibernated).

Without --apply this is a dry run: it prints the current and desired CPU, the
restart, and a plan_hash, and writes nothing. Apply with the reviewed plan:

  enclii ops addons cpu-request <addon-uuid> --project <slug> --apply \
    --expect-plan <plan_hash> --idempotency-key <key> --reason "..."

An addon already at the default reports succeeded and an apply writes nothing.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			expected = strings.TrimSpace(expected)
			if strings.TrimSpace(flags.project) == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--project (the addon's project slug) is required")}
			}
			if flags.apply && expected == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--apply requires --expect-plan with the plan_hash of the reviewed dry run")}
			}
			if !flags.apply && expected != "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--expect-plan requires --apply")}
			}
			if flags.apply && strings.TrimSpace(flags.idempotencyKey) == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--apply requires --idempotency-key")}
			}
			extra := map[string]string{"target": strings.TrimSpace(args[0])}
			if expected != "" {
				extra["expected_plan"] = expected
			}
			return runOperation(cmd, cfg, opsPath("addons", "cpu-request"), "ops.addons.cpu-request", flags, extra)
		},
	}
	addOperationFlags(cmd, &flags)
	cmd.Flags().StringVar(&expected, "expect-plan", "", "plan_hash returned by the reviewed dry run (required with --apply)")
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
