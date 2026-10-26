package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ops.storage.prune-detached deletes Longhorn volumes that nothing can use
// again. "Detached" alone is NOT that: a hibernated managed database (CNPG
// `cnpg.io/hibernation=on`), an idle restore-drill claim, a backup spool or a
// scaled-to-zero StatefulSet all leave a detached volume behind a claim that is
// still Bound, and deleting it destroys the only copy of retained data.
//
// So a volume is eligible only when it is detached AND unclaimed: its PV is
// Released (the claim was deleted under a Retain class) or it has no PV at
// all. A Bound PV, a PV in any other phase, or a claim that still references
// the volume is refused with the reason. The dry-run reports both lists and a
// plan_hash over the eligible set; apply requires args.expected_plan equal to
// the plan_hash of the CURRENT eligible set, a reason and an authenticated
// platform caller, writes an audit row before the first delete and refuses to
// delete without it, and re-reads each volume immediately before deleting it.
const opsStoragePruneDetachedOperation = "ops.storage.prune-detached"

var longhornVolumeGVR = schema.GroupVersionResource{
	Group:    "longhorn.io",
	Version:  "v1beta2",
	Resource: "volumes",
}

type longhornVolumeRef struct {
	Name            string `json:"name"`
	State           string `json:"state"`
	PVName          string `json:"pv_name,omitempty"`
	PVStatus        string `json:"pv_status,omitempty"`
	Namespace       string `json:"namespace,omitempty"`
	PVC             string `json:"pvc,omitempty"`
	LastPVCRefAt    string `json:"last_pvc_ref_at,omitempty"`
	ActualSizeBytes int64  `json:"actual_size_bytes"`
	Refusal         string `json:"refusal,omitempty"`
}

func longhornVolumeState(obj *unstructured.Unstructured) string {
	if obj == nil {
		return ""
	}
	state, _, _ := unstructured.NestedString(obj.Object, "status", "state")
	return strings.ToLower(strings.TrimSpace(state))
}

// longhornInt64 reads an integer that JSON decoding may have produced as
// int64, float64 or a string (Longhorn writes status.actualSize as a number).
func longhornInt64(obj map[string]any, fields ...string) int64 {
	raw, found, _ := unstructured.NestedFieldNoCopy(obj, fields...)
	if !found {
		return 0
	}
	switch v := raw.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case string:
		var n int64
		_, _ = fmt.Sscan(v, &n)
		return n
	}
	return 0
}

func longhornVolumeRefFrom(obj *unstructured.Unstructured) longhornVolumeRef {
	nested := func(fields ...string) string {
		value, _, _ := unstructured.NestedString(obj.Object, append([]string{"status", "kubernetesStatus"}, fields...)...)
		return strings.TrimSpace(value)
	}
	return longhornVolumeRef{
		Name:            obj.GetName(),
		State:           longhornVolumeState(obj),
		PVName:          nested("pvName"),
		PVStatus:        nested("pvStatus"),
		Namespace:       nested("namespace"),
		PVC:             nested("pvcName"),
		LastPVCRefAt:    nested("lastPVCRefAt"),
		ActualSizeBytes: longhornInt64(obj.Object, "status", "actualSize"),
	}
}

// pruneRefusal returns why a volume must not be pruned, or "" when it is an
// orphan. It is the single eligibility rule for both the plan and the apply.
func pruneRefusal(vol longhornVolumeRef) string {
	claim := vol.Namespace + "/" + vol.PVC
	if vol.State != "detached" {
		return fmt.Sprintf("state is %q, not detached", vol.State)
	}
	switch {
	case vol.PVStatus == "Bound":
		return fmt.Sprintf("PV is Bound to claim %s: a detached volume behind a live claim is idle or hibernated, not orphaned", claim)
	case vol.PVName == "" && vol.PVStatus == "":
		// No PV at all: nothing in Kubernetes can mount it again.
	case vol.PVStatus != "Released":
		return fmt.Sprintf("PV %s is %q; only a Released PV (claim deleted) or no PV is an orphan", vol.PVName, vol.PVStatus)
	}
	if vol.PVC != "" && vol.LastPVCRefAt == "" {
		return fmt.Sprintf("claim %s still references the volume", claim)
	}
	return ""
}

func (h *Handler) listLonghornVolumes(ctx context.Context) ([]longhornVolumeRef, error) {
	if h.k8sClient == nil || h.k8sClient.DynamicClient == nil {
		return nil, fmt.Errorf("kubernetes dynamic client is not configured")
	}
	resource := h.k8sClient.DynamicClient.Resource(longhornVolumeGVR).Namespace(longhornSystemNamespace)
	list, err := resource.List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]longhornVolumeRef, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, longhornVolumeRefFrom(&list.Items[i]))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// prunePlan splits the detached volumes in scope into eligible orphans and
