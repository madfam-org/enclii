package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ops.addons.delete-retired offers a retired managed-Postgres addon to the
// existing retention-hold delete (DeleteAddonWithOptions without Force): the
// addon becomes pending_deletion, its CNPG Cluster and PVCs stay for
// DefaultDeletionRetention, and the addon reconciler finalizes the teardown
// once the window elapses (FinalizeExpiredDeletion). It never forces.
//
// It exists because the generic DELETE /v1/addons/:id carries no reason, no
// reviewed plan and no idempotency key, does not refuse an addon a service
// still reads, and its records are best-effort or written after the change
// (addon_handlers.go DeleteAddon, addon_service.go emitEventWithActor,
// audit/middleware.go). An automated caller (crea-sim's fleet reconciler,
// ruling of 2026-11-21: retired kernel databases are deleted 14 days after
// hibernation) needs a closed request instead, the same shape as
// ops.addons.hibernate.
//
// Closed request: scope.project (slug) and args.target (addon UUID); apply
// adds args.expected_plan (the dry-run's plan_hash), a reason and an
// idempotency key. Refused unless the addon belongs to that project, is a
// provisioned managed Postgres the hold can retain, has no binding that is not
// `deleted`, is `ready`, and its Cluster carries cnpg.io/hibernation=on
// ("retired" means hibernated; a running database is `blocked`). An addon
// already `pending_deletion` answers `succeeded` without writing, so retries
// are idempotent. Apply writes an audit row before the delete and refuses to
// delete without it; a second row records the result. Platform rank
// (operator_routes.go).
const opsAddonsDeleteRetiredOperation = "ops.addons.delete-retired"

// retiredAddonBinding is one binding in the plan (every one is `deleted`).
type retiredAddonBinding struct {
	ID         string `json:"id"`
	ServiceID  string `json:"service_id"`
	EnvVarName string `json:"env_var_name"`
	Status     string `json:"status"`
}

// retiredAddonDeletionPlan is the deterministic, secret-free plan. PlanHash
// covers every field except HibernationConfirmed (CNPG's condition is
// informational; the annotation is the retirement record) and itself.
type retiredAddonDeletionPlan struct {
	AddonID              string                `json:"addon_id"`
	Name                 string                `json:"name"`
	Project              string                `json:"project"`
	Namespace            string                `json:"namespace"`
	Cluster              string                `json:"cluster"`
	ClusterUID           string                `json:"cluster_uid"`
	Status               string                `json:"status"`
	Hibernated           bool                  `json:"hibernated"`
	HibernationConfirmed bool                  `json:"hibernation_confirmed"`
	Bindings             []retiredAddonBinding `json:"bindings"`
	RetentionHours       int                   `json:"retention_hours"`
	PlanHash             string                `json:"plan_hash"`
}

