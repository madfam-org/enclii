package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ops.addons.cpu-request lowers one existing managed-Postgres addon's CPU
// request to the provisioning default (addons.DefaultPostgresCPURequest),
// keeping its plan CPU as the limit (addons/postgres_cpu_request.go). New
// addons get that shape at creation; this is how a Cluster provisioned before
// 2026-11-04 (request == limit) gets it. Owner-approved 2026-11-04.
//
// A CNPG resources change restarts the instance, and a single-instance addon
// is unavailable until the new pod is Ready. So this is an operator action,
// one Cluster per apply, at a time the operator chooses, never a sweep.
//
// Closed request: scope.project (slug) and args.target (addon UUID); apply
// adds args.expected_plan (the dry-run's plan_hash), a reason and an
// idempotency key. Refused unless the addon belongs to that project, is a
// managed Postgres and is `ready`. Platform rank (operator_routes.go). Apply
// writes an audit row before the mutation and refuses to mutate without it; a
// second row records the result. It writes spec.resources.{requests,limits}.cpu
// on the Cluster and nothing else: not memory, not the addon row (whose
// config.cpu is the plan CPU, now the limit, so metering is unchanged).
const opsAddonsCPURequestOperation = "ops.addons.cpu-request"

// cpuRequestRefusal is every refusal that needs only the addon row.
func cpuRequestRefusal(addon *types.DatabaseAddonWithBindings, projectID uuid.UUID) (int, string) {
	if addon == nil || addon.ProjectID != projectID {
		return http.StatusNotFound, "addon not found in this project"
	}
	if addon.Type != types.DatabaseAddonTypePostgres {
		return http.StatusUnprocessableEntity, "the CPU request change applies only to managed Postgres"
	}
	if addon.Status != types.DatabaseAddonStatusReady {
		return http.StatusConflict, fmt.Sprintf("addon is %s, not ready", addon.Status)
	}
	return 0, ""
}

func (h *Handler) handleOpsAddonsCPURequest(ctx context.Context, req operatorOperationRequest, actor opsActor) (operatorOperationResponse, int) {
	resp := operatorOperationResponse{Operation: opsAddonsCPURequestOperation, DryRun: req.DryRun, Status: "failed"}
	slug := strings.TrimSpace(req.Scope["project"])
	addonID, err := uuid.Parse(operationTarget(req))
	if err != nil || slug == "" {
		resp.Summary = "scope.project (slug) and args.target (addon UUID) are required"
		return resp, http.StatusBadRequest
	}
	expected := operationArg(req, "expected_plan", "expected-plan")
	if !req.DryRun && (strings.TrimSpace(req.Reason) == "" || strings.TrimSpace(req.IdempotencyKey) == "" || expected == "") {
		resp.Summary = "apply requires reason, idempotency_key and args.expected_plan from a reviewed dry-run"
		return resp, http.StatusBadRequest
	}
	if h.repos == nil || h.repos.Projects == nil || h.addonService == nil || h.k8sClient == nil || h.k8sClient.DynamicClient == nil {
		resp.Summary = "addon CPU request adapter is unavailable"
		return resp, http.StatusServiceUnavailable
	}
	if !req.DryRun && h.repos.AuditLogs == nil {
		resp.Summary = "the audit store is not configured; nothing was changed"
		return resp, http.StatusServiceUnavailable
	}
	project, err := h.repos.Projects.GetBySlug(slug)
	if err != nil || project == nil {
		resp.Summary = "project not found"
		return resp, http.StatusNotFound
	}
	addon, err := h.addonService.GetAddonWithBindings(ctx, addonID)
	if err != nil {
		resp.Summary = "addon not found in this project"
		return resp, http.StatusNotFound
	}
	if status, why := cpuRequestRefusal(addon, project.ID); status != 0 {
		resp.Summary = why
		return resp, status
	}
	plan, err := addons.PlanPostgresCPURequest(ctx, h.k8sClient.DynamicClient, &addon.DatabaseAddon)
	if err != nil {
		resp.Summary = "plan not available: " + err.Error()
		return resp, http.StatusBadGateway
	}
	if req.DryRun || plan.Done() || plan.Blocked != "" {
		resp.Data = plan
		switch {
		case plan.Blocked != "":
			resp.Status, resp.Summary = "blocked", plan.Blocked
			return resp, http.StatusConflict
		case plan.Done():
			resp.Status, resp.Summary = "succeeded", "already done: the Cluster requests "+plan.CurrentRequest+" CPU under a "+plan.CurrentLimit+" limit"
		default:
			resp.Status, resp.Summary = "ready_to_apply", "review the CPU change and the restart it causes ("+plan.Restart+"), then apply with args.expected_plan=plan_hash"
		}
		return resp, http.StatusOK
	}
	details := map[string]any{
		"operation": opsAddonsCPURequestOperation, "addon_id": addonID.String(), "project": slug,
		"namespace": plan.Namespace, "cluster": plan.Cluster, "plan_hash": plan.PlanHash,
		"cpu_request_from": plan.CurrentRequest, "cpu_request_to": plan.DesiredRequest, "cpu_limit": plan.DesiredLimit,
		"reason": strings.TrimSpace(req.Reason), "idempotency_key": strings.TrimSpace(req.IdempotencyKey),
	}
	auditID, err := h.writeOpsAudit(ctx, actor, opsAddonsCPURequestOperation, "requested", addonID.String(), addon.Name, details)
	if err != nil {
		resp.Summary = "the audit record could not be written, so nothing was changed"
		return resp, http.StatusServiceUnavailable
	}
	resp.AuditID = auditID
	result, applyErr := addons.ApplyPostgresCPURequest(ctx, h.k8sClient.DynamicClient, &addon.DatabaseAddon, expected)
	outcome := map[string]any{"request_audit_id": auditID}
	for k, v := range details {
		outcome[k] = v
	}
	if result != nil {
		outcome["patched"] = result.Patched
	}
	if applyErr != nil {
		outcome["error"] = applyErr.Error()
		_, _ = h.writeOpsAudit(ctx, actor, opsAddonsCPURequestOperation, "failure", addonID.String(), addon.Name, outcome)
		resp.Data = result
		resp.Summary = applyErr.Error()
		switch {
		case errors.Is(applyErr, addons.ErrCPURequestPlanChanged):
			return resp, http.StatusConflict
		case errors.Is(applyErr, addons.ErrCPURequestBlocked):
			resp.Status = "blocked"
			return resp, http.StatusConflict
		}
		return resp, http.StatusBadGateway
	}
	if _, err := h.writeOpsAudit(ctx, actor, opsAddonsCPURequestOperation, "success", addonID.String(), addon.Name, outcome); err != nil {
		resp.Warnings = append(resp.Warnings, "the result audit row was not written (the request row "+auditID+" exists): "+err.Error())
	}
	resp.Status, resp.Data = "succeeded", result
	resp.Summary = "CPU request set to " + plan.DesiredRequest + " under a " + plan.DesiredLimit + " limit; " + plan.Restart + ". Run the dry-run again to follow CNPG's phase"
	return resp, http.StatusOK
}
