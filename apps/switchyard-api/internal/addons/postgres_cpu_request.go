package addons

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

// DefaultPostgresCPURequest is the CPU a managed-Postgres instance reserves on
// its node. The plan's CPU (config.CPU, seeded from managed_db_plans) stays
// the instance's CPU LIMIT, so a busy database still bursts to what its plan
// sells, and usage metering (config.CPU) is unchanged.
//
// Owner decision, 2026-11-04. Until then the plan CPU was both request and
// limit: 500m reserved per standard-1 addon while six idle addons used about
// 0.03 cores between them. The scheduler packs by request, so that reserve was
// closed to every other pod whether the database used it or not.
//
// Trade-off: CPU request below its limit makes the instance pod Burstable
// instead of Guaranteed. Memory keeps request == limit, so the kubelet, which
// evicts pods over their memory request first, never ranks an addon there;
// but the kernel OOM killer, if the kubelet does not act first, no longer
// shields it the way it shields a Guaranteed pod.
const DefaultPostgresCPURequest = "100m"

// postgresCPURequest returns the CPU request for a plan CPU (the limit): the
// default request, or the plan CPU itself when that is smaller, because a
// request may not exceed its limit. An unparsable value is returned as it is;
// the API server rejects it as it always has.
func postgresCPURequest(planCPU string) string {
	limit, err := resource.ParseQuantity(planCPU)
	if err != nil {
		return planCPU
	}
	if limit.Cmp(resource.MustParse(DefaultPostgresCPURequest)) <= 0 {
		return planCPU
	}
	return DefaultPostgresCPURequest
}

// ---------------------------------------------------------------------------
// Existing clusters: ops.addons.cpu-request
//
// Clusters provisioned before the default changed carry request == limit. The
// provisioner only creates, and no sweep rewrites resources, because a CNPG
// resources change restarts the instance: on a single-instance cluster the
// database is unavailable until the new pod is Ready. So the change is an
// operator action, one cluster per apply, at a time the operator chooses:
//
//   - the plan reads the live Cluster and proposes requests.cpu =
//     postgresCPURequest(limit), keeping limits.cpu (or setting it to the old
//     request when there was none, which keeps the ceiling the old request
//     implied). Memory and everything else are left alone;
//   - the apply carries the reviewed plan hash and the Cluster's
//     resourceVersion, so a Cluster changed since the plan is refused.
// ---------------------------------------------------------------------------

var (
	// ErrCPURequestUnsupported: not a provisioned managed-Postgres addon.
	ErrCPURequestUnsupported = errors.New("the CPU request change applies only to a provisioned managed-Postgres addon")
	// ErrCPURequestPlanChanged: the live Cluster no longer hashes to the
	// reviewed plan; take a new dry-run.
	ErrCPURequestPlanChanged = errors.New("the cluster changed since the reviewed plan; obtain a new dry-run")
	// ErrCPURequestBlocked: the plan refuses this cluster; nothing changed.
	ErrCPURequestBlocked = errors.New("the CPU request change is blocked for this cluster; nothing was changed")
)

// CPURequestPlan is the deterministic plan for one Cluster. PlanHash covers
// every field, so an apply carrying it acts on exactly the reviewed state.
type CPURequestPlan struct {
	AddonID               string `json:"addon_id"`
	Namespace             string `json:"namespace"`
	Cluster               string `json:"cluster"`
	ClusterUID            string `json:"cluster_uid"`
	ResourceVersion       string `json:"resource_version"`
	Phase                 string `json:"phase"`
	Instances             int64  `json:"instances"`
	PrimaryUpdateStrategy string `json:"primary_update_strategy"`
	Hibernated            bool   `json:"hibernated"`
	CurrentRequest        string `json:"current_cpu_request"`
	CurrentLimit          string `json:"current_cpu_limit"`
	DesiredRequest        string `json:"desired_cpu_request"`
	DesiredLimit          string `json:"desired_cpu_limit"`
	// Restart says what CNPG will do when the apply lands.
	Restart string `json:"restart"`
	// Blocked explains why the apply is refused (empty when it is not).
	Blocked  string `json:"blocked,omitempty"`
	PlanHash string `json:"plan_hash"`
}

// CPURequestResult reports what the apply changed.
type CPURequestResult struct {
	Plan    CPURequestPlan `json:"plan"`
	Patched bool           `json:"patched"`
}

// Done reports whether the live Cluster already carries the desired CPU.
func (p CPURequestPlan) Done() bool {
	return p.Blocked == "" && sameQuantity(p.CurrentRequest, p.DesiredRequest) && sameQuantity(p.CurrentLimit, p.DesiredLimit)
}

