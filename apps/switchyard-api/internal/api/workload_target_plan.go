package api

import (
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
)

// PlanServiceDeploymentTarget plans or atomically persists an explicit target.
// Its exact route has an unconditional SQL-resolved platform operator gate.
func (h *Handler) PlanServiceDeploymentTarget(c *gin.Context) {
	data, err := io.ReadAll(io.LimitReader(c.Request.Body, 8193))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid bounded target request"})
		return
	}
	request, err := workloadtargets.ParseRequest(data)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid closed target request"})
		return
	}
	if !request.DryRun {
		h.applyServiceDeploymentTarget(c, request)
		return
	}
	snapshot, err := h.targetSnapshot(c.Request.Context(), request)
	if err != nil {
		h.targetPlanRefusal(c, err)
		return
	}
	if snapshot.Project == nil || snapshot.Project.Slug != request.Project || snapshot.Service == nil || snapshot.Service.ID != request.ServiceID ||
		snapshot.Environment == nil || snapshot.Environment.Name != request.Environment {
		h.targetPlanRefusal(c, workloadtargets.ErrIntent)
		return
	}
	plan, err := workloadtargets.ValidateSnapshot(snapshot, request.Namespace, request.Deployment)
	if err != nil {
		h.targetPlanRefusal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"operation": "ops.apps.bind-target", "status": "planned", "dry_run": true, "data": plan,
		"limits": []string{"read-only candidate; no binding persisted", "apply persists only immutable target and audit; no Kubernetes effect", "binding is not a durable stop or reset write barrier"}})
}

func (h *Handler) targetPlanRefusal(c *gin.Context, err error) {
	status := http.StatusConflict
	if errors.Is(err, workloadtargets.ErrUnavailable) {
		status = http.StatusServiceUnavailable
	}
	message := "actual workload ownership/source could not be verified"
	code := "ownership_unverified"
	if errors.Is(err, workloadtargets.ErrTrackingMethod) {
		code = "argo_tracking_unverified"
		message = "configured Argo tracking method missing or unsupported; no installation default inferred"
	}
	if errors.Is(err, workloadtargets.ErrIntent) {
		message = "persisted registration/environment ownership intent missing or conflicting"
	}
	body := gin.H{"operation": "ops.apps.bind-target", "status": "refused", "error": message, "refusal_code": code}
	h.addTargetRefusalDetail(c, "ops.apps.bind-target", err, body)
	c.JSON(status, body)
}

// addTargetRefusalDetail adds refusal_detail, the value-free name of the
// ownership check that refused (workloadtargets/refusal.go), and logs it with
// the underlying read error. error and refusal_code keep their values.
func (h *Handler) addTargetRefusalDetail(c *gin.Context, operation string, err error, body gin.H) {
	reason, ok := workloadtargets.RefusalReason(err)
	if !ok {
		return
	}
	body["refusal_detail"] = string(reason)
	if h.logger == nil {
		return
	}
	fields := []logging.Field{logging.String("operation", operation), logging.String("refusal_detail", string(reason))}
	if cause := workloadtargets.RefusalCause(err); cause != nil {
		fields = append(fields, logging.Error("cause", cause))
	}
	h.logger.Warn(c.Request.Context(), "workload target ownership refused", fields...)
}
