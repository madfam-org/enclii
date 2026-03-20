package rotation

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/lockbox"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// newTestLogger creates a logrus.Logger that writes to a buffer for assertion.
// The returned buffer is NOT safe for concurrent reads while goroutines are
// still logging. Use newConcurrentTestLogger for tests that spawn goroutines.
func newTestLogger() (*logrus.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	l := logrus.New()
	l.SetOutput(&buf)
	l.SetLevel(logrus.DebugLevel)
	return l, &buf
}

// syncBuffer is a thread-safe wrapper around bytes.Buffer that can be used
// as an io.Writer for logrus when background goroutines write concurrently.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (sb *syncBuffer) Write(p []byte) (int, error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.Write(p)
}

func (sb *syncBuffer) String() string {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.String()
}

// newConcurrentTestLogger creates a logrus.Logger backed by a thread-safe
// buffer. Use this in tests where background goroutines may log concurrently.
func newConcurrentTestLogger() (*logrus.Logger, *syncBuffer) {
	sb := &syncBuffer{}
	l := logrus.New()
	l.SetOutput(sb)
	l.SetLevel(logrus.DebugLevel)
	return l, sb
}

// makeEvent creates a SecretChangeEvent with sensible defaults.
// Optional override functions can mutate individual fields.
func makeEvent(overrides ...func(*lockbox.SecretChangeEvent)) *lockbox.SecretChangeEvent {
	e := &lockbox.SecretChangeEvent{
		SecretPath:  "secret/data/myapp/db",
		SecretName:  "DATABASE_URL",
		Provider:    lockbox.ProviderVault,
		OldVersion:  1,
		NewVersion:  2,
		ServiceID:   uuid.New().String(),
		Environment: "production",
		DetectedAt:  time.Now().UTC(),
		Status:      lockbox.RotationPending,
		TriggeredBy: "watcher",
	}
	for _, fn := range overrides {
		fn(e)
	}
	return e
}

// ---------------------------------------------------------------------------
// NewController
// ---------------------------------------------------------------------------

func TestNewController(t *testing.T) {
	t.Run("default MaxConcurrent when zero", func(t *testing.T) {
		logger, _ := newTestLogger()
		cfg := &Config{Enabled: true, MaxConcurrent: 0, Timeout: 5 * time.Minute}

		c := NewController(nil, nil, logger, cfg)

		assert.Equal(t, 3, c.maxConcurrent, "zero MaxConcurrent should default to 3")
	})

	t.Run("default Timeout when zero", func(t *testing.T) {
		logger, _ := newTestLogger()
		cfg := &Config{Enabled: true, MaxConcurrent: 5}

		_ = NewController(nil, nil, logger, cfg)

		assert.Equal(t, 10*time.Minute, cfg.Timeout,
			"zero Timeout should default to 10 minutes")
	})

	t.Run("custom MaxConcurrent preserved", func(t *testing.T) {
		logger, _ := newTestLogger()
		cfg := &Config{Enabled: true, MaxConcurrent: 7, Timeout: 2 * time.Minute}

		c := NewController(nil, nil, logger, cfg)

		assert.Equal(t, 7, c.maxConcurrent, "non-zero MaxConcurrent should be preserved")
	})

	t.Run("custom Timeout preserved", func(t *testing.T) {
		logger, _ := newTestLogger()
		cfg := &Config{Enabled: true, MaxConcurrent: 1, Timeout: 30 * time.Second}

		_ = NewController(nil, nil, logger, cfg)

		assert.Equal(t, 30*time.Second, cfg.Timeout,
			"non-zero Timeout should be preserved")
	})

	t.Run("enabled flag propagation", func(t *testing.T) {
		logger, _ := newTestLogger()

		enabled := NewController(nil, nil, logger, &Config{Enabled: true})
		disabled := NewController(nil, nil, logger, &Config{Enabled: false})

		assert.True(t, enabled.enabled, "Enabled=true should propagate")
		assert.False(t, disabled.enabled, "Enabled=false should propagate")
	})
}

// ---------------------------------------------------------------------------
// Controller struct initialization
// ---------------------------------------------------------------------------

