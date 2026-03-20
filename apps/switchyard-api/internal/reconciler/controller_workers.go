package reconciler

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// resultProcessor handles reconciliation results
func (c *Controller) resultProcessor(ctx context.Context) {
	defer c.wg.Done()

	logger := c.logger.WithField("component", "result-processor")
	logger.Debug("Starting result processor")

	for {
		select {
		case <-c.stopCh:
			logger.Debug("Result processor stopping")
			return
		case <-ctx.Done():
			logger.Debug("Result processor context cancelled")
			return
		case workResult := <-c.resultCh:
			c.handleResult(ctx, workResult, logger)
		}
	}
}

// handleResult processes a reconciliation result
func (c *Controller) handleResult(ctx context.Context, workResult *ReconcileWorkResult, logger *logrus.Entry) {
	work := workResult.Work
	result := workResult.Result

	logger = logger.WithFields(logrus.Fields{
		"deployment": work.DeploymentID,
		"success":    result.Success,
	})

	// Update deployment status in database
	var status types.DeploymentStatus
	var health types.HealthStatus

	if result.Success {
		status = types.DeploymentStatusRunning
		health = types.HealthStatusHealthy
		logger.Info("Deployment reconciled successfully")

		// Start post-deploy health observation (fire-and-forget with stopCh guard)
		c.observePostDeployHealth(work.DeploymentID)
	} else {
		const maxRetries = 10

		if result.NextCheck != nil && work.Attempt < maxRetries {
			// Retry with exponential backoff: min(30s * 2^retries, 5m)
			status = types.DeploymentStatusPending
			health = types.HealthStatusUnknown

			backoff := 30 * time.Second
			for i := 1; i < work.Attempt; i++ {
				backoff *= 2
			}
			if backoff > 5*time.Minute {
				backoff = 5 * time.Minute
			}
			nextCheck := time.Now().Add(backoff)

			retryWork := &ReconcileWork{
				DeploymentID: work.DeploymentID,
				Priority:     work.Priority + 1,
				Attempt:      work.Attempt + 1,
				ScheduledAt:  nextCheck,
			}

			go func() {
				time.Sleep(time.Until(nextCheck))
				select {
				case <-c.stopCh:
					return
				default:
				}
				if err := c.enqueueWork(retryWork); err != nil {
					c.logger.WithFields(logrus.Fields{
						"deployment": retryWork.DeploymentID,
						"attempt":    retryWork.Attempt,
					}).Debug("Retry scheduled to retry queue")
				}
			}()

			logger.WithFields(logrus.Fields{
				"next_check": nextCheck,
				"backoff":    backoff,
				"attempt":    work.Attempt,
			}).Info("Scheduled reconciliation retry with exponential backoff")
		} else {
			// Failed permanently (no NextCheck or max retries exceeded)
			status = types.DeploymentStatusFailed
			health = types.HealthStatusUnhealthy
			if work.Attempt >= maxRetries {
				logger.WithField("attempts", work.Attempt).Error("Deployment failed after max retries")
			} else {
				logger.WithError(result.Error).Error("Deployment reconciliation failed")
			}
		}
	}

	// Update deployment in database
	deploymentUUID, err := uuid.Parse(work.DeploymentID)
	if err != nil {
		logger.WithError(err).Error("Failed to parse deployment ID")
		return
	}

	// Store error message for failed deployments
	var errorMsg *string
	if status == types.DeploymentStatusFailed && result.Error != nil {
		errStr := result.Error.Error()
		errorMsg = &errStr
	}
	err = c.repositories.Deployments.UpdateStatusWithError(deploymentUUID, status, health, errorMsg)
	if err != nil {
		logger.WithError(err).Error("Failed to update deployment status")
	}

	// Send webhook notifications for final states (success or permanent failure)
	if c.notificationService != nil && (status == types.DeploymentStatusRunning || status == types.DeploymentStatusFailed) {
		go c.sendDeploymentNotification(ctx, deploymentUUID, status, result)
	}
}