// refused ones. Attached volumes are not listed at all; a target that is not
// detached is reported as refused so the caller sees why nothing is planned.
type prunePlan struct {
	Eligible []longhornVolumeRef `json:"eligible"`
	Refused  []longhornVolumeRef `json:"refused"`
	// EligibleBytes is the per-replica actualSize sum of the eligible set; the
	// node disk freed is roughly this times each volume's replica count.
	EligibleBytes int64  `json:"eligible_actual_size_bytes"`
	PlanHash      string `json:"plan_hash"`
}

func buildPrunePlan(volumes []longhornVolumeRef, target string) prunePlan {
	plan := prunePlan{Eligible: []longhornVolumeRef{}, Refused: []longhornVolumeRef{}}
	for _, vol := range volumes {
		if target != "" && vol.Name != target {
			continue
		}
		if vol.State != "detached" && target == "" {
			continue
		}
		if why := pruneRefusal(vol); why != "" {
			vol.Refusal = why
			plan.Refused = append(plan.Refused, vol)
			continue
		}
		plan.Eligible = append(plan.Eligible, vol)
		plan.EligibleBytes += vol.ActualSizeBytes
	}
	plan.PlanHash = prunePlanHash(plan.Eligible)
	return plan
}

// prunePlanHash covers what makes a volume eligible, not its size, so the hash
// is stable across the dry-run and the apply unless eligibility changed.
func prunePlanHash(eligible []longhornVolumeRef) string {
	lines := make([]string, 0, len(eligible))
	for _, vol := range eligible {
		lines = append(lines, strings.Join([]string{vol.Name, vol.State, vol.PVName, vol.PVStatus, vol.Namespace, vol.PVC}, "|"))
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

func pruneNext(plan prunePlan, target string) []string {
	if len(plan.Eligible) == 0 {
		return []string{"nothing to prune: refused volumes are claimed (Bound) or not detached; release a claim through its owner's lifecycle, not here"}
	}
	scope := ""
	if target != "" {
		scope = target + " "
	}
	return []string{
		"review every eligible row: a Released PV is retained data whose claim was deleted (Retain class); it is gone after apply",
		fmt.Sprintf("enclii ops storage prune-detached %s--expect-plan %s --apply --reason \"<why these volumes are orphaned>\"", scope, plan.PlanHash),
	}
}

func (h *Handler) handleOpsStoragePruneDetached(ctx context.Context, req operatorOperationRequest, actor opsActor) (operatorOperationResponse, int) {
	resp := operatorOperationResponse{
		OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
		Operation:   opsStoragePruneDetachedOperation,
		DryRun:      req.DryRun,
		Status:      "failed",
	}
	expected := operationArg(req, "expected_plan", "expected-plan", "expect_plan", "expect-plan")
	if !req.DryRun && (strings.TrimSpace(req.Reason) == "" || expected == "") {
		resp.Summary = "apply requires a reason and args.expected_plan (the plan_hash of a reviewed dry-run); nothing was deleted"
		return resp, http.StatusBadRequest
	}
	if h.k8sClient == nil || h.k8sClient.DynamicClient == nil {
		resp.Status = "adapter_unconfigured"
		resp.Summary = "kubernetes dynamic client is not configured on switchyard-api"
		return resp, http.StatusServiceUnavailable
	}
	if !req.DryRun && (h.repos == nil || h.repos.AuditLogs == nil) {
		resp.Summary = "the audit store is not configured; nothing was deleted"
		return resp, http.StatusServiceUnavailable
	}
	volumes, err := h.listLonghornVolumes(ctx)
	if err != nil {
		resp.Summary = "failed to list Longhorn volumes"
		resp.Warnings = []string{err.Error()}
		return resp, http.StatusBadGateway
	}
	target := operationTarget(req)
	plan := buildPrunePlan(volumes, target)
	data := map[string]any{
		"namespace": longhornSystemNamespace, "total": len(volumes), "plan_hash": plan.PlanHash,
		"eligible": plan.Eligible, "refused": plan.Refused, "eligible_actual_size_bytes": plan.EligibleBytes,
	}
	resp.Data = data
	resp.Steps = []operatorOperationStep{
		{Name: "authorize", Status: "planned", Detail: "platform rank, reason and expected_plan required on apply"},
		{Name: "load-state", Status: "completed", Detail: "listed Longhorn Volume CRs and their claim status"},
		{Name: "diff", Status: "completed", Detail: fmt.Sprintf("%d eligible (detached, PV Released or absent), %d refused", len(plan.Eligible), len(plan.Refused))},
		{Name: "audit", Status: "planned", Detail: "write an audit row before the first delete"},
	}
	resp.Next = pruneNext(plan, target)

	if req.DryRun {
		resp.Status = "ready_to_apply"
		resp.Summary = fmt.Sprintf("%d detached volume(s) are orphaned and eligible; %d detached volume(s) are refused because a claim still holds them", len(plan.Eligible), len(plan.Refused))
		if len(plan.Eligible) == 0 {
			resp.Status = "succeeded"
			resp.Summary = fmt.Sprintf("no orphaned Longhorn volumes to prune; %d refused", len(plan.Refused))
		}
		return resp, http.StatusOK
	}

	if expected != plan.PlanHash {
		resp.Summary = "the plan changed since it was reviewed (expected_plan does not match the current plan_hash); nothing was deleted: run the dry-run again"
		return resp, http.StatusConflict
	}
	if len(plan.Eligible) == 0 {
		resp.Status = "succeeded"
		resp.Summary = "no orphaned Longhorn volumes to prune"
		return resp, http.StatusOK
	}
	names := make([]string, 0, len(plan.Eligible))
	for _, vol := range plan.Eligible {
		names = append(names, vol.Name)
	}
	details := map[string]any{
		"operation": opsStoragePruneDetachedOperation, "operation_id": resp.OperationID, "plan_hash": plan.PlanHash,
		"volumes": names, "eligible_actual_size_bytes": plan.EligibleBytes, "target": target,
		"reason": strings.TrimSpace(req.Reason), "idempotency_key": strings.TrimSpace(req.IdempotencyKey),
	}
	auditID, err := h.writeOpsAuditResource(ctx, actor, opsStoragePruneDetachedOperation, "requested", "longhorn_volume", plan.PlanHash, target, details)
	if err != nil {
		resp.Summary = "the audit record could not be written, so nothing was deleted"
		resp.Warnings = []string{err.Error()}
		return resp, http.StatusServiceUnavailable
	}
	resp.AuditID = auditID

	resource := h.k8sClient.DynamicClient.Resource(longhornVolumeGVR).Namespace(longhornSystemNamespace)
	deleted := make([]string, 0, len(plan.Eligible))
	skipped := make([]string, 0)
	for _, vol := range plan.Eligible {
		current, err := resource.Get(ctx, vol.Name, metav1.GetOptions{})
		if err != nil {
			if k8serrors.IsNotFound(err) {
				skipped = append(skipped, vol.Name)
				continue
			}
			resp.Warnings = append(resp.Warnings, fmt.Sprintf("%s: get failed, not deleted: %v", vol.Name, err))
			continue
		}
		if why := pruneRefusal(longhornVolumeRefFrom(current)); why != "" {
			skipped = append(skipped, vol.Name)
			resp.Warnings = append(resp.Warnings, fmt.Sprintf("%s: no longer eligible (%s); skipped", vol.Name, why))
			continue
		}
		if err := resource.Delete(ctx, vol.Name, metav1.DeleteOptions{}); err != nil {
			resp.Warnings = append(resp.Warnings, fmt.Sprintf("%s: delete failed: %v", vol.Name, err))
			continue
		}
		deleted = append(deleted, vol.Name)
	}

	outcome := "success"
	code := http.StatusAccepted
	resp.Status = "submitted"
	resp.Summary = fmt.Sprintf("deleted %d orphaned Longhorn volume(s) through Enclii; Longhorn frees the replica data asynchronously", len(deleted))
	if len(deleted) == 0 {
		outcome, code, resp.Status = "failure", http.StatusConflict, "failed"
		resp.Summary = "no Longhorn volumes were deleted"
	}
	result := map[string]any{"request_audit_id": auditID, "deleted": deleted, "skipped": skipped, "warnings": resp.Warnings}
	for k, v := range details {
		result[k] = v
	}
	if _, err := h.writeOpsAuditResource(ctx, actor, opsStoragePruneDetachedOperation, outcome, "longhorn_volume", plan.PlanHash, target, result); err != nil {
		resp.Warnings = append(resp.Warnings, "the result audit row was not written (the request row "+auditID+" exists): "+err.Error())
	}
	data["deleted"], data["skipped"] = deleted, skipped
	resp.Steps[0].Status, resp.Steps[3].Status = "completed", "completed"
	resp.Next = []string{"verify with `enclii ops storage longhorn` and node disk usage once Longhorn finishes removing the replicas"}
	return resp, code
}
