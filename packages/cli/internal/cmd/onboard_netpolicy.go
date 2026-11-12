package cmd

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

const allowNetpolNarrowingUsage = "Apply generated NetworkPolicies even when they narrow live access (review the printed diff first)"

// printNetworkPolicyPlan prints the network_policy_plan object of an onboard,
// ensure or dry-run response: the per-policy diff, then what the narrowing
// guard refused, warned about, or could not check.
func printNetworkPolicyPlan(w io.Writer, result map[string]interface{}) {
	plan, ok := result["network_policy_plan"].(map[string]interface{})
	if !ok {
		return
	}
	fmt.Fprintln(w)
	header := "NetworkPolicy diff"
	if ns, ok := plan["namespace"].(string); ok && ns != "" {
		header += " (namespace " + ns + ")"
	}
	switch {
	case plan["dry_run"] == true && plan["blocked"] == true:
		header += ": DRY RUN, the step WOULD BE REFUSED"
	case plan["dry_run"] == true:
		header += ": DRY RUN, nothing applied"
	case plan["blocked"] == true:
		header += ": REFUSED, nothing applied"
	case plan["applied"] == true && plan["narrowing_allowed"] == true && hasEntries(plan["refusals"]):
		header += ": APPLIED with --allow-netpol-narrowing"
	case plan["applied"] == true:
		header += ": applied"
	}
	fmt.Fprintln(w, header+":")
	if lines, ok := plan["diff"].([]interface{}); ok {
		for _, line := range lines {
			fmt.Fprintf(w, "  %v\n", line)
		}
	}
	if plan["blocked"] == true {
		fmt.Fprintln(w, "  Declare the missing access in enclii.yaml, or re-run with --allow-netpol-narrowing after reviewing the lines above.")
	}
}

func hasEntries(v interface{}) bool {
	list, ok := v.([]interface{})
	return ok && len(list) > 0
}

// runOnboardEnsureDryRun prints the network_policies diff and changes nothing.
func runOnboardEnsureDryRun(ctx context.Context, apiClient *client.APIClient, req *types.OnboardingRequest) error {
	fmt.Printf("Dry run: NetworkPolicy plan for %s (project %q, namespace %q). Only the network_policies step is previewed; no step runs.\n", req.RepoFullName, req.ProjectName, req.Namespace)
	var result map[string]interface{}
	if err := apiClient.PlanOnboardingNetworkPolicies(ctx, req, &result); err != nil {
		return fmt.Errorf("network policy dry run failed: %w", err)
	}
	if result["network_declared"] == false {
		fmt.Printf("\n%v\n", result["note"])
		return nil
	}
	printNetworkPolicyPlan(os.Stdout, result)
	return nil
}
