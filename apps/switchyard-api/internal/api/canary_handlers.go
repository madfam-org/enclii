package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/reconciler"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// -----------------------------------------------------------------------------
// Canary Release Handlers (P2.7)
//
// Four endpoints:
//   POST /v1/services/:id/canary                       — start
//   GET  /v1/services/:id/canary/:rollout_id           — status
//   POST /v1/services/:id/canary/:rollout_id/promote   — manual promote
//   POST /v1/services/:id/canary/:rollout_id/rollback  — manual rollback
//
// Canary advancement happens in the background reconciler tick (not during
// the HTTP request) — the POST /canary call returns as soon as the rollout
// record is persisted. Poll GET /canary/:rollout_id or stream via the CLI's
// `enclii canary status` to follow progress.
// -----------------------------------------------------------------------------

// StartCanaryRequest is the body for POST /v1/services/:id/canary.
type StartCanaryRequest struct {
	// Digest is the image digest of the candidate. Must already be built
	// (i.e., correspond to an existing Release). For convenience we also
	// accept a Release UUID in the same field — the handler disambiguates.
	Digest                  string  `json:"digest" binding:"required"`
	Percentage              int     `json:"percentage" binding:"required"`
	ValidationWindowMinutes int     `json:"validation_window_minutes"`
	SmokeEndpoint           string  `json:"smoke_endpoint,omitempty"`
	ErrorRateThreshold      float64 `json:"error_rate_threshold,omitempty"`
	EnvironmentName         string  `json:"environment_name,omitempty"` // defaults to "production"
	ChangeTicketURL         string  `json:"change_ticket_url,omitempty"`
	// TotalReplicas overrides the current service replica count. Optional —
	// the reconciler reads the live Deployment spec if unset.
	TotalReplicas int `json:"total_replicas,omitempty"`
}

// CanaryRolloutResponse mirrors the persisted rollout.
type CanaryRolloutResponse struct {
	*types.CanaryRollout
	// ActualPercentage is the effective traffic share after replica rounding.
	ActualPercentage float64 `json:"actual_percentage"`
}