func TestControllerStructInit(t *testing.T) {
	t.Run("event queue capacity is 100", func(t *testing.T) {
		logger, _ := newTestLogger()
		c := NewController(nil, nil, logger, &Config{Enabled: true})

		assert.Equal(t, 100, cap(c.eventQueue),
			"eventQueue channel capacity should be 100")
	})

	t.Run("audit queue capacity is 100", func(t *testing.T) {
		logger, _ := newTestLogger()
		c := NewController(nil, nil, logger, &Config{Enabled: true})

		assert.Equal(t, 100, cap(c.auditQueue),
			"auditQueue channel capacity should be 100")
	})

	t.Run("all fields properly set", func(t *testing.T) {
		logger, _ := newTestLogger()
		cfg := &Config{Enabled: true, MaxConcurrent: 4, Timeout: 3 * time.Minute}

		c := NewController(nil, nil, logger, cfg)

		assert.NotNil(t, c.logger, "logger should be set")
		assert.Nil(t, c.k8sClient, "k8sClient should be nil when nil is passed")
		assert.Nil(t, c.repos, "repos should be nil when nil is passed")
		assert.Equal(t, 4, c.maxConcurrent, "maxConcurrent should match config")
		assert.True(t, c.enabled, "enabled should match config")
		assert.NotNil(t, c.eventQueue, "eventQueue should be initialized")
		assert.NotNil(t, c.auditQueue, "auditQueue should be initialized")
	})
}

// ---------------------------------------------------------------------------
// Config defaults
// ---------------------------------------------------------------------------

func TestConfigDefaults(t *testing.T) {
	t.Run("zero Config gets all defaults applied", func(t *testing.T) {
		logger, _ := newTestLogger()
		cfg := &Config{}

		c := NewController(nil, nil, logger, cfg)

		assert.Equal(t, 3, c.maxConcurrent,
			"zero MaxConcurrent should become 3")
		assert.Equal(t, 10*time.Minute, cfg.Timeout,
			"zero Timeout should become 10m")
		assert.False(t, c.enabled,
			"zero-value Enabled should remain false")
	})

	t.Run("partial Config only fills missing fields", func(t *testing.T) {
		logger, _ := newTestLogger()
		cfg := &Config{MaxConcurrent: 5}

		c := NewController(nil, nil, logger, cfg)

		assert.Equal(t, 5, c.maxConcurrent,
			"provided MaxConcurrent should be kept")
		assert.Equal(t, 10*time.Minute, cfg.Timeout,
			"missing Timeout should get default")
	})

	t.Run("full Config nothing overridden", func(t *testing.T) {
		logger, _ := newTestLogger()
		cfg := &Config{
			MaxConcurrent: 8,
			Timeout:       45 * time.Second,
			Enabled:       true,
		}

		c := NewController(nil, nil, logger, cfg)

		assert.Equal(t, 8, c.maxConcurrent)
		assert.Equal(t, 45*time.Second, cfg.Timeout)
		assert.True(t, c.enabled)
	})
}

// ---------------------------------------------------------------------------
// IsEnabled
// ---------------------------------------------------------------------------

func TestIsEnabled(t *testing.T) {
	t.Run("enabled controller returns true", func(t *testing.T) {
		logger, _ := newTestLogger()
		c := NewController(nil, nil, logger, &Config{Enabled: true})

		assert.True(t, c.IsEnabled())
	})

	t.Run("disabled controller returns false", func(t *testing.T) {
		logger, _ := newTestLogger()
		c := NewController(nil, nil, logger, &Config{Enabled: false})

		assert.False(t, c.IsEnabled())
	})
}

// ---------------------------------------------------------------------------
// EnqueueRotation
// ---------------------------------------------------------------------------

