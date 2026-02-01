package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/compliance"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/monitoring"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/provenance"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// DeployService handles service deployment requests
func (h *Handler) DeployService(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")
	serviceID, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	var req struct {
		ReleaseID       string            `json:"release_id" binding:"required"`
		Environment     map[string]string `json:"environment"`
		EnvironmentName string            `json:"environment_name"` // e.g., "production", "staging", "dev"
		Replicas        int               `json:"replicas,omitempty"`
		ChangeTicketURL string            `json:"change_ticket_url,omitempty"` // For production deployments
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Default environment name to "development" if not specified
	if req.EnvironmentName == "" {
		req.EnvironmentName = "development"
	}

	// Get service details
	service, err := h.repos.Services.GetByID(serviceID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get service", logging.Error("db_error", err))
		c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		return
	}

	// Parse release ID
	releaseID, err := uuid.Parse(req.ReleaseID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid release ID"})
		return
	}

	// Get release details
	release, err := h.repos.Releases.GetByID(releaseID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get release", logging.Error("db_error", err))
		c.JSON(http.StatusNotFound, gin.H{"error": "Release not found"})
		return
	}

	// Verify release is ready
	if release.Status != types.ReleaseStatusReady {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Release is not ready for deployment"})
		return
	}

	// Look up environment by project and name
	env, err := h.repos.Environments.GetByProjectAndName(service.ProjectID, req.EnvironmentName)
	if err != nil {
		h.logger.Error(ctx, "Failed to get environment",
			logging.String("environment_name", req.EnvironmentName),
			logging.String("project_id", service.ProjectID.String()),
			logging.Error("db_error", err))
		c.JSON(http.StatusNotFound, gin.H{
			"error":       "Environment not found",
			"environment": req.EnvironmentName,
			"hint":        "Create the environment first using POST /v1/projects/{project_id}/environments",
		})
		return
	}
	environmentID := env.ID

	// Create deployment record
	deployment := &types.Deployment{
		ID:            uuid.New(),
		ReleaseID:     releaseID,
		EnvironmentID: environmentID,
		Replicas:      req.Replicas,
		Status:        types.DeploymentStatusPending,
		Health:        types.HealthStatusUnknown,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	// Check PR approvals before deployment (if provenance checker is configured)
	if h.provenanceChecker != nil {
		approvalResult, err := h.provenanceChecker.CheckDeploymentApproval(
			ctx,
			deployment,
			release,
			service,
			req.EnvironmentName,
			req.ChangeTicketURL,
		)

		if err != nil {
			h.logger.Error(ctx, "Failed to check deployment approval", logging.Error("provenance_error", err))
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to verify deployment approval",
				"details": err.Error(),
			})
			return
		}

		if !approvalResult.Approved {
			h.logger.Warn(ctx, "Deployment blocked by approval policy",
				logging.String("environment", req.EnvironmentName),
				logging.String("service_id", serviceID.String()),
				logging.String("violations", approvalResult.Violations.Error()))

			c.JSON(http.StatusForbidden, gin.H{
				"error":             "Deployment does not meet approval requirements",
				"policy_violations": approvalResult.Violations,
				"environment":       req.EnvironmentName,
				"help":              "Ensure your PR has sufficient approvals and CI checks pass before deploying to this environment",
			})
			return
		}

		// Store approval record for audit trail
		if approvalResult.Receipt != nil {
			receiptJSON, err := approvalResult.Receipt.ToJSON()
			if err != nil {
				h.logger.Error(ctx, "Failed to serialize compliance receipt", logging.Error("receipt_error", err))
			}

			approvalRecord := &types.ApprovalRecord{
				DeploymentID:      deployment.ID,
				PRURL:             approvalResult.PRURL,
				PRNumber:          approvalResult.PRNumber,
				ApproverEmail:     approvalResult.ApproverEmail,
				ApproverName:      approvalResult.ApproverName,
				ApprovedAt:        &approvalResult.ApprovedAt,
				CIStatus:          approvalResult.CIStatus,
				ChangeTicketURL:   req.ChangeTicketURL,
				ComplianceReceipt: receiptJSON,
			}

			if err := h.repos.ApprovalRecords.Create(ctx, approvalRecord); err != nil {
				// Log error but don't block deployment - approval record is for audit only
				h.logger.Error(ctx, "Failed to store approval record", logging.Error("db_error", err))
			} else {
				h.logger.Info(ctx, "Approval record stored",
					logging.String("deployment_id", deployment.ID.String()),
					logging.String("pr_url", approvalResult.PRURL),
					logging.String("approver", approvalResult.ApproverEmail))
			}

			// Send compliance evidence to Vanta/Drata (if enabled)
			if h.complianceExporter != nil && h.complianceExporter.IsEnabled() {
				go h.sendComplianceWebhooks(ctx, deployment, release, service, req.EnvironmentName, approvalResult, receiptJSON)
			}
		}
	}

	if deployment.Replicas <= 0 {
		deployment.Replicas = 1 // Default to 1 replica
	}

	if err := h.repos.Deployments.Create(deployment); err != nil {
		h.logger.Error(ctx, "Failed to create deployment", logging.Error("db_error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create deployment"})
		return
	}

	// Schedule deployment with reconciler
	if err := h.reconciler.ScheduleReconciliation(deployment.ID.String(), 1); err != nil {
		h.logger.Warn(ctx, "Reconciler queue full, work queued for retry",
			logging.String("deployment_id", deployment.ID.String()),
			logging.Error("queue_error", err))
	}

	// Record metrics
	// TODO: Use proper metrics method
	// monitoring.RecordDeployment(req.EnvironmentName, "pending", 0)

	h.logger.Info(ctx, "Deployment created",
		logging.String("deployment_id", deployment.ID.String()),
		logging.String("service_id", serviceID.String()),
		logging.String("release_id", req.ReleaseID))

	c.JSON(http.StatusCreated, deployment)
}

