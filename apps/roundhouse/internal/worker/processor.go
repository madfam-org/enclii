package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/roundhouse/internal/builder"
	"github.com/madfam-org/enclii/apps/roundhouse/internal/config"
	"github.com/madfam-org/enclii/apps/roundhouse/internal/queue"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Processor handles build job processing
type Processor struct {
	workerID   string
	queue      *queue.RedisQueue
	builder    builder.Builder // Use interface for both Docker and Kaniko
	cfg        *config.Config
	logger     *zap.Logger
	httpClient *http.Client

	// Concurrency control
	semaphore chan struct{}
	wg        sync.WaitGroup
	shutdown  chan struct{}

	// Callback retry configuration
	callbackRetry queue.CallbackRetryConfig
}

// NewProcessor creates a new job processor
func NewProcessor(cfg *config.Config, q *queue.RedisQueue, logger *zap.Logger) (*Processor, error) {
	workerID := cfg.WorkerID
	if workerID == "" {
		hostname, _ := os.Hostname()
		workerID = fmt.Sprintf("%s-%s", hostname, uuid.New().String()[:8])
	}

	// Log callback for streaming build logs to Redis
	logFunc := func(jobID uuid.UUID, line string) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = q.AppendLog(ctx, jobID, line) // best-effort log streaming
	}

	// Initialize the appropriate builder based on build mode
	var b builder.Builder
	var err error

	switch builder.BuildMode(cfg.BuildMode) {
	case builder.BuildModeKaniko:
		logger.Info("initializing Kaniko builder (secure, rootless)",
			zap.String("registry", cfg.Registry))

		// Create Kubernetes client
		k8sClient, err := createK8sClient(cfg.KubeConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
		}

		b = builder.NewKanikoExecutor(&builder.KanikoExecutorConfig{
			K8sClient:      k8sClient,
			Registry:       cfg.Registry,
			RegistryUser:   cfg.RegistryUser,
			RegistryPass:   cfg.RegistryPassword,
			GenerateSBOM:   cfg.GenerateSBOM,
			SignImages:     cfg.SignImages,
			CosignKey:      cfg.CosignKey,
			Timeout:        cfg.BuildTimeout,
			CacheRepo:      cfg.KanikoCacheRepo,
			GitCredentials: cfg.KanikoGitCredentials,
		}, logger, logFunc)

	case builder.BuildModeDocker:
		logger.Warn("initializing Docker builder (requires Docker socket - NOT recommended for production)",
			zap.String("registry", cfg.Registry))

		b = builder.NewExecutor(&builder.ExecutorConfig{
			WorkDir:      cfg.BuildWorkDir,
			Registry:     cfg.Registry,
			RegistryUser: cfg.RegistryUser,
			RegistryPass: cfg.RegistryPassword,
			GenerateSBOM: cfg.GenerateSBOM,
			SignImages:   cfg.SignImages,
			CosignKey:    cfg.CosignKey,
			Timeout:      cfg.BuildTimeout,
		}, logger, logFunc)

	default:
		return nil, fmt.Errorf("unsupported build mode: %s (use 'docker' or 'kaniko')", cfg.BuildMode)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to initialize builder: %w", err)
	}

	p := &Processor{
		workerID:   workerID,
		queue:      q,
		builder:    b,
		cfg:        cfg,
		logger:     logger,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		semaphore:  make(chan struct{}, cfg.MaxConcurrentBuilds),
		shutdown:   make(chan struct{}),
		callbackRetry: queue.CallbackRetryConfig{
			MaxAttempts:     5,
			InitialInterval: 10 * time.Second,
			MaxInterval:     5 * time.Minute,
			Multiplier:      2.0,
		},
	}

	return p, nil
}

