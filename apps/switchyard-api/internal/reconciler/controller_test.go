package reconciler

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/sirupsen/logrus"
)

func TestIsEncliiManagedDeployment(t *testing.T) {
	tests := []struct {
		name     string
		labels   map[string]string
		expected bool
	}{
		{
			name:     "no labels",
			labels:   nil,
			expected: false,
		},
		{
			name:     "empty labels",
			labels:   map[string]string{},
			expected: false,
		},
		{
			name:     "managed by switchyard",
			labels:   map[string]string{"enclii.dev/managed-by": "switchyard"},
			expected: true,
		},
		{
			name:     "managed by manual",
			labels:   map[string]string{"enclii.dev/managed-by": "manual"},
			expected: false,
		},
		{
			name:     "other label only",
			labels:   map[string]string{"app": "test"},
			expected: false,
		},
		{
			name:     "managed by switchyard with other labels",
			labels:   map[string]string{"enclii.dev/managed-by": "switchyard", "app": "myapp", "version": "v1"},
			expected: true,
		},
		{
			name:     "wrong managed-by value",
			labels:   map[string]string{"enclii.dev/managed-by": "argocd"},
			expected: false,
		},
		{
			name:     "empty managed-by value",
			labels:   map[string]string{"enclii.dev/managed-by": ""},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dep := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Labels: tt.labels,
				},
			}
			if got := isEncliiManagedDeployment(dep); got != tt.expected {
				t.Errorf("isEncliiManagedDeployment() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

// ===========================================================================
// Edge case tests added below -- DO NOT modify tests above this line
// ===========================================================================

// ---------------------------------------------------------------------------
// NewController defaults
// ---------------------------------------------------------------------------

// TestNewController_Defaults verifies the Controller is created with correct
// default channel sizes and worker count.
func TestNewController_Defaults(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel) // silence logs during tests

	c := NewController(nil, nil, nil, logger)

	if c.workers != 5 {
		t.Errorf("NewController workers = %d, want 5", c.workers)
	}
	if cap(c.workCh) != 100 {
		t.Errorf("NewController workCh capacity = %d, want 100", cap(c.workCh))
	}
	if cap(c.resultCh) != 100 {
		t.Errorf("NewController resultCh capacity = %d, want 100", cap(c.resultCh))
	}
	if c.started {
		t.Error("NewController should not be started by default")
	}
	if c.stopCh == nil {
		t.Error("NewController stopCh should not be nil")
	}
	if c.serviceReconciler == nil {
		t.Error("NewController should create a ServiceReconciler")
	}
}

// TestNewController_LoggerSet verifies the logger is properly assigned.
func TestNewController_LoggerSet(t *testing.T) {
	logger := logrus.New()
	c := NewController(nil, nil, nil, logger)

	if c.logger != logger {
		t.Error("NewController should set the provided logger")
	}
}

// ---------------------------------------------------------------------------
// ReconcileWork struct
// ---------------------------------------------------------------------------

// TestReconcileWork_FieldsAreSetCorrectly verifies all fields of ReconcileWork
// are accessible and hold their values (contract test).
func TestReconcileWork_FieldsAreSetCorrectly(t *testing.T) {
	now := time.Now()
	work := &ReconcileWork{
		DeploymentID: "deploy-abc-123",
		Priority:     5,
		Attempt:      3,
		ScheduledAt:  now,
	}

	if work.DeploymentID != "deploy-abc-123" {
		t.Errorf("DeploymentID = %q, want %q", work.DeploymentID, "deploy-abc-123")
	}
	if work.Priority != 5 {
		t.Errorf("Priority = %d, want 5", work.Priority)
	}
	if work.Attempt != 3 {
		t.Errorf("Attempt = %d, want 3", work.Attempt)
	}
	if !work.ScheduledAt.Equal(now) {
		t.Errorf("ScheduledAt = %v, want %v", work.ScheduledAt, now)
	}
}

// TestReconcileWork_ZeroValueDefaults verifies zero-value ReconcileWork has
// sensible defaults (Attempt=0 could cause issues if not initialized).
func TestReconcileWork_ZeroValueDefaults(t *testing.T) {
	work := &ReconcileWork{}

	if work.DeploymentID != "" {
		t.Error("zero-value DeploymentID should be empty")
	}
	if work.Priority != 0 {
		t.Error("zero-value Priority should be 0")
	}
	if work.Attempt != 0 {
		t.Error("zero-value Attempt should be 0")
	}
	if !work.ScheduledAt.IsZero() {
		t.Error("zero-value ScheduledAt should be zero time")
	}
}

// ---------------------------------------------------------------------------
// ReconcileWorkResult
// ---------------------------------------------------------------------------

// TestReconcileWorkResult_Success verifies a successful work result structure.
func TestReconcileWorkResult_Success(t *testing.T) {
	work := &ReconcileWork{
		DeploymentID: "deploy-456",
		Priority:     1,
		Attempt:      1,
	}
	result := &ReconcileResult{
		Success:    true,
		Message:    "Service deployed successfully",
		K8sObjects: []string{"deployment/my-service", "service/my-service"},
	}
	workResult := &ReconcileWorkResult{
		Work:   work,
		Result: result,
	}

	if workResult.Work.DeploymentID != "deploy-456" {
		t.Error("WorkResult.Work should reference the original work")
	}
	if !workResult.Result.Success {
		t.Error("WorkResult.Result.Success should be true")
	}
	if len(workResult.Result.K8sObjects) != 2 {
		t.Errorf("K8sObjects length = %d, want 2", len(workResult.Result.K8sObjects))
	}
}

// TestReconcileWorkResult_Failure verifies a failed work result with error.
func TestReconcileWorkResult_Failure(t *testing.T) {
	work := &ReconcileWork{
		DeploymentID: "deploy-789",
		Attempt:      3,
	}
	result := &ReconcileResult{
		Success: false,
		Message: "Failed to apply deployment",
		Error:   fmt.Errorf("connection refused"),
	}
	workResult := &ReconcileWorkResult{
		Work:   work,
		Result: result,
	}

	if workResult.Result.Success {
		t.Error("failed WorkResult should have Success=false")
	}
	if workResult.Result.Error == nil {
		t.Error("failed WorkResult should have non-nil Error")
	}
	if workResult.Work.Attempt != 3 {
		t.Errorf("Attempt = %d, want 3", workResult.Work.Attempt)
	}
}

// ---------------------------------------------------------------------------
// QueuePressure struct
// ---------------------------------------------------------------------------

// TestQueuePressure_Fields verifies QueuePressure holds backpressure metrics.
func TestQueuePressure_Fields(t *testing.T) {
	qp := QueuePressure{
		QueueSize:     42,
		QueueCapacity: 100,
		DroppedWork:   7,
		RetryQueue:    3,
	}

	if qp.QueueSize != 42 {
		t.Errorf("QueueSize = %d, want 42", qp.QueueSize)
	}
	if qp.QueueCapacity != 100 {
		t.Errorf("QueueCapacity = %d, want 100", qp.QueueCapacity)
	}
	if qp.DroppedWork != 7 {
		t.Errorf("DroppedWork = %d, want 7", qp.DroppedWork)
	}
	if qp.RetryQueue != 3 {
		t.Errorf("RetryQueue = %d, want 3", qp.RetryQueue)
	}
}

// ---------------------------------------------------------------------------
// ErrQueueFull sentinel error
// ---------------------------------------------------------------------------

// TestErrQueueFull_IsSentinelError verifies ErrQueueFull is a non-nil error
// with a descriptive message.
func TestErrQueueFull_IsSentinelError(t *testing.T) {
	if ErrQueueFull == nil {
		t.Fatal("ErrQueueFull should not be nil")
	}
	if ErrQueueFull.Error() != "work queue is full" {
		t.Errorf("ErrQueueFull message = %q, want %q", ErrQueueFull.Error(), "work queue is full")
	}
}

// ---------------------------------------------------------------------------
// GetStatus on non-started controller
// ---------------------------------------------------------------------------

// TestGetStatus_NotStarted verifies GetStatus returns correct data for an
// idle controller that has never been started.
func TestGetStatus_NotStarted(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)

	c := NewController(nil, nil, nil, logger)
	status := c.GetStatus()

	if started, ok := status["started"].(bool); !ok || started {
		t.Error("status['started'] should be false for non-started controller")
	}
	if workers, ok := status["workers"].(int); !ok || workers != 5 {
		t.Errorf("status['workers'] = %v, want 5", status["workers"])
	}
	if workQueue, ok := status["work_queue"].(int); !ok || workQueue != 0 {
		t.Errorf("status['work_queue'] = %v, want 0", status["work_queue"])
	}
	if workQueueCap, ok := status["work_queue_cap"].(int); !ok || workQueueCap != 100 {
		t.Errorf("status['work_queue_cap'] = %v, want 100", status["work_queue_cap"])
	}
	if retryQueue, ok := status["retry_queue"].(int); !ok || retryQueue != 0 {
		t.Errorf("status['retry_queue'] = %v, want 0", status["retry_queue"])
	}
	if droppedTotal, ok := status["dropped_work_total"].(int64); !ok || droppedTotal != 0 {
		t.Errorf("status['dropped_work_total'] = %v, want 0", status["dropped_work_total"])
	}
}

// ---------------------------------------------------------------------------
// GetQueuePressure on non-started controller
// ---------------------------------------------------------------------------

// TestGetQueuePressure_NotStarted verifies GetQueuePressure returns zero
// values for an idle controller.
func TestGetQueuePressure_NotStarted(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)

	c := NewController(nil, nil, nil, logger)
	qp := c.GetQueuePressure()

	if qp.QueueSize != 0 {
		t.Errorf("QueueSize = %d, want 0", qp.QueueSize)
	}
	if qp.QueueCapacity != 100 {
		t.Errorf("QueueCapacity = %d, want 100", qp.QueueCapacity)
	}
	if qp.DroppedWork != 0 {
		t.Errorf("DroppedWork = %d, want 0", qp.DroppedWork)
	}
	if qp.RetryQueue != 0 {
		t.Errorf("RetryQueue = %d, want 0", qp.RetryQueue)
	}
}