// workScheduler periodically checks for pending deployments
func (c *Controller) workScheduler(ctx context.Context) {
	defer c.wg.Done()

	logger := c.logger.WithField("component", "work-scheduler")
	logger.Debug("Starting work scheduler")

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			logger.Debug("Work scheduler stopping")
			return
		case <-ctx.Done():
			logger.Debug("Work scheduler context cancelled")
			return
		case <-ticker.C:
			c.schedulePendingWork(ctx, logger)
		}
	}
}

// schedulePendingWork finds and schedules pending deployments
func (c *Controller) schedulePendingWork(ctx context.Context, logger *logrus.Entry) {
	// Get pending deployments
	deployments, err := c.repositories.Deployments.GetByStatus(ctx, types.DeploymentStatusPending)
	if err != nil {
		logger.WithError(err).Error("Failed to get pending deployments")
		return
	}

	for _, deployment := range deployments {
		// Calculate priority based on age
		age := time.Since(deployment.CreatedAt)
		priority := int(age.Minutes()) // Older deployments get higher priority

		work := &ReconcileWork{
			DeploymentID: deployment.ID.String(),
			Priority:     priority,
			Attempt:      1,
			ScheduledAt:  time.Now(),
		}

		if err := c.enqueueWork(work); err != nil {
			// Added to retry queue, will be processed when queue has space
			logger.WithFields(logrus.Fields{
				"deployment":       deployment.ID,
				"retry_queue_size": len(c.retryQueue),
			}).Debug("Pending deployment added to retry queue")
		} else {
			logger.WithFields(logrus.Fields{
				"deployment": deployment.ID,
				"age":        age,
				"priority":   priority,
			}).Debug("Scheduled pending deployment")
		}
	}

	if len(deployments) > 0 {
		logger.WithField("count", len(deployments)).Debug("Scheduled pending deployments")
	}
}

// retryQueueProcessor drains the retry queue when the work queue has space
func (c *Controller) retryQueueProcessor(ctx context.Context) {
	defer c.wg.Done()

	logger := c.logger.WithField("component", "retry-processor")
	logger.Debug("Starting retry queue processor")

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			logger.Debug("Retry queue processor stopping")
			return
		case <-ctx.Done():
			logger.Debug("Retry queue processor context cancelled")
			return
		case <-ticker.C:
			c.drainRetryQueue(logger)
		}
	}
}

// drainRetryQueue attempts to move items from retry queue to work queue
func (c *Controller) drainRetryQueue(logger *logrus.Entry) {
	c.retryMu.Lock()
	if len(c.retryQueue) == 0 {
		c.retryMu.Unlock()
		return
	}

	// Check if work queue has space (at least 20% free)
	workQueueFree := cap(c.workCh) - len(c.workCh)
	if workQueueFree < cap(c.workCh)/5 {
		c.retryMu.Unlock()
		return
	}

	// Move items from retry queue to work queue
	toMove := len(c.retryQueue)
	if toMove > workQueueFree {
		toMove = workQueueFree
	}

	moved := 0
	remaining := make([]*ReconcileWork, 0, len(c.retryQueue)-toMove)

	for i, work := range c.retryQueue {
		if i < toMove {
			select {
			case c.workCh <- work:
				moved++
			default:
				// Queue filled up, keep remaining items
				remaining = append(remaining, c.retryQueue[i:]...)
				break
			}
		} else {
			remaining = append(remaining, work)
		}
	}

	c.retryQueue = remaining
	c.retryMu.Unlock()

	if moved > 0 {
		logger.WithFields(logrus.Fields{
			"moved":     moved,
			"remaining": len(remaining),
		}).Debug("Drained retry queue to work queue")
	}
}