// createK8sClient creates a Kubernetes client
func createK8sClient(kubeconfig string) (kubernetes.Interface, error) {
	var config *rest.Config
	var err error

	if kubeconfig != "" {
		// Use provided kubeconfig file
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		// Use in-cluster config (when running in Kubernetes)
		config, err = rest.InClusterConfig()
	}

	if err != nil {
		return nil, fmt.Errorf("failed to build kubernetes config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}

	return clientset, nil
}

// Start begins processing jobs
func (p *Processor) Start(ctx context.Context) error {
	p.logger.Info("worker starting",
		zap.String("worker_id", p.workerID),
		zap.Int("max_concurrent", p.cfg.MaxConcurrentBuilds),
	)

	// Register worker
	if err := p.queue.RegisterWorker(ctx, p.workerID); err != nil {
		p.logger.Warn("failed to register worker", zap.Error(err))
	}

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		p.logger.Info("shutdown signal received, waiting for builds to complete...")
		close(p.shutdown)
	}()

	// Start callback retry processor in background
	go p.processCallbackRetries(ctx)

	// Main processing loop
	for {
		select {
		case <-ctx.Done():
			return p.gracefulShutdown()
		case <-p.shutdown:
			return p.gracefulShutdown()
		default:
			// Try to acquire semaphore (non-blocking check for shutdown)
			select {
			case p.semaphore <- struct{}{}:
				// Got a slot, try to get a job
				job, err := p.queue.Dequeue(ctx, p.cfg.PollInterval)
				if err != nil {
					p.logger.Error("failed to dequeue", zap.Error(err))
					<-p.semaphore // Release slot
					time.Sleep(time.Second)
					continue
				}

				if job == nil {
					// No job available
					<-p.semaphore
					continue
				}

				// Process job in goroutine
				p.wg.Add(1)
				go func(j *queue.BuildJob) {
					defer p.wg.Done()
					defer func() { <-p.semaphore }()
					p.processJob(ctx, j)
				}(job)

			case <-p.shutdown:
				return p.gracefulShutdown()
			}
		}
	}
}

func (p *Processor) processJob(ctx context.Context, job *queue.BuildJob) {
	logger := p.logger.With(
		zap.String("job_id", job.ID.String()),
		zap.String("service_id", job.ServiceID.String()),
		zap.String("git_sha", job.GitSHA[:8]),
	)

	logger.Info("processing job")

	// Update status to building
	if err := p.queue.UpdateStatus(ctx, job.ID, queue.StatusBuilding, p.workerID); err != nil {
		logger.Error("failed to update status", zap.Error(err))
	}

	// Create build context with timeout
	buildCtx, cancel := context.WithTimeout(ctx, p.cfg.BuildTimeout)
	defer cancel()

	// Execute build using configured builder (Docker or Kaniko)
	result, err := p.builder.Execute(buildCtx, job)

	// Update final status
	var finalStatus queue.JobStatus
	if err != nil || !result.Success {
		finalStatus = queue.StatusFailed
		logger.Error("build failed",
			zap.String("error", result.ErrorMessage),
			zap.Float64("duration_secs", result.DurationSecs),
		)
	} else {
		finalStatus = queue.StatusCompleted
		logger.Info("build completed",
			zap.String("image_uri", result.ImageURI),
			zap.Float64("duration_secs", result.DurationSecs),
		)
	}

	// Store result
	if err := p.queue.SetResult(ctx, job.ID, result); err != nil {
		logger.Error("failed to store result", zap.Error(err))
	}

	// Update status
	if err := p.queue.UpdateStatus(ctx, job.ID, finalStatus, p.workerID); err != nil {
		logger.Error("failed to update final status", zap.Error(err))
	}

	// Send callback to Switchyard (with retry on failure)
	if job.CallbackURL != "" {
		if err := p.sendCallbackWithRetry(ctx, job.ID, job.CallbackURL, result); err != nil {
			logger.Error("failed to send callback, queued for retry", zap.Error(err))
		}
	}
}

func (p *Processor) sendCallback(ctx context.Context, url string, result *queue.BuildResult) error {
	payload, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if p.cfg.SwitchyardAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.cfg.SwitchyardAPIKey)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("callback request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("callback returned status %d", resp.StatusCode)
	}

	p.logger.Info("callback sent successfully",
		zap.String("url", url),
		zap.String("job_id", result.JobID.String()),
	)

	return nil
}