// StartCanary initiates a canary rollout.
func (h *Handler) StartCanary(c *gin.Context) {
	ctx := c.Request.Context()

	serviceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service ID"})
		return
	}

	var req StartCanaryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Default values
	if req.ValidationWindowMinutes == 0 {
		req.ValidationWindowMinutes = 10
	}
	if req.ErrorRateThreshold == 0 {
		req.ErrorRateThreshold = 0.05
	}
	if req.EnvironmentName == "" {
		req.EnvironmentName = "production"
	}

	// Validate the spec
	spec := types.CanaryRolloutSpec{
		ImageDigest:             req.Digest,
		Percentage:              req.Percentage,
		ValidationWindowMinutes: req.ValidationWindowMinutes,
		SmokeEndpoint:           req.SmokeEndpoint,
		ErrorRateThreshold:      req.ErrorRateThreshold,
		ChangeTicketURL:         req.ChangeTicketURL,
	}
	if err := reconciler.ValidateRolloutSpec(spec); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Look up the service
	service, err := h.repos.Services.GetByID(serviceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service not found"})
		return
	}

	// Resolve environment
	env, err := h.repos.Environments.GetByProjectAndName(service.ProjectID, req.EnvironmentName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "environment not found: " + req.EnvironmentName})
		return
	}

	// Production HITL gate — matches InstantRollback and DeployService.
	if isProductionEnvName(env.Name) && strings.TrimSpace(req.ChangeTicketURL) == "" {
		c.JSON(http.StatusForbidden, gin.H{
			"error":       "change_ticket_url is required for production canary rollouts",
			"environment": env.Name,
		})
		return
	}

	// Prevent overlap: only one active rollout per service.
	if active, err := h.repos.CanaryRollouts.GetActiveByService(ctx, serviceID); err == nil && active != nil {
		c.JSON(http.StatusConflict, gin.H{
			"error":              fmt.Sprintf("service already has an active canary rollout (state=%s)", active.State),
			"active_rollout_id":  active.ID,
			"active_rollout_url": fmt.Sprintf("/v1/services/%s/canary/%s", serviceID, active.ID),
		})
		return
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "check active rollout: " + err.Error()})
		return
	}

	// Find the current stable deployment for this service+env.
	stableDepl, err := h.resolveStableDeployment(ctx, service.ID, env.ID)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "cannot find running stable deployment: " + err.Error()})
		return
	}

	// Resolve the canary Release: the `Digest` field may be either a digest
	// or a release UUID. Try both.
	canaryRelease, err := h.resolveCanaryRelease(ctx, service.ID, req.Digest)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "resolve canary release: " + err.Error()})
		return
	}
	if canaryRelease.Status != types.ReleaseStatusReady {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("canary release %s is not ready (status=%s)", canaryRelease.ID, canaryRelease.Status)})
		return
	}

	// Create a Deployment record for the canary (ties to the existing
	// Release → Deployment lineage; the canary Deployment K8s resource is
	// created by the reconciler on its first tick).
	canaryDeployment := &types.Deployment{
		ReleaseID:     canaryRelease.ID,
		EnvironmentID: env.ID,
		Replicas:      1,
		Status:        types.DeploymentStatusPending,
		Health:        types.HealthStatusUnknown,
	}
	if err := h.repos.Deployments.Create(canaryDeployment); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create canary deployment: " + err.Error()})
		return
	}

	// Compute replica split. Use TotalReplicas override or service.DesiredReplicas or 2.
	total := req.TotalReplicas
	if total == 0 {
		total = service.DesiredReplicas
	}
	if total < 2 {
		total = 2
	}
	split, err := reconciler.ComputeCanarySplit(total, req.Percentage)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Resolve initiator for audit.
	var initiator *uuid.UUID
	if uid, err := auth.GetUserIDFromContext(c); err == nil {
		initiator = &uid
	}

	rollout := &types.CanaryRollout{
		ServiceID:               serviceID,
		EnvironmentID:           env.ID,
		StableDeploymentID:      stableDepl.ID,
		CanaryDeploymentID:      canaryDeployment.ID,
		CanaryDigest:            req.Digest,
		CanaryPercentage:        req.Percentage,
		TotalReplicas:           split.Total,
		CanaryReplicas:          split.Canary,
		StableReplicas:          split.Stable,
		ValidationWindowSeconds: req.ValidationWindowMinutes * 60,
		SmokeEndpoint:           req.SmokeEndpoint,
		ErrorRateThreshold:      req.ErrorRateThreshold,
		State:                   types.CanaryStatePending,
		InitiatedBy:             initiator,
		ChangeTicketURL:         req.ChangeTicketURL,
	}
	if err := h.repos.CanaryRollouts.Create(ctx, rollout); err != nil {
		if strings.Contains(err.Error(), "idx_canary_one_active_per_service") {
			c.JSON(http.StatusConflict, gin.H{"error": "service already has an active canary rollout"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "persist rollout: " + err.Error()})
		return
	}

	h.logger.Info(ctx, "canary rollout started",
		logging.String("rollout_id", rollout.ID.String()),
		logging.String("service", service.Name),
		logging.String("env", env.Name),
	)

	c.JSON(http.StatusCreated, CanaryRolloutResponse{
		CanaryRollout:    rollout,
		ActualPercentage: split.ActualPercentage,
	})
}

// GetCanary returns the current state of a rollout.
func (h *Handler) GetCanary(c *gin.Context) {
	ctx := c.Request.Context()
	rolloutID, err := uuid.Parse(c.Param("rollout_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid rollout ID"})
		return
	}
	ro, err := h.repos.CanaryRollouts.GetByID(ctx, rolloutID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "rollout not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !h.enforceServiceAccess(c, ro.ServiceID) {
		return
	}
	actualPct := 0.0
	if ro.TotalReplicas > 0 {
		actualPct = float64(ro.CanaryReplicas) * 100.0 / float64(ro.TotalReplicas)
	}
	c.JSON(http.StatusOK, CanaryRolloutResponse{
		CanaryRollout:    ro,
		ActualPercentage: actualPct,
	})
}