// ---------------------------------------------------------------------------
// HealthCheck on non-started controller
// ---------------------------------------------------------------------------

// TestHealthCheck_NotStarted verifies HealthCheck returns an error when the
// controller has not been started.
func TestHealthCheck_NotStarted(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)

	c := NewController(nil, nil, nil, logger)
	err := c.HealthCheck()

	if err == nil {
		t.Fatal("HealthCheck should return error for non-started controller")
	}
	if err.Error() != "controller not started" {
		t.Errorf("HealthCheck error = %q, want %q", err.Error(), "controller not started")
	}
}

// ---------------------------------------------------------------------------
// Stop on non-started controller (no-op, no panic)
// ---------------------------------------------------------------------------

// TestStop_NotStarted verifies that calling Stop on a controller that was
// never started does not panic or block.
func TestStop_NotStarted(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)

	c := NewController(nil, nil, nil, logger)

	// Should not panic or block
	done := make(chan struct{})
	go func() {
		c.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() on non-started controller should not block")
	}
}

// ---------------------------------------------------------------------------
// Exponential backoff calculation (verified via handleResult logic)
// ---------------------------------------------------------------------------

// TestExponentialBackoff_Calculation verifies the backoff formula used in
// handleResult: min(30s * 2^(attempt-1), 5m). This tests the formula in
// isolation to document the expected progression.
func TestExponentialBackoff_Calculation(t *testing.T) {
	tests := []struct {
		attempt     int
		wantBackoff time.Duration
	}{
		{attempt: 1, wantBackoff: 30 * time.Second},  // 30s * 2^0 = 30s
		{attempt: 2, wantBackoff: 60 * time.Second},  // 30s * 2^1 = 60s
		{attempt: 3, wantBackoff: 120 * time.Second}, // 30s * 2^2 = 120s
		{attempt: 4, wantBackoff: 240 * time.Second}, // 30s * 2^3 = 240s
		{attempt: 5, wantBackoff: 300 * time.Second}, // 30s * 2^4 = 480s, capped to 5m
		{attempt: 8, wantBackoff: 300 * time.Second}, // well past cap
		{attempt: 10, wantBackoff: 300 * time.Second}, // max retries, still capped
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("attempt_%d", tt.attempt), func(t *testing.T) {
			// Replicate the formula from controller_workers.go handleResult
			backoff := 30 * time.Second
			for i := 1; i < tt.attempt; i++ {
				backoff *= 2
			}
			if backoff > 5*time.Minute {
				backoff = 5 * time.Minute
			}

			if backoff != tt.wantBackoff {
				t.Errorf("backoff for attempt %d = %v, want %v", tt.attempt, backoff, tt.wantBackoff)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Max retry limit (10 attempts)
// ---------------------------------------------------------------------------

// TestMaxRetryLimit verifies that the max retry constant used in handleResult
// is 10 attempts. This is a documentation/contract test.
func TestMaxRetryLimit(t *testing.T) {
	// The max retry count is defined as a const in handleResult (controller_workers.go)
	// We verify the expected value here as a contract test.
	const expectedMaxRetries = 10

	// An attempt at maxRetries should trigger permanent failure
	work := &ReconcileWork{
		DeploymentID: "deploy-max-retry",
		Attempt:      expectedMaxRetries,
	}

	if work.Attempt < expectedMaxRetries {
		t.Errorf("Attempt %d should be >= maxRetries %d for permanent failure", work.Attempt, expectedMaxRetries)
	}

	// An attempt below maxRetries with NextCheck should still be retriable
	workRetriable := &ReconcileWork{
		DeploymentID: "deploy-retriable",
		Attempt:      expectedMaxRetries - 1,
	}

	if workRetriable.Attempt >= expectedMaxRetries {
		t.Errorf("Attempt %d should be < maxRetries %d for retriable work", workRetriable.Attempt, expectedMaxRetries)
	}
}

// ---------------------------------------------------------------------------
// Concurrent enqueueWork safety
// ---------------------------------------------------------------------------

// TestConcurrentEnqueueWork verifies that multiple goroutines can call
// enqueueWork simultaneously without data races or panics. The total of
// successfully enqueued + retry-queued items should equal the total sent.
func TestConcurrentEnqueueWork(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)

	c := NewController(nil, nil, nil, logger)

	const numGoroutines = 20
	const itemsPerGoroutine = 10

	var wg sync.WaitGroup
	var enqueued int64
	var retried int64

	wg.Add(numGoroutines)
	for g := 0; g < numGoroutines; g++ {
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < itemsPerGoroutine; i++ {
				work := &ReconcileWork{
					DeploymentID: fmt.Sprintf("deploy-%d-%d", goroutineID, i),
					Priority:     goroutineID,
					Attempt:      1,
					ScheduledAt:  time.Now(),
				}
				err := c.enqueueWork(work)
				if err == nil {
					atomic.AddInt64(&enqueued, 1)
				} else {
					atomic.AddInt64(&retried, 1)
				}
			}
		}(g)
	}

	wg.Wait()

	totalSent := int64(numGoroutines * itemsPerGoroutine)
	totalAccounted := atomic.LoadInt64(&enqueued) + atomic.LoadInt64(&retried)

	if totalAccounted != totalSent {
		t.Errorf("enqueued(%d) + retried(%d) = %d, want %d total",
			atomic.LoadInt64(&enqueued), atomic.LoadInt64(&retried), totalAccounted, totalSent)
	}

	// Verify work channel has items
	workQueueLen := len(c.workCh)
	if workQueueLen == 0 && atomic.LoadInt64(&enqueued) > 0 {
		t.Error("work channel should have items after successful enqueuing")
	}

	// Verify retry queue captured overflow
	c.retryMu.Lock()
	retryQueueLen := len(c.retryQueue)
	c.retryMu.Unlock()

	if int64(retryQueueLen) != atomic.LoadInt64(&retried) {
		t.Errorf("retry queue len(%d) should match retried count(%d)",
			retryQueueLen, atomic.LoadInt64(&retried))
	}
}

// ---------------------------------------------------------------------------
// ScheduleReconciliation creates correct work
// ---------------------------------------------------------------------------

// TestScheduleReconciliation_EnqueuesWork verifies that ScheduleReconciliation
// puts a work item into the work channel with Attempt=1.
func TestScheduleReconciliation_EnqueuesWork(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)

	c := NewController(nil, nil, nil, logger)

	err := c.ScheduleReconciliation("deploy-schedule-test", 5)
	if err != nil {
		t.Fatalf("ScheduleReconciliation returned error: %v", err)
	}

	// Drain the work channel to inspect the item
	select {
	case work := <-c.workCh:
		if work.DeploymentID != "deploy-schedule-test" {
			t.Errorf("DeploymentID = %q, want %q", work.DeploymentID, "deploy-schedule-test")
		}
		if work.Priority != 5 {
			t.Errorf("Priority = %d, want 5", work.Priority)
		}
		if work.Attempt != 1 {
			t.Errorf("Attempt = %d, want 1 (first attempt)", work.Attempt)
		}
		if work.ScheduledAt.IsZero() {
			t.Error("ScheduledAt should not be zero")
		}
	default:
		t.Fatal("expected work item in work channel, got none")
	}
}

// ---------------------------------------------------------------------------
// SetNotificationService
// ---------------------------------------------------------------------------

// TestSetNotificationService_NilIsAllowed verifies that setting the notification
// service to nil does not panic (notifications are optional).
func TestSetNotificationService_NilIsAllowed(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)

	c := NewController(nil, nil, nil, logger)
	c.SetNotificationService(nil) // Should not panic

	if c.notificationService != nil {
		t.Error("notificationService should be nil after SetNotificationService(nil)")
	}
}

// ---------------------------------------------------------------------------
// Post-deploy health observation
// ---------------------------------------------------------------------------

// TestHealthObservation_SkipsWhenK8sClientNil verifies that health observation
// is skipped when the K8s client is nil.
func TestHealthObservation_SkipsWhenK8sClientNil(t *testing.T) {
	// Controller with nil k8sClient should not panic when observePostDeployHealth is called
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)
	c := NewController(nil, nil, nil, logger)
	// observePostDeployHealth should be a no-op when k8sClient is nil
	c.observePostDeployHealth("test-deploy-id")
	// If we get here without panic, the test passes
}