func retiredAddonPlanHash(p retiredAddonDeletionPlan) string {
	p.PlanHash = ""
	p.HibernationConfirmed = false
	raw, _ := json.Marshal(p)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func retentionHours() int { return int(addons.DefaultDeletionRetention / time.Hour) }

// deleteRetiredRefusal is every refusal that needs only the addon row. It
// reports alreadyScheduled for an addon in the retention hold (idempotent
// success, nothing to write).
func deleteRetiredRefusal(addon *types.DatabaseAddonWithBindings, projectID uuid.UUID) (status int, why string, alreadyScheduled bool) {
	if addon == nil || addon.ProjectID != projectID {
		return http.StatusNotFound, "addon not found in this project", false
	}
	if addon.Type != types.DatabaseAddonTypePostgres {
		return http.StatusUnprocessableEntity, "delete-retired applies only to managed Postgres", false
	}
	if !addons.SupportsRetentionHold(&addon.DatabaseAddon) {
		return http.StatusUnprocessableEntity, "the addon has no provisioned cluster the retention hold can keep (a plain delete would remove it at once); not offered", false
	}
	for _, b := range addon.Bindings {
		if b.Status != types.DatabaseAddonBindingStatusDeleted {
			return http.StatusConflict, fmt.Sprintf("addon still has a %s binding to service %s; unbind it first", b.Status, b.ServiceID), false
		}
	}
	if addon.Status == types.DatabaseAddonStatusPendingDeletion {
		return 0, "", true
	}
	if addon.Status != types.DatabaseAddonStatusReady {
		return http.StatusConflict, fmt.Sprintf("addon is %s, not ready", addon.Status), false
	}
	return 0, "", false
}

// deleteRetiredDeps are the adapters the operation uses; the handler builds
// them from its repositories, addon service and Kubernetes client.
type deleteRetiredDeps struct {
	projectID   func(slug string) (uuid.UUID, bool)
	addon       func(ctx context.Context, id uuid.UUID) (*types.DatabaseAddonWithBindings, error)
	hibernation func(ctx context.Context, addon *types.DatabaseAddon) (*addons.HibernationState, error)
	// scheduleDeletion is the non-forced DeleteAddonWithOptions.
	scheduleDeletion func(ctx context.Context, id uuid.UUID) error
	// audit is nil when the audit store is not configured.
	audit func(ctx context.Context, outcome, resourceID, name string, details map[string]any) (string, error)
}

func (h *Handler) deleteRetiredDeps(actor opsActor) (deleteRetiredDeps, bool) {
	if h == nil || h.repos == nil || h.repos.Projects == nil || h.addonService == nil || h.k8sClient == nil || h.k8sClient.DynamicClient == nil {
		return deleteRetiredDeps{}, false
	}
	svc, dyn := h.addonService, h.k8sClient.DynamicClient
	d := deleteRetiredDeps{
		projectID: func(slug string) (uuid.UUID, bool) {
			p, err := h.repos.Projects.GetBySlug(slug)
			if err != nil || p == nil {
				return uuid.Nil, false
			}
			return p.ID, true
		},
		addon: svc.GetAddonWithBindings,
		hibernation: func(ctx context.Context, addon *types.DatabaseAddon) (*addons.HibernationState, error) {
			return addons.ReadPostgresHibernationState(ctx, dyn, addon)
		},
		scheduleDeletion: func(ctx context.Context, id uuid.UUID) error {
			return svc.DeleteAddonWithOptions(ctx, id, opsEventActor(actor), addons.DeleteAddonOptions{Force: false})
		},
	}
	if h.repos.AuditLogs != nil {
		d.audit = func(ctx context.Context, outcome, resourceID, name string, details map[string]any) (string, error) {
			return h.writeOpsAudit(ctx, actor, opsAddonsDeleteRetiredOperation, outcome, resourceID, name, details)
		}
	}
	return d, true
}

// opsEventActor attributes the addon ledger event to the operator.
func opsEventActor(actor opsActor) addons.EventActor {
	ev := addons.EventActor{UserEmail: actor.Email}
	if id, err := uuid.Parse(actor.ID); err == nil {
		ev.UserID = &id
	}
	return ev
}

func (h *Handler) handleOpsAddonsDeleteRetired(ctx context.Context, req operatorOperationRequest, actor opsActor) (operatorOperationResponse, int) {
	resp := operatorOperationResponse{Operation: opsAddonsDeleteRetiredOperation, DryRun: req.DryRun, Status: "failed"}
	if status, ok := validateDeleteRetiredRequest(req, &resp); !ok {
		return resp, status
	}
	deps, ok := h.deleteRetiredDeps(actor)
	if !ok {
		resp.Summary = "addon delete-retired adapter is unavailable"
		return resp, http.StatusServiceUnavailable
	}
	return runOpsAddonsDeleteRetired(ctx, req, deps)
}

func validateDeleteRetiredRequest(req operatorOperationRequest, resp *operatorOperationResponse) (int, bool) {
	slug := strings.TrimSpace(req.Scope["project"])
	if _, err := uuid.Parse(operationTarget(req)); err != nil || slug == "" {
		resp.Summary = "scope.project (slug) and args.target (addon UUID) are required"
		return http.StatusBadRequest, false
	}
	if !req.DryRun && (strings.TrimSpace(req.Reason) == "" || strings.TrimSpace(req.IdempotencyKey) == "" || operationArg(req, "expected_plan", "expected-plan") == "") {
		resp.Summary = "apply requires reason, idempotency_key and args.expected_plan from a reviewed dry-run"
		return http.StatusBadRequest, false
	}
	return 0, true
}

func runOpsAddonsDeleteRetired(ctx context.Context, req operatorOperationRequest, d deleteRetiredDeps) (operatorOperationResponse, int) {
	resp := operatorOperationResponse{Operation: opsAddonsDeleteRetiredOperation, DryRun: req.DryRun, Status: "failed"}
	if status, ok := validateDeleteRetiredRequest(req, &resp); !ok {
		return resp, status
	}
	slug := strings.TrimSpace(req.Scope["project"])
	addonID := uuid.MustParse(operationTarget(req))
	expected := operationArg(req, "expected_plan", "expected-plan")
	if !req.DryRun && d.audit == nil {
		resp.Summary = "the audit store is not configured; nothing was changed"
		return resp, http.StatusServiceUnavailable
	}
	projectID, ok := d.projectID(slug)
	if !ok {
		resp.Summary = "project not found"
		return resp, http.StatusNotFound
	}
	addon, err := d.addon(ctx, addonID)
	if err != nil {
		resp.Summary = "addon not found in this project"
		return resp, http.StatusNotFound
	}
	status, why, scheduled := deleteRetiredRefusal(addon, projectID)
	if status != 0 {
		resp.Summary = why
		return resp, status
	}
	if scheduled {
		resp.Status = "succeeded"
		resp.Summary = "already scheduled: the addon is in the retention hold; nothing was changed"
		resp.Data = map[string]any{"addon_id": addonID.String(), "name": addon.Name, "already_scheduled": true,
			"deletion_scheduled_at": addon.DeletionScheduledAt, "retention_hours": retentionHours()}
		return resp, http.StatusOK
	}
	state, err := d.hibernation(ctx, &addon.DatabaseAddon)
	if err != nil {
		resp.Summary = "plan not available: " + err.Error()
		return resp, http.StatusBadGateway
	}
	plan := retiredAddonDeletionPlan{
		AddonID: addonID.String(), Name: addon.Name, Project: slug,
		Namespace: state.Namespace, Cluster: state.Cluster, ClusterUID: state.ClusterUID,
		Status: string(addon.Status), Hibernated: state.Annotated, HibernationConfirmed: state.Confirmed,
		Bindings: []retiredAddonBinding{}, RetentionHours: retentionHours(),
	}
	for _, b := range addon.Bindings {
		plan.Bindings = append(plan.Bindings, retiredAddonBinding{ID: b.ID.String(), ServiceID: b.ServiceID.String(), EnvVarName: b.EnvVarName, Status: string(b.Status)})
	}
	sort.Slice(plan.Bindings, func(i, j int) bool {
		if plan.Bindings[i].ServiceID != plan.Bindings[j].ServiceID {
			return plan.Bindings[i].ServiceID < plan.Bindings[j].ServiceID
		}
		return plan.Bindings[i].ID < plan.Bindings[j].ID
	})
	plan.PlanHash = retiredAddonPlanHash(plan)
	resp.Data = plan
	if !plan.Hibernated {
		resp.Status = "blocked"
		resp.Summary = fmt.Sprintf("the Cluster %s/%s does not carry %s=on: it is not retired (hibernate it first with ops.addons.hibernate); nothing was changed", plan.Namespace, plan.Cluster, addons.CNPGHibernationAnnotation)
		return resp, http.StatusConflict
	}
	if req.DryRun {
		resp.Status = "ready_to_apply"
		resp.Summary = fmt.Sprintf("review the addon, its bindings and the hibernated cluster, then apply with args.expected_plan=plan_hash; it enters the %d h retention hold, then the reconciler tears it down", plan.RetentionHours)
		return resp, http.StatusOK
	}
	if expected != plan.PlanHash {
		resp.Summary = "the addon or its cluster changed since the reviewed plan; obtain a new dry-run (nothing was changed)"
		return resp, http.StatusConflict
	}
	details := map[string]any{
		"operation": opsAddonsDeleteRetiredOperation, "addon_id": plan.AddonID, "addon_name": plan.Name, "project": slug,
		"namespace": plan.Namespace, "cluster": plan.Cluster, "cluster_uid": plan.ClusterUID, "plan_hash": plan.PlanHash,
		"retention_hours": plan.RetentionHours, "force": false,
		"reason": strings.TrimSpace(req.Reason), "idempotency_key": strings.TrimSpace(req.IdempotencyKey),
	}
	auditID, err := d.audit(ctx, "requested", plan.AddonID, plan.Name, details)
	if err != nil {
		resp.Summary = "the audit record could not be written, so nothing was changed"
		return resp, http.StatusServiceUnavailable
	}
	resp.AuditID = auditID
	outcome := map[string]any{"request_audit_id": auditID}
	for k, v := range details {
		outcome[k] = v
	}
	fail := func(msg string) (operatorOperationResponse, int) {
		outcome["error"] = msg
		if _, err := d.audit(ctx, "failure", plan.AddonID, plan.Name, outcome); err != nil {
			resp.Warnings = append(resp.Warnings, "the result audit row was not written (the request row "+auditID+" exists): "+err.Error())
		}
		resp.Summary = msg
		return resp, http.StatusBadGateway
	}
	if err := d.scheduleDeletion(ctx, addonID); err != nil {
		return fail("the retention-hold delete failed: " + err.Error())
	}
	after, err := d.addon(ctx, addonID)
	if err != nil {
		return fail("the delete was requested but the addon could not be re-read: " + err.Error())
	}
	if after.Status != types.DatabaseAddonStatusPendingDeletion {
		return fail(fmt.Sprintf("the addon is %s after the delete, not pending_deletion", after.Status))
	}
	outcome["deletion_scheduled_at"] = after.DeletionScheduledAt
	if _, err := d.audit(ctx, "success", plan.AddonID, plan.Name, outcome); err != nil {
		resp.Warnings = append(resp.Warnings, "the result audit row was not written (the request row "+auditID+" exists): "+err.Error())
	}
	resp.Status = "succeeded"
	resp.Summary = fmt.Sprintf("scheduled: the addon is pending_deletion; its cluster and PVCs are kept for %d h, then the reconciler tears it down", plan.RetentionHours)
	resp.Data = map[string]any{"addon_id": plan.AddonID, "name": plan.Name, "deletion_scheduled_at": after.DeletionScheduledAt,
		"request_audit_id": auditID, "plan_hash": plan.PlanHash, "retention_hours": plan.RetentionHours}
	return resp, http.StatusOK
}
