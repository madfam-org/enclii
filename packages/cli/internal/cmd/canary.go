package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// NewCanaryCommand is the top-level `enclii canary` group. Subcommands:
//
//	enclii canary status   <rollout_id>
//	enclii canary promote  <rollout_id>
//	enclii canary rollback <rollout_id> [--reason=...]
//
// Canary rollouts are *started* via `enclii deploy --canary=N%` — see
// deploy.go. We keep the start flow on `deploy` (rather than adding a
// `canary start` subcommand) to match the spec.
func NewCanaryCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "canary",
		Short: "Manage canary rollouts",
		Long: `Manage in-flight canary rollouts.

A rollout is started by ` + "`enclii deploy --canary=N%`" + ` and progresses
through states (pending → running → validating → promoting → succeeded) in
the background. Use these subcommands to observe or override that flow.

Examples:
  enclii canary status <rollout_id>
  enclii canary promote <rollout_id>
  enclii canary rollback <rollout_id> --reason "error rate spiked"`,
	}

	cmd.AddCommand(newCanaryStatusCommand(cfg))
	cmd.AddCommand(newCanaryPromoteCommand(cfg))
	cmd.AddCommand(newCanaryRollbackCommand(cfg))
	return cmd
}

func newCanaryStatusCommand(cfg *config.Config) *cobra.Command {
	var follow bool
	var serviceRef string
	cmd := &cobra.Command{
		Use:   "status <rollout_id>",
		Short: "Show canary rollout status (optionally tail until terminal)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return canaryStatus(cfg, args[0], serviceRef, follow)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Tail until rollout reaches a terminal state")
	cmd.Flags().StringVar(&serviceRef, "service", "", "Service name or UUID (required — used to build the endpoint URL)")
	return cmd
}

func newCanaryPromoteCommand(cfg *config.Config) *cobra.Command {
	var serviceRef string
	cmd := &cobra.Command{
		Use:   "promote <rollout_id>",
		Short: "Manually promote a canary (short-circuits the validation window)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return canaryPromote(cfg, args[0], serviceRef)
		},
	}
	cmd.Flags().StringVar(&serviceRef, "service", "", "Service name or UUID")
	return cmd
}

func newCanaryRollbackCommand(cfg *config.Config) *cobra.Command {
	var serviceRef, reason string
	cmd := &cobra.Command{
		Use:   "rollback <rollout_id>",
		Short: "Manually abort a canary rollout",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return canaryRollback(cfg, args[0], serviceRef, reason)
		},
	}
	cmd.Flags().StringVar(&serviceRef, "service", "", "Service name or UUID")
	cmd.Flags().StringVar(&reason, "reason", "", "Reason (captured in audit log)")
	return cmd
}

// -------------------------------------------------------------------------
// Subcommand implementations
// -------------------------------------------------------------------------

func canaryStatus(cfg *config.Config, rolloutID, serviceRef string, follow bool) error {
	ctx := context.Background()
	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)

	serviceID, err := resolveServiceID(ctx, apiClient, cfg, serviceRef)
	if err != nil {
		return err
	}

	if !follow {
		ro, err := apiClient.GetCanary(ctx, serviceID, rolloutID)
		if err != nil {
			return err
		}
		printCanaryStatus(ro)
		return nil
	}

	// Follow mode — poll every 3 seconds until terminal.
	fmt.Printf("Following rollout %s...\n", rolloutID)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	var lastState types.CanaryRolloutState
	for {
		ro, err := apiClient.GetCanary(ctx, serviceID, rolloutID)
		if err != nil {
			return err
		}
		if ro.State != lastState {
			printCanaryStatus(&client.CanaryRolloutResponse{CanaryRollout: ro.CanaryRollout, ActualPercentage: ro.ActualPercentage})
			lastState = ro.State
		}
		if ro.State.IsTerminal() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func canaryPromote(cfg *config.Config, rolloutID, serviceRef string) error {
	ctx := context.Background()
	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)
	serviceID, err := resolveServiceID(ctx, apiClient, cfg, serviceRef)
	if err != nil {
		return err
	}
	if err := apiClient.PromoteCanary(ctx, serviceID, rolloutID); err != nil {
		return &exitcodes.DeployError{Err: err}
	}
	fmt.Println("Promote requested — monitoring status:")
	return canaryStatus(cfg, rolloutID, serviceRef, true)
}

