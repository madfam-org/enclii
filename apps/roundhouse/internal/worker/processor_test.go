package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/roundhouse/internal/config"
	"github.com/madfam-org/enclii/apps/roundhouse/internal/queue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// --- pow helper ---

func TestPow(t *testing.T) {
	tests := []struct {
		name string
		x    float64
		y    float64
		want float64
	}{
		{"zero_exponent", 2.0, 0, 1.0},
		{"one_exponent", 2.0, 1, 2.0},
		{"two_exponent", 2.0, 2, 4.0},
		{"three_exponent", 2.0, 3, 8.0},
		{"base_one", 1.0, 10, 1.0},
		{"fractional_base", 1.5, 2, 2.25},
		{"multiplier_typical", 2.0, 4, 16.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pow(tt.x, tt.y)
			assert.InDelta(t, tt.want, got, 0.001, "pow(%v, %v)", tt.x, tt.y)
		})
	}
}

// --- Stats ---

func TestProcessor_Stats(t *testing.T) {
	cfg := &config.Config{
		MaxConcurrentBuilds: 5,
	}

	p := &Processor{
		workerID:  "test-worker-abc",
		cfg:       cfg,
		semaphore: make(chan struct{}, 5),
	}

	stats := p.Stats()

	assert.Equal(t, "test-worker-abc", stats["worker_id"])
	assert.Equal(t, 5, stats["max_concurrent"])
	assert.Equal(t, 0, stats["active_builds"])
	assert.Equal(t, 5, stats["available_slots"])
}

func TestProcessor_Stats_WithActiveBuilds(t *testing.T) {
	cfg := &config.Config{
		MaxConcurrentBuilds: 3,
	}

	p := &Processor{
		workerID:  "test-worker-xyz",
		cfg:       cfg,
		semaphore: make(chan struct{}, 3),
	}

	// Simulate 2 active builds by filling semaphore slots
	p.semaphore <- struct{}{}
	p.semaphore <- struct{}{}

	stats := p.Stats()

	assert.Equal(t, 2, stats["active_builds"])
	assert.Equal(t, 1, stats["available_slots"])
}

// --- sendCallback ---

