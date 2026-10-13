package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func (h *Handler) workloadTargetStore() workloadtargets.Store {
	if h.workloadTargetSession != nil {
		return h.workloadTargetSession.Store()
	}
	return workloadtargets.Store{DB: h.repos.DB()}
}
func (h *Handler) targetSnapshot(ctx context.Context, r workloadtargets.Request) (workloadtargets.Snapshot, error) {
	reader := h.workloadTargetSnapshotReader
	if reader == nil {
		reader = h.readWorkloadTargetSnapshot
	}
	s, err := reader(ctx, r)
	if err != nil {
		return s, err
	}
	if s.Project == nil || s.Project.Slug != r.Project || s.Service == nil || s.Service.ID != r.ServiceID || s.Environment == nil || s.Environment.Name != r.Environment || s.Registration == nil {
		return s, workloadtargets.ErrIntent
	}
	return s, nil
}
func (h *Handler) applyServiceDeploymentTarget(c *gin.Context, r workloadtargets.Request) {
	ctx := c.Request.Context()
	actor, err := uuid.Parse(c.GetString("user_id"))
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "actual platform operator required"})
		return
	}
	session, err := (workloadtargets.Store{DB: h.repos.DB()}).LockService(ctx, r.ServiceID)
	if err != nil {
		h.targetPlanRefusal(c, err)
		return
	}
	defer session.Close()
	if err = session.RequireOperator(ctx, actor); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "actual platform operator required"})
		return
	}
	requestSHA, err := workloadtargets.RequestHash(r, actor)
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
		if previous.RequestSHA != requestSHA {
			h.targetPlanRefusal(c, workloadtargets.ErrConflict)
			return
		}
		c.JSON(http.StatusOK, gin.H{"operation": "ops.apps.bind-target", "status": "applied", "dry_run": false, "data": previous, "idempotent": true})
		return
	}
	scoped := *h
	scoped.repos = h.repos.InTransaction(session.Tx())
	scoped.workloadTargetSession = session
	snapshot, err := scoped.targetSnapshot(ctx, r)
	if err != nil {
		h.targetPlanRefusal(c, err)
		return
	}
	if err = session.LockIntent(ctx, snapshot.Environment.ID, snapshot.Registration.ID); err != nil {
		h.targetPlanRefusal(c, err)
		return
	}
	// Re-read locked authoritative rows and native/source evidence after obtaining
	// their locks. The earlier read cannot become the apply projection.
	snapshot, err = scoped.targetSnapshot(ctx, r)
	if err != nil {
		h.targetPlanRefusal(c, err)
		return
	}
	plan, err := workloadtargets.ValidateSnapshot(snapshot, r.Namespace, r.Deployment)
	if err != nil {
		h.targetPlanRefusal(c, err)
		return
	}
	if plan.SHA != r.ExpectedPlan {
		h.targetPlanRefusal(c, workloadtargets.ErrConflict)
		return
	}
	receipt, err := session.Persist(ctx, r, actor, plan)
	if err != nil {
		h.targetPlanRefusal(c, err)
		return
	}
	if err = session.Commit(); err != nil {
		// One read of the SAME UUID, never a second insert or transaction retry.
		existing, readErr := h.workloadTargetStore().Receipt(ctx, r.IdempotencyID)
		if readErr != nil || existing == nil || existing.RequestSHA != requestSHA {
			c.JSON(http.StatusServiceUnavailable, gin.H{"operation": "ops.apps.bind-target", "status": "commit_unknown", "dry_run": false, "idempotency_key": r.IdempotencyID, "error": "read the same idempotency ID; do not retry another binding"})
			return
		}
		receipt = existing
	}
	c.JSON(http.StatusOK, gin.H{"operation": "ops.apps.bind-target", "status": "applied", "dry_run": false, "data": receipt})
}

// No automatic retry maps SQL/Kubernetes uncertainty into a success claim.
func targetMutationStatus(err error) int {
	if errors.Is(err, workloadtargets.ErrOperator) {
		return http.StatusForbidden
	}
	if errors.Is(err, workloadtargets.ErrUnavailable) || errors.Is(err, workloadtargets.ErrCommitUnknown) {
		return http.StatusServiceUnavailable
	}
	if apierrors.IsConflict(err) || errors.Is(err, workloadtargets.ErrConflict) || errors.Is(err, workloadtargets.ErrIntent) || errors.Is(err, workloadtargets.ErrOwnership) || errors.Is(err, workloadtargets.ErrTrackingMethod) {
		return http.StatusConflict
	}
	return http.StatusServiceUnavailable
}
