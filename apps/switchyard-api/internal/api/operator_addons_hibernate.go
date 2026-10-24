package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ops.addons.hibernate stops a retired managed-Postgres addon's compute and
// keeps its data (addons/hibernation.go). It replaces the break-glass
// `kubectl annotate clusters.postgresql.cnpg.io … cnpg.io/hibernation=on`
// plus `kubectl delete pod …` that operators ran by hand after each training
// kernel reseed (crea-sim ADR-0008 §5).
//
// Closed request: scope.project (slug) and args.target (addon UUID); apply
// adds args.expected_plan (the dry-run's plan_hash), a reason and an
// idempotency key. Refused unless the addon belongs to that project, is
// `ready`, and has no binding that is not `deleted`: an addon a service still
// reads is never stopped. Platform rank (operator_routes.go). Apply writes an
// audit row before any mutation and refuses to mutate without it; a second row
// records the result. It never deletes a Cluster, PVC or Secret, and never
// changes the addon's status: the CNPG annotation is the record.
const opsAddonsHibernateOperation = "ops.addons.hibernate"

// hibernationRefusal is every refusal that needs only the addon row.
func hibernationRefusal(addon *types.DatabaseAddonWithBindings, projectID uuid.UUID) (int, string) {
	if addon == nil || addon.ProjectID != projectID {
		return http.StatusNotFound, "addon not found in this project"
	}
	if addon.Type != types.DatabaseAddonTypePostgres {
		return http.StatusUnprocessableEntity, "hibernation applies only to managed Postgres"
	}
	if addon.Status != types.DatabaseAddonStatusReady {
		return http.StatusConflict, fmt.Sprintf("addon is %s, not ready", addon.Status)
	}
	for _, b := range addon.Bindings {
		if b.Status != types.DatabaseAddonBindingStatusDeleted {
			return http.StatusConflict, fmt.Sprintf("addon still has a %s binding to service %s; unbind it first", b.Status, b.ServiceID)
		}
	}
	return 0, ""
}

func (h *Handler) handleOpsAddonsHibernate(ctx context.Context, req operatorOperationRequest, actor opsActor) (operatorOperationResponse, int) {
	resp := operatorOperationResponse{Operation: opsAddonsHibernateOperation, DryRun: req.DryRun, Status: "failed"}
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
	if h.repos == nil || h.repos.Projects == nil || h.addonService == nil || h.k8sClient == nil || h.k8sClient.DynamicClient == nil || h.opsKubeClient() == nil {
		resp.Summary = "addon hibernation adapter is unavailable"
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
	if status, why := hibernationRefusal(addon, project.ID); status != 0 {
		resp.Summary = why
		return resp, status
	}
	plan, err := addons.PlanPostgresHibernation(ctx, h.k8sClient.DynamicClient, h.opsKubeClient(), &addon.DatabaseAddon)
	if err != nil {
		resp.Summary = "plan not available: " + err.Error()
		return resp, http.StatusBadGateway
	}
	if req.DryRun || plan.Done() {
		resp.Data = plan
		switch {
		case plan.Done():
			resp.Status, resp.Summary = "succeeded", "already hibernated: nothing left to stop"
		default:
			resp.Status, resp.Summary = "ready_to_apply", "review the cluster, pods and exporter, then apply with args.expected_plan=plan_hash"
		}
		return resp, http.StatusOK
	}
	details := map[string]any{
		"operation": opsAddonsHibernateOperation, "addon_id": addonID.String(), "project": slug,
		"namespace": plan.Namespace, "cluster": plan.Cluster, "plan_hash": plan.PlanHash,
		"reason": strings.TrimSpace(req.Reason), "idempotency_key": strings.TrimSpace(req.IdempotencyKey),
	}
	auditID, err := h.writeOpsAudit(ctx, actor, opsAddonsHibernateOperation, "requested", addonID.String(), addon.Name, details)
	if err != nil {
		resp.Summary = "the audit record could not be written, so nothing was changed"
		return resp, http.StatusServiceUnavailable
	}
	resp.AuditID = auditID
	result, applyErr := addons.ApplyPostgresHibernation(ctx, h.k8sClient.DynamicClient, h.opsKubeClient(), &addon.DatabaseAddon, expected)
	outcome := map[string]any{"request_audit_id": auditID}
	for k, v := range details {
		outcome[k] = v
	}
	if result != nil {
		outcome["annotated"], outcome["pods_deleted"], outcome["exporter_scaled_to_zero"] = result.Annotated, result.PodsDeleted, result.ExporterOff
	}
	if applyErr != nil {
		outcome["error"] = applyErr.Error()
		_, _ = h.writeOpsAudit(ctx, actor, opsAddonsHibernateOperation, "failure", addonID.String(), addon.Name, outcome)
		resp.Data = result
		resp.Summary = applyErr.Error()
		if errors.Is(applyErr, addons.ErrHibernationPlanChanged) {
			return resp, http.StatusConflict
		}
		return resp, http.StatusBadGateway
	}
	if _, err := h.writeOpsAudit(ctx, actor, opsAddonsHibernateOperation, "success", addonID.String(), addon.Name, outcome); err != nil {
		resp.Warnings = append(resp.Warnings, "the result audit row was not written (the request row "+auditID+" exists): "+err.Error())
	}
	resp.Status, resp.Summary, resp.Data = "succeeded", "hibernated: annotation on, instance pods stopped, exporter at zero; the PVC is kept", result
	return resp, http.StatusOK
}

// writeOpsAudit inserts one audit row for an ops adapter and returns its ID.
func (h *Handler) writeOpsAudit(ctx context.Context, actor opsActor, action, outcome, resourceID, name string, details map[string]any) (string, error) {
	if h.repos == nil || h.repos.AuditLogs == nil {
		return "", fmt.Errorf("audit store unavailable")
	}
	actorID, err := uuid.Parse(actor.ID)
	if err != nil {
		return "", fmt.Errorf("authenticated actor required")
	}
	entry := &types.AuditLog{
		ActorID: &actorID, ActorEmail: actor.Email, ActorRole: types.Role(auth.RolePlatformAdmin),
		Action: action, ResourceType: "database_addon", ResourceID: resourceID, ResourceName: name,
		IPAddress: actor.IP, UserAgent: actor.UserAgent, Outcome: outcome, Context: details,
	}
	if actor.TokenName != "" {
		entry.Metadata = map[string]any{"api_token_name": actor.TokenName}
	}
	if err := h.repos.AuditLogs.Log(ctx, entry); err != nil {
		return "", fmt.Errorf("audit write failed: %w", err)
	}
	return entry.ID.String(), nil
}