// GetServiceStatus returns the current status of a service
func (h *Handler) GetServiceStatus(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")
	serviceID, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	// Check cache first
	cacheKey := fmt.Sprintf("service:status:%s", serviceID.String())
	if cached, err := h.cache.Get(ctx, cacheKey); err == nil && cached != nil {
		c.Header("X-Cache", "hit")
		c.Data(http.StatusOK, "application/json", cached)
		return
	}

	// Get service
	service, err := h.repos.Services.GetByID(serviceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		return
	}

	// Get latest deployment for this service
	latestDeployment, err := h.repos.Deployments.GetLatestByService(ctx, serviceID.String())
	if err != nil && err != sql.ErrNoRows {
		h.logger.Error(ctx, "Failed to get latest deployment", logging.Error("db_error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get service status"})
		return
	}

	status := gin.H{
		"service": service,
		"status":  "unknown",
	}

	if latestDeployment != nil {
		status["status"] = string(latestDeployment.Status)
		status["latest_deployment"] = latestDeployment

		// Get Kubernetes status if deployment is running
		if latestDeployment.Status == types.DeploymentStatusRunning {
			// Get the environment to determine the correct namespace
			env, err := h.repos.Environments.GetByID(ctx, latestDeployment.EnvironmentID)
			if err == nil && env.KubeNamespace != "" {
				namespace := env.KubeNamespace
				if pods, err := h.k8sClient.ListPods(ctx, namespace, fmt.Sprintf("enclii.dev/service=%s", service.Name)); err == nil {
					status["pods"] = pods.Items
					status["running_pods"] = len(pods.Items)
				}
			}
		}
	}

	// Cache for 30 seconds (non-critical - log but don't fail on error)
	if statusJSON, err := json.Marshal(status); err == nil {
		if cacheErr := h.cache.Set(ctx, cacheKey, statusJSON, 30*time.Second); cacheErr != nil {
			h.logger.Warn(ctx, "Failed to cache deployment status",
				logging.String("cache_key", cacheKey),
				logging.Error("error", cacheErr))
		}
	}

	c.JSON(http.StatusOK, status)
}

// GetLogs retrieves logs for a deployment
func (h *Handler) GetLogs(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")
	deploymentID, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid deployment ID"})
		return
	}

	// Get deployment
	deployment, err := h.repos.Deployments.GetByID(ctx, deploymentID.String())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Deployment not found"})
		return
	}

	// Get release to find service ID
	release, err := h.repos.Releases.GetByID(deployment.ReleaseID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get release", logging.Error("db_error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get release"})
		return
	}

	// Get service to determine namespace
	service, err := h.repos.Services.GetByID(release.ServiceID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get service", logging.Error("db_error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get service"})
		return
	}

	// Get query parameters for log options
	lines := c.DefaultQuery("lines", "100")
	follow := c.Query("follow") == "true"

	linesInt, err := strconv.Atoi(lines)
	if err != nil {
		linesInt = 100
	}

	// Get the environment to determine the correct namespace
	env, err := h.repos.Environments.GetByID(ctx, deployment.EnvironmentID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get environment", logging.Error("db_error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get environment"})
		return
	}

	namespace := env.KubeNamespace
	if namespace == "" {
		h.logger.Warn(ctx, "Environment has no kube_namespace set",
			logging.String("environment_id", deployment.EnvironmentID.String()))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Environment has no kubernetes namespace configured"})
		return
	}
	labelSelector := fmt.Sprintf("enclii.dev/service=%s", service.Name)

	// Get logs from Kubernetes
	logs, err := h.k8sClient.GetLogs(ctx, namespace, labelSelector, linesInt, follow)
	if err != nil {
		h.logger.Error(ctx, "Failed to get logs", logging.Error("k8s_error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve logs"})
		return
	}

	if follow {
		// Stream logs via SSE
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("Access-Control-Allow-Origin", "*")

		// Stream logs (simplified implementation)
		c.String(http.StatusOK, logs)
	} else {
		// Return logs as JSON
		c.JSON(http.StatusOK, gin.H{
			"deployment_id": deployment.ID,
			"service_name":  service.Name,
			"logs":          logs,
			"lines":         linesInt,
		})
	}
}

