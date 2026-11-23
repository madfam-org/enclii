package api

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
)

func registerKarafielProductionTargetAdminRoutes(platform *gin.RouterGroup, h *Handler) {
	platform.POST("/projects/:slug/environments/production/target-enrollment", h.RequirePlatformOperator(), h.KarafielProductionTargetEnrollment)
}

func closedKarafielProductionRequest(raw []byte) (workloadtargets.Request, error) {
	r, err := workloadtargets.ParseRequest(raw)
	if err != nil || r.Project != "karafiel" || r.Environment != "production" || r.Namespace != "karafiel" || !workloadtargets.KarafielProductionDeployment(r.Deployment) {
		return workloadtargets.Request{}, workloadtargets.ErrIntent
	}
	return r, nil
}

func karafielProductionSelectedPlan(s []workloadtargets.Snapshot, r workloadtargets.Request) (workloadtargets.Plan, error) {
	return karafielProductionSelectedPlanWithDiagnostic(s, r, nil)
}

func karafielProductionSelectedPlanWithDiagnostic(s []workloadtargets.Snapshot, r workloadtargets.Request, diagnostic *workloadtargets.ProductionFenceDiagnostic) (workloadtargets.Plan, error) {
	plans, err := workloadtargets.ValidateKarafielProductionFleetWithDiagnostic(s, diagnostic)
	if err != nil {
		return workloadtargets.Plan{}, err
	}
	for _, p := range plans {
		if p.Identity.ServiceID == r.ServiceID && p.Identity.Deployment == r.Deployment {
			return p, nil
		}
	}
	return workloadtargets.Plan{}, diagnostic.ValidationFailure("selected_target", workloadtargets.ErrIntent)
}

func productionTargetEnrollmentResponse(receipt *workloadtargets.Receipt, replay bool) gin.H {
	return gin.H{"schema": "enclii-karafiel-production-writer-fence-v1", "phase": "WRITER_EXCLUSION_REGISTERED", "activation_held": true, "namespace_ownership_pending": receipt.Binding.Identity.NamespaceOwnershipPending, "dry_run": false, "idempotent": replay, "receipt": receipt}
}

// This closed compatibility path persists only a native043 binding/event for
// an existing Service. It does not repair any repository/Service/env metadata,
// change flags, stop Argo, deploy, relabel a namespace or claim readiness.
func (h *Handler) KarafielProductionTargetEnrollment(c *gin.Context) {
	actor, ok := h.karafielStageRegistrationOperator(c)
	if !ok {
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 8192))
	r, parseErr := closedKarafielProductionRequest(raw)
	if err != nil || parseErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "closed_production_target_enrollment_required"})
		return
	}
	if !r.DryRun {
		h.applyKarafielProductionTargetEnrollment(c, actor, r)
		return
	}
	diagnostic := &workloadtargets.ProductionFenceDiagnostic{}
	snapshots, err := h.readKarafielProductionTargetSnapshotsWithDiagnostic(c.Request.Context(), diagnostic)
	if err != nil {
		karafielProductionDryRefusal(c, err, diagnostic)
		return
	}
	plan, err := karafielProductionSelectedPlanWithDiagnostic(snapshots, r, diagnostic)
	if err != nil {
		karafielProductionDryRefusal(c, err, diagnostic)
		return
	}
	c.JSON(http.StatusOK, gin.H{"schema": "enclii-karafiel-production-writer-fence-plan-v1", "phase": "WRITER_EXCLUSION_PLAN", "activation_held": true, "namespace_ownership_pending": plan.Identity.NamespaceOwnershipPending, "dry_run": true, "plan": plan})
}

func (h *Handler) applyKarafielProductionTargetEnrollment(c *gin.Context, actor uuid.UUID, r workloadtargets.Request) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	project, err := h.repos.Projects.GetBySlug("karafiel")
	if err != nil || project == nil {
		h.targetPlanRefusal(c, workloadtargets.ErrIntent)
		return
	}
	session, err := (workloadtargets.Store{DB: h.repos.DB()}).LockProject(ctx, project.ID)
	if err != nil {
		h.targetPlanRefusal(c, err)
		return
	}
	defer session.Close()
	if session.RequireOperator(ctx, actor) != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "production_target_enrollment_refused"})
		return
	}
	var active bool
	if session.Tx().QueryRowContext(ctx, `SELECT active FROM users WHERE id=$1 FOR SHARE`, actor).Scan(&active) != nil || !active {
		c.JSON(http.StatusForbidden, gin.H{"error": "production_target_enrollment_refused"})
		return
	}
	want, err := workloadtargets.RequestHash(r, actor)
	if err != nil {
		h.targetPlanRefusal(c, err)
		return
	}
	previous, err := session.Store().Receipt(ctx, r.IdempotencyID)
	if err != nil {
		h.targetPlanRefusal(c, err)
		return
	}
	if previous != nil {
		if previous.RequestSHA != want || previous.Binding.Identity.SourceAuthority != workloadtargets.KarafielProductionFenceAuthority || previous.Binding.Identity.ProjectID != project.ID {
			h.targetPlanRefusal(c, workloadtargets.ErrConflict)
			return
		}
		c.JSON(http.StatusOK, productionTargetEnrollmentResponse(previous, true))
		return
	}
	scoped := *h
	scoped.repos = h.repos.InTransaction(session.Tx())
	scoped.workloadTargetSession = session
	source := newKarafielProductionSourceReader()
	snapshots, err := scoped.readKarafielProductionTargetSnapshotsWithSource(ctx, nil, source)
	if err != nil {
		h.targetPlanRefusal(c, err)
		return
	}
	if err = session.LockIntent(ctx, snapshots[0].Environment.ID, snapshots[0].Registration.ID); err != nil {
		h.targetPlanRefusal(c, err)
		return
	}
	snapshots, err = scoped.readKarafielProductionTargetSnapshotsWithSource(ctx, nil, source)
	if err != nil {
		h.targetPlanRefusal(c, err)
		return
	}
	plan, err := karafielProductionSelectedPlan(snapshots, r)
	if err != nil {
		h.targetPlanRefusal(c, err)
		return
	}
	if plan.SHA != r.ExpectedPlan {
		h.targetPlanRefusal(c, workloadtargets.ErrConflict)
		return
	}
	if err = session.SelectServiceForBinding(ctx, r.ServiceID); err != nil {
		h.targetPlanRefusal(c, err)
		return
	}
	receipt, err := session.Persist(ctx, r, actor, plan)
	if err != nil {
		h.targetPlanRefusal(c, err)
		return
	}
	if err = session.Commit(); err != nil {
		existing, readErr := h.workloadTargetStore().Receipt(ctx, r.IdempotencyID)
		if readErr != nil || existing == nil || existing.RequestSHA != want || existing.Binding.Identity.SourceAuthority != workloadtargets.KarafielProductionFenceAuthority {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "production_target_enrollment_commit_unknown", "idempotency_key": r.IdempotencyID})
			return
		}
		receipt = existing
	}
	c.JSON(http.StatusOK, productionTargetEnrollmentResponse(receipt, false))
}

// Dry failures alone receive static diagnostic categories. Apply uses the
// unchanged ordinary refusal renderer and never allocates a diagnostic sink.
func karafielProductionDryRefusal(c *gin.Context, err error, diagnostic *workloadtargets.ProductionFenceDiagnostic) {
	status, body := targetPlanRefusalResponse(err)
	if stage, reason, ok := diagnostic.Fields(); ok {
		body["diagnostic_stage"], body["diagnostic_reason"] = stage, reason
	}
	c.JSON(status, body)
}