// PromoteCanary short-circuits the validation window and promotes now.
func (h *Handler) PromoteCanary(c *gin.Context) {
	ctx := c.Request.Context()

	serviceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service ID"})
		return
	}
	rolloutID, err := uuid.Parse(c.Param("rollout_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid rollout ID"})
		return
	}

	ro, err := h.repos.CanaryRollouts.GetByID(ctx, rolloutID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "rollout not found"})
		return
	}
	if ro.ServiceID != serviceID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "rollout does not belong to service"})
		return
	}
	if ro.State.IsTerminal() {
		c.JSON(http.StatusConflict, gin.H{"error": "rollout already terminal: " + string(ro.State)})
		return
	}
	if ro.State != types.CanaryStateValidating && ro.State != types.CanaryStateRunning {
		c.JSON(http.StatusConflict, gin.H{"error": "promote is only allowed from validating/running (got " + string(ro.State) + ")"})
		return
	}

	service, err := h.repos.Services.GetByID(serviceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	env, err := h.repos.Environments.GetByID(ctx, ro.EnvironmentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Look up Release refs for digest → image resolution during promotion.
	canaryDepl, err := h.repos.Deployments.GetByID(ctx, ro.CanaryDeploymentID.String())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "canary deployment: " + err.Error()})
		return
	}
	canaryRel, err := h.repos.Releases.GetByID(canaryDepl.ReleaseID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "canary release: " + err.Error()})
		return
	}

	rec := reconciler.NewCanaryReconciler(h.k8sClient, h.repos, nil)
	if err := rec.ManualPromote(ctx, ro, service, env, nil, canaryRel); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "promote: " + err.Error()})
		return
	}

	h.logger.Info(ctx, "canary manually promoted",
		logging.String("rollout_id", ro.ID.String()),
		logging.String("service", service.Name),
	)

	ro2, _ := h.repos.CanaryRollouts.GetByID(ctx, ro.ID)
	c.JSON(http.StatusOK, gin.H{
		"message": "canary promoted",
		"rollout": ro2,
	})
}

// RollbackCanary aborts the rollout and scales canary to 0.
func (h *Handler) RollbackCanary(c *gin.Context) {
	ctx := c.Request.Context()

	serviceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service ID"})
		return
	}
	rolloutID, err := uuid.Parse(c.Param("rollout_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid rollout ID"})
		return
	}

	var req struct {
		Reason string `json:"reason,omitempty"`
	}
	_ = c.ShouldBindJSON(&req) // body optional

	ro, err := h.repos.CanaryRollouts.GetByID(ctx, rolloutID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "rollout not found"})
		return
	}
	if ro.ServiceID != serviceID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "rollout does not belong to service"})
		return
	}
	if ro.State.IsTerminal() {
		c.JSON(http.StatusConflict, gin.H{"error": "rollout already terminal: " + string(ro.State)})
		return
	}

	service, err := h.repos.Services.GetByID(serviceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	env, err := h.repos.Environments.GetByID(ctx, ro.EnvironmentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	rec := reconciler.NewCanaryReconciler(h.k8sClient, h.repos, nil)
	if err := rec.ManualRollback(ctx, ro, service, env, req.Reason); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "rollback: " + err.Error()})
		return
	}

	h.logger.Info(ctx, "canary manually rolled back",
		logging.String("rollout_id", ro.ID.String()),
		logging.String("service", service.Name),
		logging.String("reason", req.Reason),
	)

	ro2, _ := h.repos.CanaryRollouts.GetByID(ctx, ro.ID)
	c.JSON(http.StatusOK, gin.H{
		"message": "canary rolled back",
		"rollout": ro2,
	})
}

// ListServiceCanaries returns rollout history for a service.
func (h *Handler) ListServiceCanaries(c *gin.Context) {
	ctx := c.Request.Context()
	serviceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service ID"})
		return
	}
	rollouts, err := h.repos.CanaryRollouts.ListByService(ctx, serviceID, 50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"rollouts": rollouts})
}

// -------------------------------------------------------------------------
// Internals
// -------------------------------------------------------------------------

// resolveStableDeployment finds the currently running deployment for a
// service in an environment (the "stable" side of the canary split).
func (h *Handler) resolveStableDeployment(ctx context.Context, serviceID, envID uuid.UUID) (*types.Deployment, error) {
	_ = envID // future: filter by environment once repo supports it in the repo signature
	depl, err := h.repos.Deployments.GetLatestByService(ctx, serviceID.String())
	if err != nil {
		return nil, err
	}
	if depl == nil {
		return nil, fmt.Errorf("no running deployment")
	}
	return depl, nil
}

// resolveCanaryRelease resolves either a full release UUID or an image
// digest substring to a Release for the given service.
func (h *Handler) resolveCanaryRelease(ctx context.Context, serviceID uuid.UUID, digestOrID string) (*types.Release, error) {
	// Try as UUID first (cheapest)
	if relID, err := uuid.Parse(digestOrID); err == nil {
		rel, err := h.repos.Releases.GetByID(relID)
		if err == nil && rel.ServiceID == serviceID {
			return rel, nil
		}
	}
	// Fall back to substring match on image_uri. List releases, find one
	// whose image URI contains the digest. This is O(N) but N is small
	// per-service.
	_ = ctx
	rels, err := h.repos.Releases.ListByService(serviceID)
	if err != nil {
		return nil, err
	}
	for _, r := range rels {
		if r.ImageURI == digestOrID || strings.Contains(r.ImageURI, digestOrID) {
			return r, nil
		}
	}
	return nil, fmt.Errorf("no release found matching digest %q", digestOrID)
}