func TestEnqueueRotation(t *testing.T) {
	t.Run("disabled controller returns error", func(t *testing.T) {
		logger, _ := newTestLogger()
		c := NewController(nil, nil, logger, &Config{Enabled: false})

		err := c.EnqueueRotation(makeEvent())

		require.Error(t, err)
		assert.Contains(t, err.Error(), "secret rotation is disabled")
	})

	t.Run("nil UUID event gets new UUID assigned", func(t *testing.T) {
		logger, _ := newTestLogger()
		c := NewController(nil, nil, logger, &Config{Enabled: true})

		event := makeEvent(func(e *lockbox.SecretChangeEvent) {
			e.ID = uuid.Nil
		})

		err := c.EnqueueRotation(event)

		require.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, event.ID,
			"nil UUID should be replaced with a generated one")
	})

	t.Run("pre-set UUID is preserved", func(t *testing.T) {
		logger, _ := newTestLogger()
		c := NewController(nil, nil, logger, &Config{Enabled: true})

		fixedID := uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
		event := makeEvent(func(e *lockbox.SecretChangeEvent) {
			e.ID = fixedID
		})

		err := c.EnqueueRotation(event)

		require.NoError(t, err)
		assert.Equal(t, fixedID, event.ID,
			"pre-set UUID should not be overwritten")
	})

	t.Run("normal enqueue succeeds", func(t *testing.T) {
		logger, _ := newTestLogger()
		c := NewController(nil, nil, logger, &Config{Enabled: true})

		err := c.EnqueueRotation(makeEvent())

		require.NoError(t, err)
		assert.Equal(t, 1, len(c.eventQueue),
			"event queue should contain the enqueued event")
	})

	t.Run("queue full returns error", func(t *testing.T) {
		logger, _ := newTestLogger()
		c := NewController(nil, nil, logger, &Config{Enabled: true})

		// Fill the queue to capacity (100).
		for i := 0; i < 100; i++ {
			err := c.EnqueueRotation(makeEvent())
			require.NoError(t, err, "enqueue %d should succeed", i)
		}

		// The 101st enqueue must fail.
		err := c.EnqueueRotation(makeEvent())

		require.Error(t, err)
		assert.Contains(t, err.Error(), "queue is full")
	})

	t.Run("valid event fields preserved after enqueue", func(t *testing.T) {
		logger, _ := newTestLogger()
		c := NewController(nil, nil, logger, &Config{Enabled: true})

		serviceID := uuid.New().String()
		event := makeEvent(func(e *lockbox.SecretChangeEvent) {
			e.ID = uuid.New()
			e.SecretName = "API_KEY"
			e.SecretPath = "secret/data/app/api-key"
			e.OldVersion = 5
			e.NewVersion = 6
			e.ServiceID = serviceID
			e.Environment = "staging"
			e.TriggeredBy = "webhook"
		})
		originalID := event.ID

		err := c.EnqueueRotation(event)
		require.NoError(t, err)

		// Drain the event from the queue and verify all fields.
		received := <-c.eventQueue

		assert.Equal(t, originalID, received.ID)
		assert.Equal(t, "API_KEY", received.SecretName)
		assert.Equal(t, "secret/data/app/api-key", received.SecretPath)
		assert.Equal(t, 5, received.OldVersion)
		assert.Equal(t, 6, received.NewVersion)
		assert.Equal(t, serviceID, received.ServiceID)
		assert.Equal(t, "staging", received.Environment)
		assert.Equal(t, "webhook", received.TriggeredBy)
	})

	t.Run("enqueue logs the rotation details", func(t *testing.T) {
		logger, buf := newTestLogger()
		c := NewController(nil, nil, logger, &Config{Enabled: true})

		event := makeEvent(func(e *lockbox.SecretChangeEvent) {
			e.SecretName = "MY_SECRET"
			e.OldVersion = 3
			e.NewVersion = 4
		})

		err := c.EnqueueRotation(event)
		require.NoError(t, err)

		logOutput := buf.String()
		assert.Contains(t, logOutput, "MY_SECRET",
			"log should mention the secret name")
		assert.Contains(t, logOutput, "Enqueued secret rotation",
			"log should contain the enqueue message")
	})
}

// ---------------------------------------------------------------------------
// Start with disabled controller
// ---------------------------------------------------------------------------

func TestStart_Disabled(t *testing.T) {
	t.Run("returns nil immediately when disabled", func(t *testing.T) {
		logger, _ := newTestLogger()
		c := NewController(nil, nil, logger, &Config{Enabled: false})

		// Start should return nil without blocking since the controller is disabled.
		err := c.Start(context.Background())

		assert.NoError(t, err, "disabled controller Start should return nil")
	})

	t.Run("logs disabled message", func(t *testing.T) {
		logger, buf := newTestLogger()
		c := NewController(nil, nil, logger, &Config{Enabled: false})

		_ = c.Start(context.Background())

		logOutput := buf.String()
		assert.Contains(t, logOutput, "disabled",
			"should log that the controller is disabled")
	})
}

// ---------------------------------------------------------------------------
// Start with enabled controller (context cancellation)
// Uses newConcurrentTestLogger to avoid data races on the log buffer.
// ---------------------------------------------------------------------------

