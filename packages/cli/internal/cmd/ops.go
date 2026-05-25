package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

// NewOpsCommand creates the MADFAM operator command surface. This is the
// plan-first replacement layer for routine kubectl/Argo/Longhorn/Kyverno/ARC
// operations; direct kubectl remains break-glass until every command below is
// backed by a concrete server-side adapter.
func NewOpsCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ops",
		Short: "Operate Kubernetes, Argo, storage, policy, and runner workflows",
		Long: `Operate MADFAM infrastructure through Enclii's audited operator layer.

Mutating commands are dry-run by default. Add --apply --reason "..." to execute
once the server-side adapter supports the operation.`,
	}
	cmd.AddCommand(newOpsCapabilitiesCommand(cfg))
	cmd.AddCommand(newOpsAppsCommand(cfg))
	cmd.AddCommand(newOpsPodsCommand(cfg))
	cmd.AddCommand(newOpsJobsCommand(cfg))
	cmd.AddCommand(newOpsStorageCommand(cfg))
	cmd.AddCommand(newOpsSecretsCommand(cfg))
	cmd.AddCommand(newOpsPolicyCommand(cfg))
	cmd.AddCommand(newOpsRunnersCommand(cfg))
	return cmd
}

func newOpsCapabilitiesCommand(cfg *config.Config) *cobra.Command {
	var flags operationFlags
	cmd := &cobra.Command{
		Use:   "capabilities",
		Short: "List server-supported ops capabilities",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCapabilities(cmd, cfg, "/v1/ops/capabilities", flags)
		},
	}
	addReadFlags(cmd, &flags)
	return cmd
}

func newOpsAppsCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{Use: "apps", Short: "Argo application workflows"}
	cmd.AddCommand(newOpsReadCommand(cfg, "apps", "status", "List or inspect Argo application status"))
	cmd.AddCommand(newOpsActionCommand(cfg, "apps", "sync", "Sync an Argo application"))
	cmd.AddCommand(newOpsReadCommand(cfg, "apps", "diff", "Inspect desired-vs-live application drift"))
	cmd.AddCommand(newOpsActionCommand(cfg, "apps", "retire", "Retire an Argo application without routine kubectl access"))
	cmd.AddCommand(newOpsActionCommand(cfg, "apps", "rollback", "Rollback an application to a known revision"))
	return cmd
}

func newOpsPodsCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{Use: "pods", Short: "Pod diagnosis, logs, and restarts"}
	cmd.AddCommand(newOpsReadCommand(cfg, "pods", "diagnose", "Diagnose pod scheduling, probe, image, and event issues"))
	cmd.AddCommand(newOpsPodsLogsCommand(cfg))
	cmd.AddCommand(newOpsActionCommand(cfg, "pods", "restart", "Restart pods or workloads via a safe rollout path"))
	return cmd
}

func newOpsPodsLogsCommand(cfg *config.Config) *cobra.Command {
	var flags operationFlags
	var container string
	var tailLines int64 = 400
	var limitBytes int64 = 262144
	cmd := &cobra.Command{
		Use:   "logs [target]",
		Short: "Fetch pod or workload logs",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			extra := map[string]string{
				"tailLines":  fmt.Sprint(tailLines),
				"limitBytes": fmt.Sprint(limitBytes),
			}
			if len(args) == 1 {
				extra["target"] = args[0]
			}
			if strings.TrimSpace(container) != "" {
				extra["container"] = strings.TrimSpace(container)
			}
			flags.apply = false
			return runOperation(cmd, cfg, opsPath("pods", "logs"), "ops.pods.logs", flags, extra)
		},
	}
	addReadFlags(cmd, &flags)
	cmd.Flags().StringVarP(&flags.namespace, "namespace", "n", "", "Kubernetes namespace scope")
	cmd.Flags().StringVar(&flags.project, "project", "", "Enclii project slug scope")
	cmd.Flags().StringVar(&flags.service, "service", "", "Enclii service name/id scope")
	cmd.Flags().StringVar(&container, "container", "", "Container name when a pod has multiple containers")
	cmd.Flags().Int64Var(&tailLines, "tail", tailLines, "Recent log lines to request; use 0 for all lines within --limit-bytes")
	cmd.Flags().Int64Var(&limitBytes, "limit-bytes", limitBytes, "Maximum log bytes to return")
	return cmd
}

func newOpsJobsCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{Use: "jobs", Short: "CronJob inspection and audited triggers"}
	cmd.AddCommand(newOpsReadCommand(cfg, "jobs", "list", "List Kubernetes CronJobs through Enclii"))
	cmd.AddCommand(newOpsActionCommand(cfg, "jobs", "trigger", "Trigger an existing CronJob once from its live template"))
	return cmd
}

func newOpsStorageCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{Use: "storage", Short: "PVC, PV, and Longhorn workflows"}
	cmd.AddCommand(newOpsReadCommand(cfg, "storage", "volumes", "List storage volumes and health"))
	cmd.AddCommand(newOpsReadCommand(cfg, "storage", "pvc", "Inspect PVC/PV binding and attach state"))
	cmd.AddCommand(newOpsReadCommand(cfg, "storage", "longhorn", "Inspect Longhorn volume health and scheduling"))
	cmd.AddCommand(newOpsActionCommand(cfg, "storage", "repair-plan", "Generate or apply a storage repair plan"))
	return cmd
}

func newOpsSecretsCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{Use: "secrets", Short: "ExternalSecrets, Vault, and secret readiness workflows"}
	cmd.AddCommand(newOpsReadCommand(cfg, "secrets", "external", "Inspect ExternalSecret readiness and target Secret shape"))
	cmd.AddCommand(newOpsReadCommand(cfg, "secrets", "vault", "Inspect Vault auth, seal, and sync readiness"))
	cmd.AddCommand(newOpsActionCommand(cfg, "secrets", "refresh", "Refresh external secret reconciliation"))
	cmd.AddCommand(newOpsActionCommand(cfg, "secrets", "sync", "Alias for ExternalSecret reconciliation refresh"))
	cmd.AddCommand(newOpsActionCommand(cfg, "secrets", "rotate", "Plan a secret rotation through the audited operator layer"))
	return cmd
}

func newOpsPolicyCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{Use: "policy", Short: "Kyverno and admission policy workflows"}
	cmd.AddCommand(newOpsReadCommand(cfg, "policy", "violations", "List policy violations by namespace or workload"))
	cmd.AddCommand(newOpsReadCommand(cfg, "policy", "exceptions", "List active PolicyExceptions"))
	cmd.AddCommand(newOpsActionCommand(cfg, "policy", "waiver-plan", "Generate or apply a time-bound policy waiver plan"))
	return cmd
}

func newOpsRunnersCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{Use: "runners", Short: "ARC and CI runner workflows"}
	cmd.AddCommand(newOpsReadCommand(cfg, "runners", "arc", "Inspect ARC runner scale sets and ephemeral runners"))
	cmd.AddCommand(newOpsActionCommand(cfg, "runners", "drain", "Drain or pause runner capacity safely"))
	return cmd
}

func newOpsReadCommand(cfg *config.Config, domain, action, short string) *cobra.Command {
	var flags operationFlags
	cmd := &cobra.Command{
		Use:   action + " [target]",
		Short: short,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			extra := map[string]string{}
			if len(args) == 1 {
				extra["target"] = args[0]
			}
			flags.apply = false
			return runOperation(cmd, cfg, opsPath(domain, action), fmt.Sprintf("ops.%s.%s", domain, action), flags, extra)
		},
	}
	addReadFlags(cmd, &flags)
	cmd.Flags().StringVarP(&flags.namespace, "namespace", "n", "", "Kubernetes namespace scope")
	cmd.Flags().StringVar(&flags.project, "project", "", "Enclii project slug scope")
	cmd.Flags().StringVar(&flags.service, "service", "", "Enclii service name/id scope")
	return cmd
}

func newOpsActionCommand(cfg *config.Config, domain, action, short string) *cobra.Command {
	var flags operationFlags
	cmd := &cobra.Command{
		Use:   action + " [target]",
		Short: short,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			extra := map[string]string{}
			if len(args) == 1 {
				extra["target"] = args[0]
			}
			return runOperation(cmd, cfg, opsPath(domain, action), fmt.Sprintf("ops.%s.%s", domain, action), flags, extra)
		},
	}
	addOperationFlags(cmd, &flags)
	return cmd
}

func opsPath(domain, action string) string {
	return fmt.Sprintf("/v1/ops/%s/%s", domain, strings.ReplaceAll(action, "_", "-"))
}