// RollbackDeployment rolls back a deployment to a previous version
func (h *Handler) RollbackDeployment(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")
	deploymentID, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid deployment ID"})
		return
	}

	// Get deployment
	deployment, err := h.repos.Deployments.GetByID(ctx, deploymentID.String())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Deployment not found"})
		return
	}

	// Get release to find service ID
	release, err := h.repos.Releases.GetByID(deployment.ReleaseID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get release", logging.Error("db_error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get release"})
		return
	}

	// Get service
	service, err := h.repos.Services.GetByID(release.ServiceID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get service", logging.Error("db_error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get service"})
		return
	}

	// Find previous successful deployment by getting all releases for the service
	// then finding deployments for those releases
	releases, err := h.repos.Releases.ListByService(release.ServiceID)
	if err != nil {
		h.logger.Error(ctx, "Failed to list releases", logging.Error("db_error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to find previous deployment"})
		return
	}

	var previousDeployment *types.Deployment
	for _, r := range releases {
		if r.ID == release.ID {
			continue // Skip current release
		}
		deployments, err := h.repos.Deployments.ListByRelease(ctx, r.ID.String())
		if err != nil {
			continue // Skip on error
		}
		for _, d := range deployments {
			if d.Status == types.DeploymentStatusRunning {
				previousDeployment = d
				break
			}
		}
		if previousDeployment != nil {
			break
		}
	}

	if previousDeployment == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No previous deployment found to rollback to"})
		return
	}

	// Update current deployment status (mark as failed since we're rolling back)
	if err := h.repos.Deployments.UpdateStatus(deployment.ID, types.DeploymentStatusFailed, types.HealthStatusUnhealthy); err != nil {
		h.logger.Error(ctx, "Failed to update deployment status", logging.Error("db_error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update deployment status"})
		return
	}

	// Trigger rollback in Kubernetes
	if h.serviceReconciler != nil {
		// Get the environment to determine the correct namespace
		env, err := h.repos.Environments.GetByID(ctx, deployment.EnvironmentID)
		if err != nil {
			h.logger.Error(ctx, "Failed to get environment for rollback", logging.Error("db_error", err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get environment"})
			return
		}
		namespace := env.KubeNamespace
		if namespace == "" {
			h.logger.Error(ctx, "Environment has no kube_namespace set for rollback",
				logging.String("environment_id", deployment.EnvironmentID.String()))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Environment has no kubernetes namespace configured"})
			return
		}
		if err := h.serviceReconciler.Rollback(ctx, namespace, service.Name); err != nil {
			h.logger.Error(ctx, "Failed to rollback in Kubernetes", logging.Error("k8s_error", err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to rollback deployment"})
			return
		}
	}

	// Clear cache (non-critical - log but don't fail on error)
	cacheKey := fmt.Sprintf("service:status:%s", service.ID.String())
	if cacheErr := h.cache.Del(ctx, cacheKey); cacheErr != nil {
		h.logger.Warn(ctx, "Failed to clear deployment cache after rollback",
			logging.String("cache_key", cacheKey),
			logging.Error("error", cacheErr))
	}

	// Record rollback metrics
	monitoring.RecordDeployment("production", "rollback", 0)

	h.logger.Info(ctx, "Deployment rolled back",
		logging.String("deployment_id", deployment.ID.String()),
		logging.String("service_id", service.ID.String()),
		logging.String("previous_deployment_id", previousDeployment.ID.String()))

	c.JSON(http.StatusOK, gin.H{
		"message":            "Deployment rolled back successfully",
		"rolled_back_to":     previousDeployment,
		"current_deployment": deployment,
	})
}

// GetLatestDeployment returns the most recent deployment for a service
func (h *Handler) GetLatestDeployment(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")
	serviceID, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	// Get latest deployment for this service
	deployment, err := h.repos.Deployments.GetLatestByService(ctx, serviceID.String())
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "No deployments found for this service"})
			return
		}
		h.logger.Error(ctx, "Failed to get latest deployment", logging.Error("db_error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve deployment"})
		return
	}

	// Get release info for version
	release, err := h.repos.Releases.GetByID(deployment.ReleaseID)
	if err == nil {
		c.JSON(http.StatusOK, gin.H{
			"deployment": deployment,
			"release":    release,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"deployment": deployment,
	})
}