func canaryRollback(cfg *config.Config, rolloutID, serviceRef, reason string) error {
	ctx := context.Background()
	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)
	serviceID, err := resolveServiceID(ctx, apiClient, cfg, serviceRef)
	if err != nil {
		return err
	}
	if err := apiClient.RollbackCanary(ctx, serviceID, rolloutID, reason); err != nil {
		return &exitcodes.DeployError{Err: err}
	}
	fmt.Println("Rollback requested — monitoring status:")
	return canaryStatus(cfg, rolloutID, serviceRef, true)
}

// resolveServiceID accepts a service UUID or name (from --service) or falls
// back to the current cfg.Project default service.
func resolveServiceID(ctx context.Context, apiClient *client.APIClient, cfg *config.Config, serviceRef string) (string, error) {
	if serviceRef == "" {
		return "", fmt.Errorf("--service is required (name or UUID)")
	}
	// If it looks like a UUID, accept directly.
	if looksLikeUUID(serviceRef) {
		return serviceRef, nil
	}

	// Otherwise look it up by name under the configured project.
	projectSlug := cfg.Project
	if projectSlug == "" {
		projectSlug = "default"
	}
	services, err := apiClient.ListServices(ctx, projectSlug)
	if err != nil {
		return "", fmt.Errorf("list services: %w", err)
	}
	for _, s := range services {
		if s.Name == serviceRef {
			return s.ID.String(), nil
		}
	}
	return "", fmt.Errorf("service %q not found in project %q", serviceRef, projectSlug)
}

func looksLikeUUID(s string) bool {
	return len(s) == 36 && strings.Count(s, "-") == 4
}

// -------------------------------------------------------------------------
// Pretty-printing
// -------------------------------------------------------------------------

func printCanaryStatus(resp *client.CanaryRolloutResponse) {
	if resp == nil || resp.CanaryRollout == nil {
		return
	}
	r := resp.CanaryRollout

	fmt.Printf("\nRollout: %s\n", r.ID)
	fmt.Printf("  State:              %s\n", r.State)
	fmt.Printf("  Service:            %s\n", r.ServiceID)
	fmt.Printf("  Digest:             %s\n", truncate(r.CanaryDigest, 24))
	fmt.Printf("  Requested %%:        %d%%\n", r.CanaryPercentage)
	fmt.Printf("  Actual %%:           %.1f%% (%d canary / %d total)\n", resp.ActualPercentage, r.CanaryReplicas, r.TotalReplicas)
	fmt.Printf("  Validation window:  %ds\n", r.ValidationWindowSeconds)
	if r.SmokeEndpoint != "" {
		fmt.Printf("  Smoke endpoint:     %s\n", r.SmokeEndpoint)
	}
	if r.ValidatingStartedAt != nil && r.State == types.CanaryStateValidating {
		elapsed := time.Since(*r.ValidatingStartedAt)
		remaining := time.Duration(r.ValidationWindowSeconds)*time.Second - elapsed
		fmt.Printf("  Validation elapsed: %s (remaining ~%s)\n", truncDur(elapsed), truncDur(remaining))
	}
	if r.LastError != "" {
		fmt.Printf("  Last error:         %s\n", r.LastError)
	}
	if r.RollbackReason != "" {
		fmt.Printf("  Rollback reason:    %s\n", r.RollbackReason)
	}
	if r.State.IsTerminal() {
		fmt.Fprintln(os.Stdout)
		switch r.State {
		case types.CanaryStateSucceeded:
			fmt.Println("Rollout succeeded.")
		case types.CanaryStateAutoRolledBack:
			fmt.Println("Rollout was auto-rolled-back (canary failed validation).")
		case types.CanaryStateManualRolledBack:
			fmt.Println("Rollout was manually rolled back.")
		case types.CanaryStateFailed:
			fmt.Println("Rollout failed.")
		}
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func truncDur(d time.Duration) string {
	if d < 0 {
		return "0s"
	}
	// Keep it readable: integer seconds/minutes.
	if d < time.Minute {
		return strconv.Itoa(int(d.Seconds())) + "s"
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}
