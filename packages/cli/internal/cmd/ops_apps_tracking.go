package cmd

import (
	"fmt"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/spf13/cobra"
)

// The target is fixed server-side (argocd/argocd-cm), so this command takes no
// namespace, project or service scope.
func newOpsAppsTrackingCommand(cfg *config.Config) *cobra.Command {
	var flags operationFlags
	var uid, resourceVersion string
	cmd := &cobra.Command{
		Use:   "tracking",
		Short: "Declare Argo CD's annotation tracking default in argocd-cm (dry-run by default)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if flags.apply && (uid == "" || resourceVersion == "") {
				return fmt.Errorf("apply requires --expect-uid and --expect-resource-version from the reviewed dry-run")
			}
			return runOperation(cmd, cfg, opsPath("apps", "tracking"), "ops.apps.tracking", flags,
				map[string]string{"expect_uid": uid, "expect_resource_version": resourceVersion})
		},
	}
	cmd.Flags().BoolVar(&flags.jsonOut, "json", false, "Emit machine-readable JSON")
	cmd.Flags().BoolVar(&flags.apply, "apply", false, "Write the declaration; without this flag, only a read-only plan is requested")
	cmd.Flags().StringVar(&flags.reason, "reason", "", "Audit reason (required with --apply)")
	cmd.Flags().StringVar(&flags.idempotencyKey, "idempotency-key", "", "Idempotency key for safe retries")
	cmd.Flags().StringVar(&uid, "expect-uid", "", "argocd-cm UID from the reviewed dry-run")
	cmd.Flags().StringVar(&resourceVersion, "expect-resource-version", "", "argocd-cm resourceVersion from the reviewed dry-run")
	return cmd
}