// sendCallbackWithRetry attempts to send a callback, queuing for retry on failure
func (p *Processor) sendCallbackWithRetry(ctx context.Context, jobID uuid.UUID, url string, result *queue.BuildResult) error {
	err := p.sendCallback(ctx, url, result)
	if err == nil {
		return nil
	}

	// Queue for retry
	callback := &queue.FailedCallback{
		JobID:       jobID,
		URL:         url,
		Result:      result,
		Attempts:    1,
		MaxAttempts: p.callbackRetry.MaxAttempts,
		NextRetry:   time.Now().Add(p.callbackRetry.InitialInterval),
		LastError:   err.Error(),
	}

	if queueErr := p.queue.EnqueueFailedCallback(ctx, callback); queueErr != nil {
		p.logger.Error("failed to queue callback for retry",
			zap.String("job_id", jobID.String()),
			zap.Error(queueErr),
		)
		return fmt.Errorf("callback failed and could not queue retry: %w (original: %v)", queueErr, err)
	}

	return err
}

// processCallbackRetries runs in background to retry failed callbacks
func (p *Processor) processCallbackRetries(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second) // Check every 5 seconds
	defer ticker.Stop()

	p.logger.Info("callback retry processor started")

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.shutdown:
			return
		case <-ticker.C:
			p.processReadyCallbacks(ctx)
		}
	}
}

// processReadyCallbacks handles callbacks that are ready to be retried
func (p *Processor) processReadyCallbacks(ctx context.Context) {
	callbacks, err := p.queue.DequeueReadyCallbacks(ctx, 10)
	if err != nil {
		p.logger.Error("failed to dequeue ready callbacks", zap.Error(err))
		return
	}

	for _, callback := range callbacks {
		p.retryCallback(ctx, callback)
	}
}

// retryCallback attempts to send a callback and handles success/failure
func (p *Processor) retryCallback(ctx context.Context, callback *queue.FailedCallback) {
	logger := p.logger.With(
		zap.String("callback_id", callback.ID.String()),
		zap.String("job_id", callback.JobID.String()),
		zap.Int("attempt", callback.Attempts+1),
		zap.Int("max_attempts", callback.MaxAttempts),
	)

	err := p.sendCallback(ctx, callback.URL, callback.Result)
	if err == nil {
		logger.Info("callback retry succeeded")
		_ = p.queue.RemoveCallback(ctx, callback.ID)
		return
	}

	callback.Attempts++
	callback.LastError = err.Error()

	if callback.Attempts >= callback.MaxAttempts {
		logger.Error("callback permanently failed after max retries",
			zap.String("last_error", callback.LastError),
		)
		_ = p.queue.RemoveCallback(ctx, callback.ID)
		return
	}

	// Calculate next retry with exponential backoff
	interval := time.Duration(float64(p.callbackRetry.InitialInterval) * pow(p.callbackRetry.Multiplier, float64(callback.Attempts-1)))
	if interval > p.callbackRetry.MaxInterval {
		interval = p.callbackRetry.MaxInterval
	}
	callback.NextRetry = time.Now().Add(interval)

	logger.Warn("callback retry failed, will retry later",
		zap.String("error", err.Error()),
		zap.Duration("next_retry_in", interval),
	)

	if updateErr := p.queue.UpdateFailedCallback(ctx, callback); updateErr != nil {
		logger.Error("failed to update callback for retry", zap.Error(updateErr))
	}
}

// pow calculates x^y for float64
func pow(x, y float64) float64 {
	result := 1.0
	for i := 0; i < int(y); i++ {
		result *= x
	}
	return result
}

func (p *Processor) gracefulShutdown() error {
	p.logger.Info("waiting for active builds to complete...")

	// Wait for all active jobs with timeout
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.logger.Info("all builds completed")
	case <-time.After(5 * time.Minute):
		p.logger.Warn("shutdown timeout, some builds may be interrupted")
	}

	// Unregister worker
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p.queue.UnregisterWorker(ctx, p.workerID); err != nil {
		p.logger.Warn("failed to unregister worker", zap.Error(err))
	}

	return nil
}

// Stats returns current worker statistics
func (p *Processor) Stats() map[string]interface{} {
	return map[string]interface{}{
		"worker_id":       p.workerID,
		"max_concurrent":  p.cfg.MaxConcurrentBuilds,
		"active_builds":   len(p.semaphore),
		"available_slots": p.cfg.MaxConcurrentBuilds - len(p.semaphore),
	}
}
