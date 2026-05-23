package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/monitoring"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// InstantRollbackAPIRequest is the body for a one-click instant rollback.
type InstantRollbackAPIRequest struct {
	// TargetDeploymentID is the historical Deployment to flip traffic to.
	TargetDeploymentID string `json:"target_deployment_id" binding:"required"`
	// Reason is an optional human note captured in the audit event.
	Reason string `json:"reason,omitempty"`
	// ChangeTicketURL is required when rolling back in production envs
	// (matches the DeployService approval pattern).
	ChangeTicketURL string `json:"change_ticket_url,omitempty"`
}

// InstantRollbackAPIResponse describes the outcome of a rollback.
type InstantRollbackAPIResponse struct {
	Message          string `json:"message"`
	TookMS           int64  `json:"took_ms"`
	ScaledUp         bool   `json:"scaled_up"`
	FromDeploymentID string `json:"from_deployment_id,omitempty"`
	ToDeploymentID   string `json:"to_deployment_id"`
	// P2.6: surface the Heroku-style v-numbers so CLI/UI can render
	// "v43 → v41" without a round-trip. Pointers because historical rows
	// may not have version_number allocated yet.
	FromVersion   *int   `json:"from_version,omitempty"`
	ToVersion     *int   `json:"to_version,omitempty"`
	ReadyReplicas int32  `json:"ready_replicas"`
	Strategy      string `json:"strategy"`
	Namespace     string `json:"namespace"`
}

// InstantRollback flips the K8s Service selector to route traffic to a
// historical deployment's pods. This is the "Vercel-parity" code path —
// ArgoCD reconciliation continues in the background (not interfered with).
//
// Route: POST /v1/services/:id/rollback
// (The spec names a `/projects/{id}/services/{svc}/rollback` shape — we
// kept the shorter form because the service UUID already uniquely identifies
// the target and it matches every other service endpoint in this router.)
func (h *Handler) InstantRollback(c *gin.Context) {
	ctx := c.Request.Context()

	serviceID, ok := h.mustServiceAccess(c)
	if !ok {
		return
	}

	var req InstantRollbackAPIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	targetDeploymentID, err := uuid.Parse(req.TargetDeploymentID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid target_deployment_id"})
		return
	}

	// Look up the service.
	service, err := h.repos.Services.GetByID(serviceID)
	if err != nil {
		h.logger.Error(ctx, "InstantRollback: service lookup failed", logging.Error("db_error", err))
		c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		return
	}

	// Look up the target deployment.
	targetDeployment, err := h.repos.Deployments.GetByID(ctx, targetDeploymentID.String())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Target deployment not found"})
		return
	}

	// Verify target deployment belongs to this service (defense-in-depth:
	// prevents cross-service rollback via a forged deployment ID).
	targetRelease, err := h.repos.Releases.GetByID(targetDeployment.ReleaseID)
	if err != nil || targetRelease.ServiceID != service.ID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Target deployment does not belong to this service"})
		return
	}

	// Resolve environment and namespace.
	env, err := h.repos.Environments.GetByID(ctx, targetDeployment.EnvironmentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to resolve environment"})
		return
	}
	if env.KubeNamespace == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Environment has no kube_namespace set"})
		return
	}

	// HITL guard: production rollbacks require a change ticket URL
	// (matches the DeployService convention). Staging/dev: direct.
	if isProductionEnvName(env.Name) && strings.TrimSpace(req.ChangeTicketURL) == "" {
		c.JSON(http.StatusForbidden, gin.H{
			"error":            "change_ticket_url is required for production instant-rollback",
			"environment":      env.Name,
			"hint":             "Pass change_ticket_url in the request body (same gate as production deploys).",
			"approval_pattern": "HITL",
		})
		return
	}

	// Resolve the actor (Janua subject) for audit.
	actor := ""
	if userID, err := auth.GetUserIDFromContext(c); err == nil {
		actor = userID.String()
	}

	if h.k8sClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Kubernetes client not configured"})
		return
	}

	// Execute the selector flip.
	k8sReq := k8s.InstantRollbackRequest{
		Namespace:          env.KubeNamespace,
		ServiceName:        service.Name,
		TargetDeploymentID: targetDeploymentID.String(),
		Actor:              actor,
	}
	result, err := h.k8sClient.InstantRollback(ctx, k8sReq)
	if err != nil {
		h.logger.Error(ctx, "InstantRollback: k8s flip failed",
			logging.String("service", service.Name),
			logging.String("namespace", env.KubeNamespace),
			logging.String("target", targetDeploymentID.String()),
			logging.Error("k8s_error", err))
		// Emit failed-rollback audit event.
		h.emitRollbackLifecycleEvent(service, env, targetDeployment, targetRelease, actor,
			"deploy.rollback_failed", err.Error(), nil)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Instant rollback failed", "detail": err.Error()})
		return
	}

	// Success path: write the successful rollback audit event.
	reasonPtr := &req.Reason
	if req.Reason == "" {
		reasonPtr = nil
	}
	metadata := map[string]interface{}{
		"took_ms":            result.TookMS,
		"scaled_up":          result.ScaledUp,
		"from_deployment_id": result.FromDeploymentID,
		"to_deployment_id":   targetDeploymentID.String(),
		"ready_replicas":     result.ReadyReplicas,
		"change_ticket_url":  req.ChangeTicketURL,
		"strategy":           "instant_selector_flip",
		"reason":             ptrOrEmpty(reasonPtr),
	}
	// P2.6: include the target's v-number so audit UIs can render
	// "rollback: v43 → v41" without re-querying. The "from" side isn't
	// always known here (the k8s client returns the from-deployment UUID,
	// which may or may not exist in our deployments table if this flip
	// crossed the P2.6 migration boundary).
	if targetDeployment.VersionNumber != nil {
		metadata["to_version"] = *targetDeployment.VersionNumber
		metadata["to_version_label"] = targetDeployment.VersionLabel()
	}
	if result.FromDeploymentID != "" {
		if fromUUID, perr := uuid.Parse(result.FromDeploymentID); perr == nil {
			if fromDep, derr := h.repos.Deployments.GetByID(ctx, fromUUID.String()); derr == nil && fromDep.VersionNumber != nil {
				metadata["from_version"] = *fromDep.VersionNumber
				metadata["from_version_label"] = fromDep.VersionLabel()
			}
		}
	}
	h.emitRollbackLifecycleEvent(service, env, targetDeployment, targetRelease, actor,
		"deploy.rolled_back", "", metadata)

	// Record metric — separate label from argocd-commit rollback so we can
	// observe adoption.
	monitoring.RecordDeployment(env.Name, "rollback_instant", time.Duration(result.TookMS)*time.Millisecond)

	h.logger.Info(ctx, "Instant rollback completed",
		logging.String("service", service.Name),
		logging.String("namespace", env.KubeNamespace),
		logging.String("from", result.FromDeploymentID),
		logging.String("to", targetDeploymentID.String()),
		logging.String("actor", actor),
	)

	// Extract the version pointers resolved above (re-read from metadata to
	// avoid another DB round-trip).
	var fromVersion, toVersion *int
	if v, ok := metadata["from_version"].(int); ok {
		fromVersion = &v
	}
	if v, ok := metadata["to_version"].(int); ok {
		toVersion = &v
	}

	c.JSON(http.StatusOK, InstantRollbackAPIResponse{
		Message:          "Traffic flipped successfully",
		TookMS:           result.TookMS,
		ScaledUp:         result.ScaledUp,
		FromDeploymentID: result.FromDeploymentID,
		ToDeploymentID:   targetDeploymentID.String(),
		FromVersion:      fromVersion,
		ToVersion:        toVersion,
		ReadyReplicas:    result.ReadyReplicas,
		Strategy:         "instant_selector_flip",
		Namespace:        env.KubeNamespace,
	})
}

