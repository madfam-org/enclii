package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	networkingv1 "k8s.io/api/networking/v1"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/manifest"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/netpolicy"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// The network_policies step of onboarding and onboard ensure, behind the
// narrowing guard. On 2026-10-21 a repair-mode ensure regenerated crea-map's
// policies from enclii.yaml with every step "ok" and cut the runtime off from
// PgBouncer for 5h42m. The step now diffs the live policies against the
// generated ones first, returns that diff in the response, and refuses to
// apply a change that narrows live access unless allow_netpol_narrowing is
// set. See netpolicy.ComputePlan for the rules.

const networkPolicyStep = "network_policies"

// networkPolicyCluster is the part of the Kubernetes client the step uses;
// *k8s.Client satisfies it.
type networkPolicyCluster interface {
	ListNetworkPolicies(ctx context.Context, namespace string) ([]networkingv1.NetworkPolicy, error)
	ListNetworkPolicyWorkloads(ctx context.Context, namespace string) ([]netpolicy.Workload, []string)
	ApplyNetworkPolicies(ctx context.Context, namespace string, yamlBytes []byte) (int, error)
}

// networkPolicyPlanView is the network_policy_plan object in the response.
type networkPolicyPlanView struct {
	*netpolicy.Plan
	// Blocked: the plan narrows live access and nothing was applied.
	Blocked bool `json:"blocked"`
	// NarrowingAllowed: allow_netpol_narrowing was set on the request.
	NarrowingAllowed bool `json:"narrowing_allowed"`
	Applied          bool `json:"applied"`
	DryRun           bool `json:"dry_run,omitempty"`
	// Diff is the plan rendered as lines, for the CLI to print as is.
	Diff []string `json:"diff"`
}

func newNetworkPolicyPlanView(plan *netpolicy.Plan, allow bool) *networkPolicyPlanView {
	return &networkPolicyPlanView{Plan: plan, NarrowingAllowed: allow, Diff: plan.Lines()}
}

// planNetworkPolicies diffs the generated policies against the live ones.
// Failing to read the live policies is an error: without them the guard
// cannot tell a no-op from an outage, so nothing may be applied.
func planNetworkPolicies(ctx context.Context, cluster networkPolicyCluster, namespace string, npYAML []byte, spec netpolicy.NetworkSpec) (*netpolicy.Plan, error) {
	generated, err := netpolicy.ParsePolicies(npYAML)
	if err != nil {
		return nil, err
	}
	live, err := cluster.ListNetworkPolicies(ctx, namespace)
	if err != nil {
		return nil, fmt.Errorf("could not read the live NetworkPolicies to check for narrowing, so nothing was applied: %w", err)
	}
	workloads, notes := cluster.ListNetworkPolicyWorkloads(ctx, namespace)
	plan := netpolicy.ComputePlan(namespace, live, generated, workloads, spec)
	plan.Notes = append(plan.Notes, notes...)
	return plan, nil
}

// applyNetworkPoliciesGuarded plans, then applies unless the plan narrows
// live access and the caller did not opt in.
func applyNetworkPoliciesGuarded(ctx context.Context, cluster networkPolicyCluster, namespace string, npYAML []byte, spec netpolicy.NetworkSpec, allow bool) (*networkPolicyPlanView, error) {
	plan, err := planNetworkPolicies(ctx, cluster, namespace, npYAML, spec)
	if err != nil {
		return nil, err
	}
	view := newNetworkPolicyPlanView(plan, allow)
	if plan.Narrows() && !allow {
		view.Blocked = true
		return view, plan.RefusalError()
	}
	count, err := cluster.ApplyNetworkPolicies(ctx, namespace, npYAML)
	if err != nil {
		return view, fmt.Errorf("applied %d policies before error: %w", count, err)
	}
	view.Applied = true
	return view, nil
}

// runNetworkPolicyStep is the network_policies step shared by OnboardRepo and
// EnsureOnboarding. It returns the plan for the response (nil when the step
// could not plan at all).
func (h *Handler) runNetworkPolicyStep(ctx context.Context, steps *[]stepResult, namespace, projectName string, merged *manifest.EncliiYAMLNetwork, allow bool) *networkPolicyPlanView {
	spec := convertNetworkSpec(merged)
	npYAML, err := netpolicy.GeneratePolicies(namespace, projectName, spec)
	if err != nil {
		h.recordStep(ctx, steps, networkPolicyStep, false, err)
		return nil
	}
	if h.k8sClient == nil || !h.k8sClient.IsValid() {
		h.recordStep(ctx, steps, networkPolicyStep, false, fmt.Errorf("k8s client not available for NetworkPolicy apply"))
		return nil
	}
	view, err := applyNetworkPoliciesGuarded(ctx, h.k8sClient, namespace, npYAML, spec, allow)
	h.recordStep(ctx, steps, networkPolicyStep, false, err)
	if view == nil {
		return nil
	}
	if err == nil && view.Narrows() {
		// Opted in: the step is ok, but it must not read as a plain "ok".
		(*steps)[len(*steps)-1].Detail = fmt.Sprintf("applied with allow_netpol_narrowing despite %d narrowing finding(s); see network_policy_plan", len(view.Refusals))
		h.logger.Warn(ctx, "Applied narrowing NetworkPolicies by explicit opt-in",
			logging.String("namespace", namespace),
			logging.String("findings", strings.Join(findingMessages(view.Refusals), " | ")))
	} else if err == nil {
		h.logger.Info(ctx, "Applied NetworkPolicies via K8s API", logging.String("namespace", namespace))
	}
	return view
}

func findingMessages(findings []netpolicy.Finding) []string {
	out := make([]string, len(findings))
	for i, f := range findings {
		out[i] = f.Message
	}
	return out
}

// PlanOnboardingNetworkPolicies is the dry run of the network_policies step:
// it generates the policies from the repo's enclii.yaml and diffs them
// against the namespace, and changes nothing.
// POST /v1/admin/onboard/netpolicy-plan
func (h *Handler) PlanOnboardingNetworkPolicies(c *gin.Context) {
	ctx := c.Request.Context()
	var req types.OnboardingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	namespace := req.Namespace
	if namespace == "" {
		namespace = req.ProjectName
	}
	docs := manifest.FetchAndParseDocuments(ctx, h.logger, h.config.GitHubToken, req.RepoFullName, "HEAD")
	if manifest.FirstServiceDocument(docs) == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("could not read enclii.yaml from %s; no plan can be computed", req.RepoFullName)})
		return
	}
	merged := manifest.MergedNetwork(docs)
	if merged == nil {
		c.JSON(http.StatusOK, gin.H{"namespace": namespace, "network_declared": false,
			"note": "enclii.yaml declares no network section; the network_policies step would not run"})
		return
	}
	if h.k8sClient == nil || !h.k8sClient.IsValid() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "K8s client not available to read the live NetworkPolicies"})
		return
	}
	spec := convertNetworkSpec(merged)
	npYAML, err := netpolicy.GeneratePolicies(namespace, req.ProjectName, spec)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	plan, err := planNetworkPolicies(ctx, h.k8sClient, namespace, npYAML, spec)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	view := newNetworkPolicyPlanView(plan, req.AllowNetpolNarrowing)
	view.DryRun = true
	view.Blocked = plan.Narrows() && !req.AllowNetpolNarrowing
	c.JSON(http.StatusOK, gin.H{"namespace": namespace, "network_declared": true, "network_policy_plan": view})
}