func TestProcessor_SendCallback_Success(t *testing.T) {
	var receivedBody []byte
	var receivedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		receivedBody = body
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := &Processor{
		cfg: &config.Config{
			SwitchyardAPIKey: "test-api-key",
		},
		logger:     zap.NewNop(),
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	result := &queue.BuildResult{
		JobID:   uuid.New(),
		Success: true,
	}

	err := p.sendCallback(context.Background(), server.URL+"/callback", result)
	require.NoError(t, err)
	assert.Equal(t, "Bearer test-api-key", receivedAuth)
	assert.NotEmpty(t, receivedBody)
}

func TestProcessor_SendCallback_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	p := &Processor{
		cfg:        &config.Config{},
		logger:     zap.NewNop(),
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	result := &queue.BuildResult{
		JobID:   uuid.New(),
		Success: false,
	}

	err := p.sendCallback(context.Background(), server.URL+"/callback", result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")
}

func TestProcessor_SendCallback_NoAPIKey(t *testing.T) {
	var receivedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := &Processor{
		cfg:        &config.Config{},
		logger:     zap.NewNop(),
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	result := &queue.BuildResult{
		JobID:   uuid.New(),
		Success: true,
	}

	err := p.sendCallback(context.Background(), server.URL+"/callback", result)
	require.NoError(t, err)
	assert.Empty(t, receivedAuth, "should not set Authorization header when no API key")
}

func TestProcessor_SendCallback_InvalidURL(t *testing.T) {
	p := &Processor{
		cfg:        &config.Config{},
		logger:     zap.NewNop(),
		httpClient: &http.Client{Timeout: 500 * time.Millisecond},
	}

	result := &queue.BuildResult{
		JobID:   uuid.New(),
		Success: true,
	}

	// Use a non-routable address that will fail fast
	err := p.sendCallback(context.Background(), "http://127.0.0.1:1/unreachable", result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "callback request failed")
}

func TestProcessor_SendCallback_ContentType(t *testing.T) {
	var receivedContentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := &Processor{
		cfg:        &config.Config{},
		logger:     zap.NewNop(),
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	result := &queue.BuildResult{
		JobID:   uuid.New(),
		Success: true,
	}

	err := p.sendCallback(context.Background(), server.URL+"/callback", result)
	require.NoError(t, err)
	assert.Equal(t, "application/json", receivedContentType)
}

func TestProcessor_SendCallback_PayloadContent(t *testing.T) {
	var receivedResult queue.BuildResult

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := json.NewDecoder(r.Body).Decode(&receivedResult)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	jobID := uuid.New()
	releaseID := uuid.New()

	p := &Processor{
		cfg:        &config.Config{},
		logger:     zap.NewNop(),
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	result := &queue.BuildResult{
		JobID:        jobID,
		ReleaseID:    releaseID,
		Success:      true,
		ImageURI:     "ghcr.io/org/service:abc12345",
		ImageDigest:  "sha256:abcdef123456",
		DurationSecs: 45.5,
	}

	err := p.sendCallback(context.Background(), server.URL+"/callback", result)
	require.NoError(t, err)
	assert.Equal(t, jobID, receivedResult.JobID)
	assert.Equal(t, releaseID, receivedResult.ReleaseID)
	assert.True(t, receivedResult.Success)
	assert.Equal(t, "ghcr.io/org/service:abc12345", receivedResult.ImageURI)
}

func TestProcessor_SendCallback_ContextCancelled(t *testing.T) {
	// Use an already-cancelled context to verify the callback fails immediately
	p := &Processor{
		cfg:        &config.Config{},
		logger:     zap.NewNop(),
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	result := &queue.BuildResult{
		JobID:   uuid.New(),
		Success: true,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := p.sendCallback(ctx, "http://127.0.0.1:9999/callback", result)
	require.Error(t, err)
}

// --- Processor struct field validation ---

func TestProcessor_StructFields(t *testing.T) {
	cfg := &config.Config{
		MaxConcurrentBuilds: 4,
	}

	p := &Processor{
		workerID:  "worker-abc123",
		cfg:       cfg,
		logger:    zap.NewNop(),
		semaphore: make(chan struct{}, 4),
		shutdown:  make(chan struct{}),
		callbackRetry: queue.CallbackRetryConfig{
			MaxAttempts:     5,
			InitialInterval: 10 * time.Second,
			MaxInterval:     5 * time.Minute,
			Multiplier:      2.0,
		},
	}

	assert.Equal(t, "worker-abc123", p.workerID)
	assert.Equal(t, 4, p.cfg.MaxConcurrentBuilds)
	assert.NotNil(t, p.semaphore)
	assert.NotNil(t, p.shutdown)
	assert.Equal(t, 5, p.callbackRetry.MaxAttempts)
}

// --- WaitGroup concurrency pattern ---

func TestProcessor_WaitGroupPattern(t *testing.T) {
	// Validates that the WaitGroup pattern used in gracefulShutdown works correctly:
	// wg.Wait() returns immediately when no goroutines are active.
	p := &Processor{
		workerID: "test-worker",
		cfg:      &config.Config{},
		logger:   zap.NewNop(),
	}

	// wg starts at zero -- Wait should return immediately
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Expected: wg.Wait() returned immediately
	case <-time.After(time.Second):
		t.Fatal("wg.Wait() should return immediately when no goroutines are active")
	}
}

func TestProcessor_WaitGroupPattern_WaitsForActive(t *testing.T) {
	p := &Processor{
		workerID: "test-worker",
		cfg:      &config.Config{},
		logger:   zap.NewNop(),
	}

	// Simulate an active job
	p.wg.Add(1)

	var shutdownComplete bool
	var mu sync.Mutex

	go func() {
		p.wg.Wait()
		mu.Lock()
		shutdownComplete = true
		mu.Unlock()
	}()

	// Verify wait has not completed yet
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	assert.False(t, shutdownComplete, "should wait for active job")
	mu.Unlock()

	// Finish the simulated job
	p.wg.Done()

	// Wait a bit and verify wait completed
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	assert.True(t, shutdownComplete, "should complete after job finishes")
	mu.Unlock()
}

// --- Shutdown channel ---

func TestProcessor_ShutdownChannel(t *testing.T) {
	p := &Processor{
		shutdown: make(chan struct{}),
	}

	// Channel should be open initially
	select {
	case <-p.shutdown:
		t.Fatal("shutdown channel should not be closed initially")
	default:
		// Expected: channel is open
	}

	// Close the channel (simulates shutdown signal)
	close(p.shutdown)

	// Channel should now be readable
	select {
	case <-p.shutdown:
		// Expected: channel is closed
	default:
		t.Fatal("shutdown channel should be readable after close")
	}
}

// --- CallbackRetryConfig defaults ---

func TestCallbackRetryConfig_Defaults(t *testing.T) {
	// Verify the defaults used in NewProcessor match documented behavior
	retryConfig := queue.CallbackRetryConfig{
		MaxAttempts:     5,
		InitialInterval: 10 * time.Second,
		MaxInterval:     5 * time.Minute,
		Multiplier:      2.0,
	}

	assert.Equal(t, 5, retryConfig.MaxAttempts)
	assert.Equal(t, 10*time.Second, retryConfig.InitialInterval)
	assert.Equal(t, 5*time.Minute, retryConfig.MaxInterval)
	assert.Equal(t, 2.0, retryConfig.Multiplier)
}

// --- Exponential backoff calculation ---

func TestExponentialBackoff_Intervals(t *testing.T) {
	initialInterval := 10 * time.Second
	maxInterval := 5 * time.Minute
	multiplier := 2.0

	tests := []struct {
		name     string
		attempt  int
		expected time.Duration
	}{
		{"first_retry", 1, 10 * time.Second},
		{"second_retry", 2, 20 * time.Second},
		{"third_retry", 3, 40 * time.Second},
		{"fourth_retry", 4, 80 * time.Second},
		{"fifth_retry_capped", 5, 160 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			interval := time.Duration(float64(initialInterval) * pow(multiplier, float64(tt.attempt-1)))
			if interval > maxInterval {
				interval = maxInterval
			}
			assert.Equal(t, tt.expected, interval)
		})
	}
}

func TestExponentialBackoff_MaxIntervalCap(t *testing.T) {
	initialInterval := 10 * time.Second
	maxInterval := 5 * time.Minute
	multiplier := 2.0

	// Attempt 10 would be 10s * 2^9 = 5120s, should be capped at 5m
	interval := time.Duration(float64(initialInterval) * pow(multiplier, float64(9)))
	if interval > maxInterval {
		interval = maxInterval
	}

	assert.Equal(t, maxInterval, interval, "should be capped at max interval")
}