// isProductionEnvName returns true for environments that require HITL
// approval (matches the convention used elsewhere in the control plane).
func isProductionEnvName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return n == "production" || n == "prod"
}

// emitRollbackLifecycleEvent writes a lifecycle event to audit the rollback.
// Non-blocking (best-effort, matches existing emit pattern).
func (h *Handler) emitRollbackLifecycleEvent(
	service *types.Service,
	env *types.Environment,
	target *types.Deployment,
	release *types.Release,
	actor string,
	eventType string,
	message string,
	metadata map[string]interface{},
) {
	serviceID := service.ID
	projectID := service.ProjectID
	releaseID := release.ID
	targetEnv := env.Name

	md := map[string]interface{}{}
	for k, v := range metadata {
		md[k] = v
	}
	if actor != "" {
		md["actor"] = actor
	}
	md["service_name"] = service.Name
	md["target_deployment_id"] = target.ID.String()

	var messagePtr *string
	if message != "" {
		m := message
		messagePtr = &m
	}

	event := &types.DeploymentLifecycleEvent{
		DeploymentID: &target.ID,
		ReleaseID:    &releaseID,
		ProjectID:    &projectID,
		ServiceID:    &serviceID,
		RepoFullName: service.GitRepo,
		CommitSHA:    release.GitSHA,
		Branch:       "",
		Ref:          "",
		TargetEnv:    &targetEnv,
		EventType:    eventType,
		Source:       types.SourcePlatform,
		Message:      messagePtr,
		Metadata:     md,
		CreatedAt:    time.Now().UTC(),
	}
	h.emitLifecycleEvent(event)
}

func ptrOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
