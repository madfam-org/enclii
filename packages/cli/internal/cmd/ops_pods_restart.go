package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
)

// newOpsPodsRestartCommand is `enclii ops pods restart`: an audited rollout
// restart of a Deployment, StatefulSet or DaemonSet, or the recreation of one
// controller-owned pod. The server enforces the platform rank, the namespace
// allowlist and the audit row; the checks here only fail fast.
func newOpsPodsRestartCommand(cfg *config.Config) *cobra.Command {
	var flags operationFlags
	var expectUID string
	cmd := &cobra.Command{
		Use:   "restart <kind>/<name>",
		Short: "Rollout-restart a workload, or recreate one controller-owned pod (dry-run by default)",
		Long: `Restart a workload through Enclii instead of kubectl.

  deployment/<name>, statefulset/<name>, daemonset/<name>
      Rollout restart: the same patch as kubectl rollout restart (the pod
      template annotation kubectl.kubernetes.io/restartedAt). Short kinds
      deploy, sts and ds are accepted.

  pod/<name>
      Delete one pod so its ReplicaSet, StatefulSet or DaemonSet recreates it,
      with the pod's own grace period. Use this for a StatefulSet pod that is
      crash-looping on an old revision: under OrderedReady the controller never
      replaces it, so a rollout restart stalls behind it. Pods without such a
      controller are refused.

Requires the platform_admin rank. The namespace must be in the API's
ENCLII_OPS_RESTART_NAMESPACES allowlist; kube-* namespaces are always refused.
One exception: in an Enclii project namespace (project-<id>), pod/<name> may
recreate a CloudNativePG instance pod of an Enclii addon, and nothing else.
CNPG recreates it on the same PVCs. That apply requires --expect-uid.
Without --apply the command only prints the plan, including the object's UID.
Pass that UID back with --expect-uid so the apply acts only on the object you
reviewed. --apply needs --reason, which is written to the audit log together
with who ran it and when.

Examples:
  # Plan, then roll a Deployment so it reads a changed Secret
  enclii ops pods restart deployment/<name> -n <namespace>
  enclii ops pods restart deployment/<name> -n <namespace> --expect-uid <uid> \
    --apply --reason "load the rotated environment Secret"

  # Recreate a crash-looping StatefulSet pod that OrderedReady will not replace
  enclii ops pods restart pod/loki-0 -n logging
  enclii ops pods restart pod/loki-0 -n logging --expect-uid <uid> \
    --apply --reason "recreate loki-0 on the fixed revision"

  # Recreate a managed-Postgres instance whose volume mount broke
  enclii ops pods restart pod/<cluster>-1 -n project-<id>
  enclii ops pods restart pod/<cluster>-1 -n project-<id> --expect-uid <uid> \
    --apply --reason "remount after the node fault (I/O error in the postgres log)"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kind, name, found := strings.Cut(strings.TrimSpace(args[0]), "/")
			if !found || kind == "" || name == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("target must be <kind>/<name>, e.g. deployment/web or pod/loki-0")}
			}
			if strings.TrimSpace(flags.namespace) == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--namespace is required")}
			}
			extra := map[string]string{"target": strings.TrimSpace(args[0])}
			if strings.TrimSpace(expectUID) != "" {
				extra["expect_uid"] = strings.TrimSpace(expectUID)
			}
			return runOperation(cmd, cfg, opsPath("pods", "restart"), "ops.pods.restart", flags, extra)
		},
	}
	addOperationFlags(cmd, &flags)
	cmd.Flags().StringVar(&expectUID, "expect-uid", "", "UID from the reviewed dry run; the apply is refused if the object was replaced since")
	return cmd
}