func TestStart_Enabled_ContextCancellation(t *testing.T) {
	t.Run("blocks until context is cancelled then returns nil", func(t *testing.T) {
		logger, buf := newConcurrentTestLogger()
		c := NewController(nil, nil, logger, &Config{Enabled: true, MaxConcurrent: 1})

		ctx, cancel := context.WithCancel(context.Background())

		done := make(chan error, 1)
		go func() {
			done <- c.Start(ctx)
		}()

		// Give workers a moment to start.
		time.Sleep(50 * time.Millisecond)

		cancel()

		select {
		case err := <-done:
			assert.NoError(t, err, "Start should return nil after context cancellation")
		case <-time.After(2 * time.Second):
			t.Fatal("Start did not return within 2 seconds after context cancellation")
		}

		// Allow background goroutines to finish logging.
		time.Sleep(50 * time.Millisecond)

		logOutput := buf.String()
		assert.Contains(t, logOutput, "Starting secret rotation controller",
			"should log startup message")
	})

	t.Run("logs shutdown message after cancellation", func(t *testing.T) {
		logger, buf := newConcurrentTestLogger()
		c := NewController(nil, nil, logger, &Config{Enabled: true, MaxConcurrent: 1})

		ctx, cancel := context.WithCancel(context.Background())

		done := make(chan error, 1)
		go func() {
			done <- c.Start(ctx)
		}()

		time.Sleep(50 * time.Millisecond)
		cancel()

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("Start did not return within 2 seconds after context cancellation")
		}

		// Allow background goroutines to finish logging.
		time.Sleep(50 * time.Millisecond)

		logOutput := buf.String()
		assert.Contains(t, logOutput, "shutting down",
			"should log shutdown message after context cancellation")
	})
}

// ---------------------------------------------------------------------------
// rotationLogData struct (unexported type access from same package)
// ---------------------------------------------------------------------------

func TestRotationLogData(t *testing.T) {
	t.Run("fields can be populated", func(t *testing.T) {
		now := time.Now().UTC()
		completedAt := now.Add(5 * time.Second)
		durationMs := int64(5000)

		data := rotationLogData{
			ID:              uuid.New(),
			EventID:         uuid.New(),
			ServiceID:       uuid.New(),
			ServiceName:     "my-service",
			Environment:     "production",
			SecretName:      "DB_PASSWORD",
			SecretPath:      "secret/data/db",
			OldVersion:      1,
			NewVersion:      2,
			Status:          "completed",
			StartedAt:       now,
			CompletedAt:     &completedAt,
			DurationMs:      &durationMs,
			RolloutStrategy: "rolling",
			PodsRestarted:   3,
			Error:           "",
			ChangedBy:       "vault-agent",
			TriggeredBy:     "watcher",
		}

		assert.Equal(t, "my-service", data.ServiceName)
		assert.Equal(t, "production", data.Environment)
		assert.Equal(t, "DB_PASSWORD", data.SecretName)
		assert.Equal(t, 1, data.OldVersion)
		assert.Equal(t, 2, data.NewVersion)
		assert.Equal(t, "completed", data.Status)
		assert.Equal(t, "rolling", data.RolloutStrategy)
		assert.Equal(t, 3, data.PodsRestarted)
		assert.Equal(t, &completedAt, data.CompletedAt)
		assert.Equal(t, &durationMs, data.DurationMs)
		assert.Equal(t, "vault-agent", data.ChangedBy)
		assert.Equal(t, "watcher", data.TriggeredBy)
	})

	t.Run("nil optional fields", func(t *testing.T) {
		data := rotationLogData{
			ID:          uuid.New(),
			EventID:     uuid.New(),
			ServiceID:   uuid.New(),
			Status:      "in_progress",
			StartedAt:   time.Now().UTC(),
			CompletedAt: nil,
			DurationMs:  nil,
		}

		assert.Nil(t, data.CompletedAt, "CompletedAt should be nil before completion")
		assert.Nil(t, data.DurationMs, "DurationMs should be nil before completion")
	})

	t.Run("error field holds failure message", func(t *testing.T) {
		data := rotationLogData{
			ID:     uuid.New(),
			Status: "failed",
			Error:  "connection refused to vault at 127.0.0.1:8200",
		}

		assert.Equal(t, "failed", data.Status)
		assert.Contains(t, data.Error, "connection refused")
	})
}

// ---------------------------------------------------------------------------
// EnqueueRotation concurrency safety
// ---------------------------------------------------------------------------

