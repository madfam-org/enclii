package cmd

import (
	"fmt"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/spf13/cobra"
	"strings"
)

func newOpsJobsSuspendCommand(cfg *config.Config) *cobra.Command {
	var flags operationFlags
	var uid, resourceVersion string
	cmd := &cobra.Command{Use: "suspend <cronjob>", Short: "Persist a namespace-specific service-job hold (dry-run by default)", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(flags.namespace) == "" || strings.TrimSpace(flags.project) == "" || strings.TrimSpace(flags.service) == "" {
			return fmt.Errorf("--namespace, --project and --service UUID are required")
		}
		if flags.apply && (uid == "" || resourceVersion == "") {
			return fmt.Errorf("apply requires --expect-uid and --expect-resource-version from the reviewed dry-run")
		}
		return runOperation(cmd, cfg, opsPath("jobs", "suspend"), "ops.jobs.suspend", flags, map[string]string{"target": args[0], "expect_uid": uid, "expect_resource_version": resourceVersion})
	}}
	addOperationFlags(cmd, &flags)
	cmd.Flags().StringVar(&uid, "expect-uid", "", "UID from the reviewed dry-run")
	cmd.Flags().StringVar(&resourceVersion, "expect-resource-version", "", "resourceVersion from the reviewed dry-run")
	return cmd
}