// GetDeployment returns a specific deployment by ID
func (h *Handler) GetDeployment(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")
	deploymentID, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid deployment ID"})
		return
	}

	deployment, err := h.repos.Deployments.GetByID(ctx, deploymentID.String())
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Deployment not found"})
			return
		}
		h.logger.Error(ctx, "Failed to get deployment", logging.Error("db_error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve deployment"})
		return
	}

	c.JSON(http.StatusOK, deployment)
}

// ListServiceDeployments returns all deployments for a service
func (h *Handler) ListServiceDeployments(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")
	serviceID, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	// Get all releases for this service
	releases, err := h.repos.Releases.ListByService(serviceID)
	if err != nil {
		h.logger.Error(ctx, "Failed to list releases", logging.Error("db_error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve releases"})
		return
	}

	// Get deployments for each release
	var allDeployments []*types.Deployment
	for _, release := range releases {
		deployments, err := h.repos.Deployments.ListByRelease(ctx, release.ID.String())
		if err != nil {
			h.logger.Error(ctx, "Failed to list deployments", logging.Error("db_error", err))
			continue
		}
		allDeployments = append(allDeployments, deployments...)
	}

	c.JSON(http.StatusOK, gin.H{
		"service_id":  serviceID,
		"deployments": allDeployments,
		"count":       len(allDeployments),
	})
}

// ListAllDeployments returns all deployments across services
func (h *Handler) ListAllDeployments(c *gin.Context) {
	ctx := c.Request.Context()

	var since *time.Time
	if sinceStr := c.Query("since"); sinceStr != "" {
		// Parse duration like "24h", "7d", "1h"
		if d, err := time.ParseDuration(sinceStr); err == nil {
			t := time.Now().Add(-d)
			since = &t
		}
	}

	limit := 50
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	deployments, err := h.repos.Deployments.ListAllEnriched(ctx, since, limit)
	if err != nil {
		h.logger.Error(ctx, "Failed to list all deployments", logging.Error("db_error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve deployments"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"deployments": deployments,
		"count":       len(deployments),
	})
}

// sendComplianceWebhooks sends deployment evidence to Vanta/Drata
func (h *Handler) sendComplianceWebhooks(
	ctx context.Context,
	deployment *types.Deployment,
	release *types.Release,
	service *types.Service,
	environmentName string,
	approvalResult *provenance.ApprovalResult,
	receiptJSON string,
) {
	// Get project information
	project, err := h.repos.Projects.GetByID(ctx, service.ProjectID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get project for compliance webhook", logging.Error("db_error", err))
		return
	}

	// Get user context (who is deploying)
	userEmail := "system@enclii.dev" // Default
	userName := "System"
	if userCtx := ctx.Value("user"); userCtx != nil {
		if user, ok := userCtx.(*types.User); ok {
			userEmail = user.Email
			userName = user.Name
		}
	}

	// Construct deployment evidence
	evidence := &compliance.DeploymentEvidence{
		EventType:   "deployment",
		EventID:     deployment.ID.String(),
		Timestamp:   time.Now().UTC(),
		ServiceName: service.Name,
		Environment: environmentName,
		ProjectName: project.Name,

		DeploymentID:   deployment.ID.String(),
		ReleaseVersion: release.Version,
		ImageURI:       release.ImageURI,

		GitSHA:  release.GitSHA,
		GitRepo: service.GitRepo,

		// Provenance from PR approval
		PRURL:      approvalResult.PRURL,
		PRNumber:   approvalResult.PRNumber,
		ApprovedBy: approvalResult.ApproverEmail,
		ApprovedAt: approvalResult.ApprovedAt,
		CIStatus:   approvalResult.CIStatus,

		// Deployer information
		DeployedBy:      userName,
		DeployedByEmail: userEmail,
		DeployedAt:      time.Now().UTC(),

		// Supply chain security
		SBOM:              release.SBOM,
		SBOMFormat:        release.SBOMFormat,
		ImageSignature:    release.ImageSignature,
		SignatureVerified: release.SignatureVerifiedAt != nil,

		// Compliance receipt
		ComplianceReceipt: receiptJSON,
	}

	h.logger.Info(ctx, "Sending compliance evidence webhooks",
		logging.String("deployment_id", deployment.ID.String()),
		logging.String("service", service.Name),
		logging.String("environment", environmentName))

	// Send to configured webhooks
	results := h.complianceExporter.ExportDeployment(
		ctx,
		evidence,
		h.config.VantaWebhookURL,
		h.config.DrataWebhookURL,
	)

	// Log results
	h.complianceExporter.LogExportResults(results)
}
