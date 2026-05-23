package reconciler

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/monitoring"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/notifications"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// tracer — reconciler package emits spans under this name so Tempo can
// filter by span_name=reconciler.* to isolate reconcile-loop activity.
var tracer = otel.Tracer("switchyard-api/reconciler")

// Controller manages the reconciliation loop for all deployments
type Controller struct {
	db                *sql.DB
	repositories      *db.Repositories
	serviceReconciler *ServiceReconciler
	k8sClient         *k8s.Client
	logger            *logrus.Logger

	// Notification service for webhooks (optional)
	notificationService *notifications.Service

	// Control channels
	stopCh   chan struct{}
	workCh   chan *ReconcileWork
	resultCh chan *ReconcileWorkResult

	// Worker management
	workers int
	wg      sync.WaitGroup
	started bool
	mu      sync.RWMutex

	// Backpressure tracking
	droppedWork int64            // Atomic counter for dropped work items
	retryQueue  []*ReconcileWork // Items that need to be retried when queue has space
	retryMu     sync.Mutex       // Protects retryQueue
}

// ReconcileWork represents a unit of reconciliation work
type ReconcileWork struct {
	DeploymentID string
	Priority     int
	Attempt      int
	ScheduledAt  time.Time
}

// ReconcileWorkResult represents the result of reconciliation work
type ReconcileWorkResult struct {
	Work   *ReconcileWork
	Result *ReconcileResult
}

// QueuePressure tracks work queue metrics
type QueuePressure struct {
	QueueSize     int
	QueueCapacity int
	DroppedWork   int64
	RetryQueue    int
}

// ErrQueueFull is returned when the work queue cannot accept more work
var ErrQueueFull = fmt.Errorf("work queue is full")

// NewController creates a new reconciliation controller
func NewController(database *sql.DB, repositories *db.Repositories, k8sClient *k8s.Client, logger *logrus.Logger) *Controller {
	return &Controller{
		db:                database,
		repositories:      repositories,
		serviceReconciler: NewServiceReconciler(k8sClient, logger),
		k8sClient:         k8sClient,
		logger:            logger,
		stopCh:            make(chan struct{}),
		workCh:            make(chan *ReconcileWork, DefaultWorkQueueSize),
		resultCh:          make(chan *ReconcileWorkResult, DefaultWorkQueueSize),
		workers:           DefaultWorkerCount,
	}
}

// SetNotificationService sets the notification service for sending webhook events
func (c *Controller) SetNotificationService(svc *notifications.Service) {
	c.notificationService = svc
}

// Start begins the reconciliation controller
func (c *Controller) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return fmt.Errorf("controller already started")
	}

	c.started = true
	c.logger.Info("Starting reconciliation controller")

	// Start worker goroutines
	for i := 0; i < c.workers; i++ {
		c.wg.Add(1)
		go c.worker(ctx, i)
	}

	// Start result processor
	c.wg.Add(1)
	go c.resultProcessor(ctx)

	// Start work scheduler
	c.wg.Add(1)
	go c.workScheduler(ctx)

	// Start K8s→DB sync job (runs every 60 seconds)
	c.wg.Add(1)
	go c.k8sSyncScheduler(ctx)

	// Start retry queue processor (drains retry queue when work queue has space)
	c.wg.Add(1)
	go c.retryQueueProcessor(ctx)

	// Start canary scheduler (P2.7) — advances active canary rollouts every
	// CanaryTickInterval (15s). No-op if CanaryRollouts repo is nil.
	c.wg.Add(1)
	go c.canaryScheduler(ctx)

	c.logger.WithField("workers", c.workers).Info("Reconciliation controller started")
	return nil
}

// Stop gracefully shuts down the controller
func (c *Controller) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return
	}

	c.logger.Info("Stopping reconciliation controller")
	close(c.stopCh)
	c.wg.Wait()
	c.started = false
	c.logger.Info("Reconciliation controller stopped")
}

// ScheduleReconciliation adds a deployment to the reconciliation queue.
// Returns ErrQueueFull if the queue cannot accept the work.
func (c *Controller) ScheduleReconciliation(deploymentID string, priority int) error {
	work := &ReconcileWork{
		DeploymentID: deploymentID,
		Priority:     priority,
		Attempt:      1,
		ScheduledAt:  time.Now(),
	}

	return c.enqueueWork(work)
}

// enqueueWork attempts to add work to the queue, with retry queue fallback
func (c *Controller) enqueueWork(work *ReconcileWork) error {
	select {
	case c.workCh <- work:
		c.logger.WithFields(logrus.Fields{
			"deployment": work.DeploymentID,
			"priority":   work.Priority,
			"attempt":    work.Attempt,
		}).Debug("Scheduled reconciliation work")
		c.recordQueueMetrics()
		monitoring.RecordReconcilerWorkScheduled()
		return nil
	default:
		// Queue is full - add to retry queue with backpressure tracking
		atomic.AddInt64(&c.droppedWork, 1)

		c.retryMu.Lock()
		c.retryQueue = append(c.retryQueue, work)
		retryLen := len(c.retryQueue)
		c.retryMu.Unlock()

		c.logger.WithFields(logrus.Fields{
			"deployment":       work.DeploymentID,
			"retry_queue_size": retryLen,
			"dropped_total":    atomic.LoadInt64(&c.droppedWork),
		}).Warn("Work queue full, added to retry queue")

		c.recordQueueMetrics()
		return ErrQueueFull
	}
}