func TestEnqueueRotation_ConcurrentAccess(t *testing.T) {
	t.Run("concurrent enqueues do not panic or lose events", func(t *testing.T) {
		logger, _ := newConcurrentTestLogger()
		c := NewController(nil, nil, logger, &Config{Enabled: true})

		const goroutines = 50
		errs := make(chan error, goroutines)

		for i := 0; i < goroutines; i++ {
			go func() {
				errs <- c.EnqueueRotation(makeEvent())
			}()
		}

		successCount := 0
		for i := 0; i < goroutines; i++ {
			if err := <-errs; err == nil {
				successCount++
			}
		}

		assert.Equal(t, goroutines, successCount,
			"all 50 concurrent enqueues should succeed (queue capacity is 100)")
		assert.Equal(t, goroutines, len(c.eventQueue),
			"event queue should contain all enqueued events")
	})

	t.Run("concurrent enqueues beyond capacity return queue full errors", func(t *testing.T) {
		logger, _ := newConcurrentTestLogger()
		c := NewController(nil, nil, logger, &Config{Enabled: true})

		// Pre-fill queue to capacity.
		for i := 0; i < 100; i++ {
			require.NoError(t, c.EnqueueRotation(makeEvent()))
		}

		const extra = 20
		errs := make(chan error, extra)

		for i := 0; i < extra; i++ {
			go func() {
				errs <- c.EnqueueRotation(makeEvent())
			}()
		}

		failCount := 0
		for i := 0; i < extra; i++ {
			if err := <-errs; err != nil {
				failCount++
				assert.Contains(t, err.Error(), "queue is full")
			}
		}

		assert.Equal(t, extra, failCount,
			"all enqueues beyond capacity should fail")
	})
}

// ---------------------------------------------------------------------------
// Config type zero-value and full-value
// ---------------------------------------------------------------------------

func TestConfigType(t *testing.T) {
	t.Run("zero value Config", func(t *testing.T) {
		cfg := Config{}

		assert.Equal(t, 0, cfg.MaxConcurrent)
		assert.Equal(t, time.Duration(0), cfg.Timeout)
		assert.False(t, cfg.Enabled)
	})

	t.Run("fully populated Config", func(t *testing.T) {
		cfg := Config{
			MaxConcurrent: 10,
			Timeout:       5 * time.Minute,
			Enabled:       true,
		}

		assert.Equal(t, 10, cfg.MaxConcurrent)
		assert.Equal(t, 5*time.Minute, cfg.Timeout)
		assert.True(t, cfg.Enabled)
	})
}

// ---------------------------------------------------------------------------
// Multiple sequential enqueue and drain
// ---------------------------------------------------------------------------

func TestEnqueueRotation_MultipleEventsOrdering(t *testing.T) {
	t.Run("events are dequeued in FIFO order", func(t *testing.T) {
		logger, _ := newTestLogger()
		c := NewController(nil, nil, logger, &Config{Enabled: true})

		ids := make([]uuid.UUID, 5)
		for i := range ids {
			ids[i] = uuid.New()
			event := makeEvent(func(e *lockbox.SecretChangeEvent) {
				e.ID = ids[i]
			})
			require.NoError(t, c.EnqueueRotation(event))
		}

		for i := range ids {
			received := <-c.eventQueue
			assert.Equal(t, ids[i], received.ID,
				"event %d should be dequeued in FIFO order", i)
		}
	})
}

// ---------------------------------------------------------------------------
// EnqueueRotation with various TriggeredBy values
// ---------------------------------------------------------------------------

func TestEnqueueRotation_TriggeredByVariants(t *testing.T) {
	tests := []struct {
		name        string
		triggeredBy string
	}{
		{"watcher trigger", "watcher"},
		{"webhook trigger", "webhook"},
		{"manual trigger", "manual"},
		{"empty trigger", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, _ := newTestLogger()
			c := NewController(nil, nil, logger, &Config{Enabled: true})

			event := makeEvent(func(e *lockbox.SecretChangeEvent) {
				e.TriggeredBy = tt.triggeredBy
			})

			err := c.EnqueueRotation(event)
			require.NoError(t, err)

			received := <-c.eventQueue
			assert.Equal(t, tt.triggeredBy, received.TriggeredBy)
		})
	}
}

// ---------------------------------------------------------------------------
// NewController MaxConcurrent boundary values
// ---------------------------------------------------------------------------

func TestNewController_MaxConcurrentBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected int
	}{
		{"zero defaults to 3", 0, 3},
		{"one is preserved", 1, 1},
		{"large value preserved", 100, 100},
		{"negative value preserved", -1, -1}, // controller does not guard negatives
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, _ := newTestLogger()
			cfg := &Config{MaxConcurrent: tt.input, Enabled: true}

			c := NewController(nil, nil, logger, cfg)

			assert.Equal(t, tt.expected, c.maxConcurrent)
		})
	}
}

