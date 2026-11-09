package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
)

// newOpsStoragePruneDetachedCommand deletes ORPHANED Longhorn volumes only.
// The server refuses a detached volume whose PV is still Bound (a hibernated
// database, an idle claim) and binds the apply to the reviewed dry-run with
// --expect-plan, so a volume that became claimed between review and apply is
// never deleted.
func newOpsStoragePruneDetachedCommand(cfg *config.Config) *cobra.Command {
	var flags operationFlags
	var expected string
	cmd := &cobra.Command{
		Use:   "prune-detached [volume]",
		Short: "Delete orphaned Longhorn volumes: detached with a Released PV or none (GA O-4)",
		Long: `Delete orphaned Longhorn volumes.

A volume is eligible only when it is detached AND its PV is Released (the claim
was deleted under a Retain class) or it has no PV. A detached volume whose PV is
still Bound -- a hibernated managed database, a restore-drill or backup claim,
a scaled-to-zero StatefulSet -- is refused and listed with the reason.

Without --apply this is a dry run that prints the eligible and refused volumes
and a plan_hash. Apply with the reviewed plan:

  enclii ops storage prune-detached [volume] --expect-plan <plan_hash> --apply --reason "..."`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			expected = strings.TrimSpace(expected)
			if flags.apply && expected == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--apply requires --expect-plan with the plan_hash of the reviewed dry run")}
			}
			if !flags.apply && expected != "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--expect-plan requires --apply")}
			}
			extra := map[string]string{}
			if len(args) == 1 {
				extra["target"] = args[0]
			}
			if expected != "" {
				extra["expected_plan"] = expected
			}
			return runOperation(cmd, cfg, opsPath("storage", "prune-detached"), "ops.storage.prune-detached", flags, extra)
		},
	}
	addOperationFlags(cmd, &flags)
	cmd.Flags().StringVar(&expected, "expect-plan", "", "plan_hash returned by the reviewed dry run (required with --apply)")
	return cmd
}