// PlanPostgresCPURequest reads the Cluster. It writes nothing.
func PlanPostgresCPURequest(ctx context.Context, dyn dynamic.Interface, addon *types.DatabaseAddon) (*CPURequestPlan, error) {
	if addon == nil || addon.Type != types.DatabaseAddonTypePostgres || addon.K8sNamespace == "" || addon.K8sResourceName == "" {
		return nil, ErrCPURequestUnsupported
	}
	if dyn == nil {
		return nil, fmt.Errorf("kubernetes client is unavailable")
	}
	ns, name := addon.K8sNamespace, addon.K8sResourceName
	cluster, err := dyn.Resource(cnpgGVR).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("read CNPG cluster %s/%s: %w", ns, name, err)
	}
	plan := &CPURequestPlan{
		AddonID: addon.ID.String(), Namespace: ns, Cluster: name,
		ClusterUID: string(cluster.GetUID()), ResourceVersion: cluster.GetResourceVersion(),
		Hibernated: cluster.GetAnnotations()[CNPGHibernationAnnotation] == "on",
	}
	plan.Phase, _, _ = unstructured.NestedString(cluster.Object, "status", "phase")
	plan.Instances, _, _ = unstructured.NestedInt64(cluster.Object, "spec", "instances")
	plan.PrimaryUpdateStrategy, _, _ = unstructured.NestedString(cluster.Object, "spec", "primaryUpdateStrategy")
	plan.CurrentRequest, _, _ = unstructured.NestedString(cluster.Object, "spec", "resources", "requests", "cpu")
	plan.CurrentLimit, _, _ = unstructured.NestedString(cluster.Object, "spec", "resources", "limits", "cpu")

	ceiling := plan.CurrentLimit
	if ceiling == "" {
		ceiling = plan.CurrentRequest
	}
	plan.DesiredRequest, plan.DesiredLimit = plan.CurrentRequest, plan.CurrentLimit
	switch {
	case !managedForAddon(cluster, addon):
		plan.Blocked = "the Cluster does not carry Enclii's labels for this addon; it is not ours to change"
	case plan.CurrentRequest == "":
		plan.Blocked = "the Cluster sets no CPU request (the namespace default applies); there is nothing to lower"
	case !validQuantity(ceiling):
		plan.Blocked = fmt.Sprintf("the Cluster's CPU %q is not a valid quantity", ceiling)
	default:
		plan.DesiredRequest, plan.DesiredLimit = postgresCPURequest(ceiling), ceiling
		// Only a change needs a healthy cluster: a cluster already at the
		// desired CPU is done even while CNPG is still rolling it.
		if !plan.Done() && !plan.Hibernated && plan.Phase != cnpgHealthyPhase {
			plan.Blocked = fmt.Sprintf("CNPG reports «%s», not «%s»: a resources change would start a rolling update on a cluster the operator is not managing cleanly. Plan again once it is healthy", plan.Phase, cnpgHealthyPhase)
		}
	}
	plan.Restart = cpuRequestRestart(*plan)
	plan.PlanHash = cpuRequestPlanHash(*plan)
	return plan, nil
}

// cpuRequestRestart describes what CNPG does once the new resources land.
func cpuRequestRestart(p CPURequestPlan) string {
	switch {
	case p.Blocked != "":
		return "none: the apply is refused"
	case p.Done():
		return "none: the Cluster already carries the desired CPU"
	case p.Hibernated:
		return "none now: the cluster is hibernated; its instances start with the new request when it is woken"
	case p.PrimaryUpdateStrategy == "supervised":
		return "CNPG updates any replicas, then waits: the primary keeps the old request until an operator switches over or restarts it"
	case p.Instances <= 1:
		return "CNPG restarts the single instance on the same volume: the database is unavailable until the new pod is Ready"
	default:
		return "CNPG restarts the replicas one by one, then switches over to an updated replica: clients see a brief disconnect at the switchover"
	}
}

func cpuRequestPlanHash(p CPURequestPlan) string {
	p.PlanHash = ""
	raw, _ := json.Marshal(p)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// ApplyPostgresCPURequest re-plans, refuses a blocked cluster or one whose
// live plan no longer hashes to expectedPlan, then writes spec.resources.cpu
// with an Update that carries the reviewed resourceVersion. A plan that is
// already Done returns without writing, so an exact retry is safe.
func ApplyPostgresCPURequest(ctx context.Context, dyn dynamic.Interface, addon *types.DatabaseAddon, expectedPlan string) (*CPURequestResult, error) {
	plan, err := PlanPostgresCPURequest(ctx, dyn, addon)
	if err != nil {
		return nil, err
	}
	result := &CPURequestResult{Plan: *plan}
	if plan.Blocked != "" {
		return nil, fmt.Errorf("%w: %s", ErrCPURequestBlocked, plan.Blocked)
	}
	if plan.Done() {
		return result, nil
	}
	if expectedPlan == "" || expectedPlan != plan.PlanHash {
		return nil, ErrCPURequestPlanChanged
	}
	cluster, err := dyn.Resource(cnpgGVR).Namespace(plan.Namespace).Get(ctx, plan.Cluster, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("re-read CNPG cluster: %w", err)
	}
	if string(cluster.GetUID()) != plan.ClusterUID || cluster.GetResourceVersion() != plan.ResourceVersion {
		return nil, ErrCPURequestPlanChanged
	}
	if err := unstructured.SetNestedField(cluster.Object, plan.DesiredRequest, "spec", "resources", "requests", "cpu"); err != nil {
		return nil, fmt.Errorf("set CPU request: %w", err)
	}
	if err := unstructured.SetNestedField(cluster.Object, plan.DesiredLimit, "spec", "resources", "limits", "cpu"); err != nil {
		return nil, fmt.Errorf("set CPU limit: %w", err)
	}
	if _, err := dyn.Resource(cnpgGVR).Namespace(plan.Namespace).Update(ctx, cluster, metav1.UpdateOptions{FieldManager: "enclii-addon-cpu-request"}); err != nil {
		return nil, fmt.Errorf("update CNPG cluster (nothing changed): %w", err)
	}
	result.Patched = true
	return result, nil
}

func validQuantity(value string) bool {
	_, err := resource.ParseQuantity(value)
	return err == nil
}

func sameQuantity(a, b string) bool {
	if a == b {
		return true
	}
	qa, errA := resource.ParseQuantity(a)
	qb, errB := resource.ParseQuantity(b)
	return errA == nil && errB == nil && qa.Cmp(qb) == 0
}