func (c *Controller) recordQueueMetrics() {
	qp := c.GetQueuePressure()
	monitoring.RecordReconcilerQueuePressure(
		qp.QueueSize,
		qp.QueueCapacity,
		qp.RetryQueue,
		qp.DroppedWork,
	)
}

// worker processes reconciliation work
func (c *Controller) worker(ctx context.Context, workerID int) {
	defer c.wg.Done()

	logger := c.logger.WithField("worker", workerID)
	logger.Debug("Starting reconciliation worker")

	for {
		select {
		case <-c.stopCh:
			logger.Debug("Worker stopping")
			return
		case <-ctx.Done():
			logger.Debug("Worker context cancelled")
			return
		case work := <-c.workCh:
			result := c.processWork(ctx, work, logger)

			select {
			case c.resultCh <- &ReconcileWorkResult{Work: work, Result: result}:
			case <-c.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}
}

// processWork handles a single reconciliation task.
//
// A span covers the whole reconcile tick so slow deployments show up as
// wide spans in Tempo with their child k8s.*, db.*, and image-pull
// sub-operations nested underneath.
func (c *Controller) processWork(ctx context.Context, work *ReconcileWork, logger *logrus.Entry) *ReconcileResult {
	ctx, span := tracer.Start(ctx, "reconciler.processWork") // SetAttributes is called after span start so values that
	// include anything resembling a credential would still be filtered
	// by the secret-attribute processor wired in packages/otel-go.

	defer span.End()
	span.SetAttributes(
		attribute.String("reconciler.deployment_id", work.DeploymentID),
		attribute.Int("reconciler.attempt", work.Attempt),
	)

	logger = logger.WithFields(logrus.Fields{
		"deployment": work.DeploymentID,
		"attempt":    work.Attempt,
	})

	logger.Info("Processing reconciliation work")
	start := time.Now()

	// Get deployment details from database
	deployment, err := c.repositories.Deployments.GetByID(ctx, work.DeploymentID)
	if err != nil {
		logger.WithError(err).Error("Failed to get deployment")
		return &ReconcileResult{
			Success: false,
			Message: "Failed to retrieve deployment",
			Error:   err,
		}
	}

	// Get associated release first
	release, err := c.repositories.Releases.GetByID(deployment.ReleaseID)
	if err != nil {
		logger.WithError(err).Error("Failed to get release")
		return &ReconcileResult{
			Success: false,
			Message: "Failed to retrieve release",
			Error:   err,
		}
	}

	// Get service from release
	service, err := c.repositories.Services.GetByID(release.ServiceID)
	if err != nil {
		logger.WithError(err).Error("Failed to get service")
		return &ReconcileResult{
			Success: false,
			Message: "Failed to retrieve service",
			Error:   err,
		}
	}

	// Get environment to determine the target Kubernetes namespace
	environment, err := c.repositories.Environments.GetByID(ctx, deployment.EnvironmentID)
	if err != nil {
		logger.WithError(err).Error("Failed to get environment")
		return &ReconcileResult{
			Success: false,
			Message: "Failed to retrieve environment",
			Error:   err,
		}
	}

	// CRITICAL: Check if K8s deployment has reconciliation disabled BEFORE reconciling
	// This prevents the reconciler from overwriting manually-managed deployments like Janua
	// The annotation check in syncDeploymentToDatabase only applies during K8s→DB sync,
	// this check applies during actual reconciliation of existing DB records
	if c.k8sClient != nil {
		existing, err := c.k8sClient.Clientset.AppsV1().Deployments(environment.KubeNamespace).Get(
			ctx, service.Name, metav1.GetOptions{},
		)
		if err == nil {
			if val, ok := existing.Annotations["enclii.dev/reconcile"]; ok && val == "disabled" {
				logger.WithFields(logrus.Fields{
					"deployment": service.Name,
					"namespace":  environment.KubeNamespace,
				}).Info("Skipping reconciliation - disabled via annotation")
				return &ReconcileResult{
					Success: true,
					Message: "Reconciliation disabled via annotation",
				}
			}
		}
		// If the deployment doesn't exist yet, or we can't fetch it, proceed with reconciliation
	}

	// Get environment variables (decrypted) for this service and environment
	// Use GetDecryptedWithMeta to properly separate secrets from regular env vars
	var envVars map[string]string
	var envVarsWithMeta []EnvVarWithMeta
	if c.repositories.EnvVars != nil {
		// Try the new metadata-aware method first
		dbEnvVars, err := c.repositories.EnvVars.GetDecryptedWithMeta(ctx, service.ID, deployment.EnvironmentID)
		if err != nil {
			logger.WithError(err).Warn("Failed to get environment variables with metadata, falling back to legacy")
			var legacyErr error
			envVars, legacyErr = c.repositories.EnvVars.GetDecrypted(ctx, service.ID, deployment.EnvironmentID)
			if legacyErr != nil {
				logger.WithError(legacyErr).Warn("Failed to get environment variables via legacy method, continuing without env vars")
				envVars = make(map[string]string)
			}
		} else {
			// Convert db.EnvVarWithMeta to reconciler.EnvVarWithMeta
			envVarsWithMeta = make([]EnvVarWithMeta, len(dbEnvVars))
			envVars = make(map[string]string) // Also populate legacy map for backwards compatibility
			for i, ev := range dbEnvVars {
				envVarsWithMeta[i] = EnvVarWithMeta{
					Key:      ev.Key,
					Value:    ev.Value,
					IsSecret: ev.IsSecret,
				}
				envVars[ev.Key] = ev.Value
			}
		}
	} else {
		envVars = make(map[string]string)
	}

	// Get database addon bindings for this service
	var addonBindings []AddonBinding
	if c.repositories.DatabaseAddons != nil {
		bindings, err := c.repositories.DatabaseAddons.GetBindingsByService(ctx, service.ID)
		if err != nil {
			logger.WithError(err).Warn("Failed to get addon bindings, continuing without them")
		} else {
			for _, binding := range bindings {
				// Get the addon details
				addon, err := c.repositories.DatabaseAddons.GetByID(ctx, binding.AddonID)
				if err != nil {
					logger.WithError(err).WithField("addon_id", binding.AddonID).Warn("Failed to get addon for binding")
					continue
				}
				// Only include ready addons
				if addon.Status != types.DatabaseAddonStatusReady {
					logger.WithFields(logrus.Fields{
						"addon_id": addon.ID,
						"status":   addon.Status,
					}).Debug("Skipping non-ready addon binding")
					continue
				}
				addonBindings = append(addonBindings, AddonBinding{
					EnvVarName:       binding.EnvVarName,
					AddonType:        addon.Type,
					K8sNamespace:     addon.K8sNamespace,
					K8sResourceName:  addon.K8sResourceName,
					ConnectionSecret: addon.ConnectionSecret,
				})
			}
		}
	}

	// Create reconcile request
	req := &ReconcileRequest{
		Service:         service,
		Release:         release,
		Deployment:      deployment,
		Environment:     environment,
		EnvVars:         envVars,
		EnvVarsWithMeta: envVarsWithMeta,
		AddonBindings:   addonBindings,
	}

	// Perform reconciliation
	result := c.serviceReconciler.Reconcile(ctx, req)

	duration := time.Since(start)
	logger.WithFields(logrus.Fields{
		"success":  result.Success,
		"duration": duration,
		"message":  result.Message,
	}).Info("Completed reconciliation work")

	// Record outcome on the span so Tempo exemplars on the reconciler
	// latency panel are routed correctly (success vs. error breakdown).
	span.SetAttributes(attribute.Bool("reconciler.success", result.Success))
	if !result.Success {
		span.SetStatus(codes.Error, result.Message)
		if result.Error != nil {
			span.RecordError(result.Error)
		}
	}

	monitoring.RecordReconcilerProcess(duration, !result.Success)
	return result
}

// GetStatus returns the current status of the controller
func (c *Controller) GetStatus() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	c.retryMu.Lock()
	retryQueueLen := len(c.retryQueue)
	c.retryMu.Unlock()

	return map[string]interface{}{
		"started":            c.started,
		"workers":            c.workers,
		"work_queue":         len(c.workCh),
		"work_queue_cap":     cap(c.workCh),
		"result_queue":       len(c.resultCh),
		"result_queue_cap":   cap(c.resultCh),
		"retry_queue":        retryQueueLen,
		"dropped_work_total": atomic.LoadInt64(&c.droppedWork),
	}
}

// GetQueuePressure returns detailed backpressure metrics
func (c *Controller) GetQueuePressure() QueuePressure {
	c.retryMu.Lock()
	retryQueueLen := len(c.retryQueue)
	c.retryMu.Unlock()

	return QueuePressure{
		QueueSize:     len(c.workCh),
		QueueCapacity: cap(c.workCh),
		DroppedWork:   atomic.LoadInt64(&c.droppedWork),
		RetryQueue:    retryQueueLen,
	}
}

// HealthCheck returns an error if the controller is unhealthy
func (c *Controller) HealthCheck() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.started {
		return fmt.Errorf("controller not started")
	}

	// Check if work channels are functional
	if len(c.workCh) == cap(c.workCh) {
		return fmt.Errorf("work queue is full")
	}

	if len(c.resultCh) == cap(c.resultCh) {
		return fmt.Errorf("result queue is full")
	}

	return nil
}

// GetPodEnvVars retrieves environment variables from a running pod (delegates to ServiceReconciler)
func (c *Controller) GetPodEnvVars(ctx context.Context, namespace, podName string) (map[string]string, error) {
	return c.serviceReconciler.GetPodEnvVars(ctx, namespace, podName)
}