// ---------------------------------------------------------------------------
// NewController Timeout boundary values
// ---------------------------------------------------------------------------

func TestNewController_TimeoutBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Duration
		expected time.Duration
	}{
		{"zero defaults to 10m", 0, 10 * time.Minute},
		{"1 second preserved", 1 * time.Second, 1 * time.Second},
		{"30 minutes preserved", 30 * time.Minute, 30 * time.Minute},
		{"negative value preserved", -1 * time.Second, -1 * time.Second}, // no guard
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, _ := newTestLogger()
			cfg := &Config{Timeout: tt.input, Enabled: true}

			_ = NewController(nil, nil, logger, cfg)

			assert.Equal(t, tt.expected, cfg.Timeout)
		})
	}
}

// ---------------------------------------------------------------------------
// EnqueueRotation provider variants
// ---------------------------------------------------------------------------

func TestEnqueueRotation_ProviderVariants(t *testing.T) {
	tests := []struct {
		name     string
		provider lockbox.SecretProvider
	}{
		{"vault provider", lockbox.ProviderVault},
		{"1password provider", lockbox.ProviderOnePassword},
		{"kubernetes provider", lockbox.ProviderKubernetes},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, _ := newTestLogger()
			c := NewController(nil, nil, logger, &Config{Enabled: true})

			event := makeEvent(func(e *lockbox.SecretChangeEvent) {
				e.Provider = tt.provider
			})

			err := c.EnqueueRotation(event)
			require.NoError(t, err)

			received := <-c.eventQueue
			assert.Equal(t, tt.provider, received.Provider)
		})
	}
}

// ---------------------------------------------------------------------------
// EnqueueRotation version edge cases
// ---------------------------------------------------------------------------

func TestEnqueueRotation_VersionEdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		oldVersion int
		newVersion int
	}{
		{"version 0 to 1", 0, 1},
		{"same version", 5, 5},
		{"large version numbers", 999999, 1000000},
		{"version downgrade", 10, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, _ := newTestLogger()
			c := NewController(nil, nil, logger, &Config{Enabled: true})

			event := makeEvent(func(e *lockbox.SecretChangeEvent) {
				e.OldVersion = tt.oldVersion
				e.NewVersion = tt.newVersion
			})

			err := c.EnqueueRotation(event)
			require.NoError(t, err)

			received := <-c.eventQueue
			assert.Equal(t, tt.oldVersion, received.OldVersion)
			assert.Equal(t, tt.newVersion, received.NewVersion)
		})
	}
}

// ---------------------------------------------------------------------------
// EnqueueRotation with disabled controller does not touch event
// ---------------------------------------------------------------------------

func TestEnqueueRotation_DisabledDoesNotMutateEvent(t *testing.T) {
	t.Run("event ID remains nil when disabled controller rejects it", func(t *testing.T) {
		logger, _ := newTestLogger()
		c := NewController(nil, nil, logger, &Config{Enabled: false})

		event := makeEvent(func(e *lockbox.SecretChangeEvent) {
			e.ID = uuid.Nil
		})

		err := c.EnqueueRotation(event)

		require.Error(t, err)
		// The disabled check happens before the UUID generation, so the
		// event ID should still be nil.
		assert.Equal(t, uuid.Nil, event.ID,
			"disabled controller should not mutate the event")
	})
}

// ---------------------------------------------------------------------------
// EnqueueRotation queue length tracking
// ---------------------------------------------------------------------------

func TestEnqueueRotation_QueueLengthTracking(t *testing.T) {
	t.Run("queue length increments with each enqueue", func(t *testing.T) {
		logger, _ := newTestLogger()
		c := NewController(nil, nil, logger, &Config{Enabled: true})

		for i := 1; i <= 10; i++ {
			require.NoError(t, c.EnqueueRotation(makeEvent()))
			assert.Equal(t, i, len(c.eventQueue),
				"queue length should be %d after %d enqueues", i, i)
		}
	})

	t.Run("queue length decrements when events are drained", func(t *testing.T) {
		logger, _ := newTestLogger()
		c := NewController(nil, nil, logger, &Config{Enabled: true})

		for i := 0; i < 5; i++ {
			require.NoError(t, c.EnqueueRotation(makeEvent()))
		}
		assert.Equal(t, 5, len(c.eventQueue))

		// Drain 3 events.
		for i := 0; i < 3; i++ {
			<-c.eventQueue
		}
		assert.Equal(t, 2, len(c.eventQueue),
			"queue length should be 2 after draining 3 of 5 events")
	})
}