// observePostDeployHealth monitors a deployment after successful reconciliation.
// It checks deployment health every 30s for 2 minutes (4 checks). If replicas
// are unavailable for 2 consecutive checks, it triggers an automatic rollback.
// This is a fire-and-forget goroutine guarded by the controller's stopCh.
func (c *Controller) observePostDeployHealth(deploymentID string) {
	if c.k8sClient == nil {
		return
	}

	go func() {
		logger := c.logger.WithFields(logrus.Fields{
			"component":  "health-observer",
			"deployment": deploymentID,
		})

		// Resolve the deployment to get service name and namespace.
		// Use context.Background() since the caller's context may be cancelled.
		ctx := context.Background()

		if c.repositories == nil {
			logger.Debug("Repositories not available, skipping health observation")
			return
		}

		deployment, err := c.repositories.Deployments.GetByID(ctx, deploymentID)
		if err != nil {
			logger.WithError(err).Warn("Failed to get deployment for health observation")
			return
		}

		release, err := c.repositories.Releases.GetByID(deployment.ReleaseID)
		if err != nil {
			logger.WithError(err).Warn("Failed to get release for health observation")
			return
		}

		service, err := c.repositories.Services.GetByID(release.ServiceID)
		if err != nil {
			logger.WithError(err).Warn("Failed to get service for health observation")
			return
		}

		environment, err := c.repositories.Environments.GetByID(ctx, deployment.EnvironmentID)
		if err != nil {
			logger.WithError(err).Warn("Failed to get environment for health observation")
			return
		}

		serviceName := service.Name
		namespace := environment.KubeNamespace

		logger = logger.WithFields(logrus.Fields{
			"service":   serviceName,
			"namespace": namespace,
		})
		logger.Info("Starting post-deploy health observation")

		const (
			checkInterval       = 30 * time.Second
			totalChecks         = 4 // 4 checks * 30s = 2 minutes
			consecutiveFailures = 2 // trigger rollback after 2 consecutive failures
		)

		failCount := 0
		for i := 0; i < totalChecks; i++ {
			select {
			case <-c.stopCh:
				logger.Debug("Health observation cancelled by shutdown")
				return
			case <-time.After(checkInterval):
			}

			statusInfo, err := c.k8sClient.GetDeploymentStatusInfo(ctx, namespace, serviceName)
			if err != nil {
				logger.WithError(err).Warn("Failed to get deployment status during health observation")
				failCount = 0 // reset on transient errors (can't determine health)
				continue
			}

			if statusInfo.UnavailableReplicas > 0 {
				failCount++
				logger.WithFields(logrus.Fields{
					"unavailable_replicas": statusInfo.UnavailableReplicas,
					"ready_replicas":       statusInfo.ReadyReplicas,
					"consecutive_failures": failCount,
					"check":                i + 1,
				}).Warn("Deployment has unavailable replicas")

				if failCount >= consecutiveFailures {
					logger.Error("Deployment unhealthy for consecutive checks, triggering auto-rollback")

					rollbackErr := c.k8sClient.RollbackDeployment(ctx, serviceName, namespace)
					if rollbackErr != nil {
						logger.WithError(rollbackErr).Error("Auto-rollback failed")
					} else {
						logger.Info("Auto-rollback completed successfully")
					}

					// Update deployment status to Failed in database
					deploymentUUID, parseErr := uuid.Parse(deploymentID)
					if parseErr == nil {
						errMsg := fmt.Sprintf("auto-rollback triggered: %d unavailable replicas for %d consecutive health checks", statusInfo.UnavailableReplicas, failCount)
						c.repositories.Deployments.UpdateStatusWithError(deploymentUUID, types.DeploymentStatusFailed, types.HealthStatusUnhealthy, &errMsg)
					}
					return
				}
			} else {
				failCount = 0 // reset on healthy check
				logger.WithFields(logrus.Fields{
					"ready_replicas":     statusInfo.ReadyReplicas,
					"available_replicas": statusInfo.AvailableReplicas,
					"check":              i + 1,
				}).Debug("Deployment healthy during observation")
			}
		}

		logger.Info("Post-deploy health observation completed, deployment stable")
	}()
}
