package logging

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- helpers ---

// newTestLogger creates a StructuredLogger backed by a logrus test hook so log
// entries can be inspected without any stdout/stderr output. The returned hook
// is the primary inspection point for all assertions.
func newTestLogger(t *testing.T, level logrus.Level) (*StructuredLogger, *test.Hook) {
	t.Helper()
	logger, hook := test.NewNullLogger()
	logger.SetLevel(level)

	return &StructuredLogger{
		logger: logger,
		fields: logrus.Fields{
			"service":     "test-service",
			"version":     "0.0.1",
			"environment": "test",
		},
	}, hook
}

// --- NewStructuredLogger ---

func TestNewStructuredLogger_DefaultConfig(t *testing.T) {
	cfg := &LogConfig{
		Level:       "info",
		Format:      "json",
		Output:      "stdout",
		ServiceName: "unit-test",
		Version:     "1.0.0",
		Environment: "testing",
	}

	logger, err := NewStructuredLogger(cfg)
	require.NoError(t, err)
	require.NotNil(t, logger)

	sl, ok := logger.(*StructuredLogger)
	require.True(t, ok, "expected *StructuredLogger")
	assert.Equal(t, logrus.InfoLevel, sl.logger.Level)
	assert.Equal(t, "unit-test", sl.fields["service"])
	assert.Equal(t, "1.0.0", sl.fields["version"])
	assert.Equal(t, "testing", sl.fields["environment"])
}

func TestNewStructuredLogger_InvalidLevelDefaultsToInfo(t *testing.T) {
	cfg := &LogConfig{
		Level:       "not-a-level",
		Format:      "text",
		Output:      "stdout",
		ServiceName: "test",
		Version:     "0.0.1",
		Environment: "test",
	}

	logger, err := NewStructuredLogger(cfg)
	require.NoError(t, err)

	sl := logger.(*StructuredLogger)
	assert.Equal(t, logrus.InfoLevel, sl.logger.Level, "invalid level should fall back to info")
}

func TestNewStructuredLogger_FileOutputInvalidPath(t *testing.T) {
	cfg := &LogConfig{
		Level:       "info",
		Format:      "json",
		Output:      "/nonexistent/deeply/nested/path/test.log",
		ServiceName: "test",
		Version:     "0.0.1",
		Environment: "test",
	}

	logger, err := NewStructuredLogger(cfg)
	assert.Error(t, err, "should fail on invalid file path")
	assert.Nil(t, logger)
	assert.Contains(t, err.Error(), "failed to open log file")
}

// --- Log Level Filtering ---

func TestLogLevelFiltering(t *testing.T) {
	// Create a logger at Warn level -- Debug and Info should be suppressed.
	sl, hook := newTestLogger(t, logrus.WarnLevel)
	ctx := context.Background()

	sl.Debug(ctx, "debug message")
	sl.Info(ctx, "info message")
	sl.Warn(ctx, "warn message")
	sl.Error(ctx, "error message")

	// Only warn and error should be captured.
	entries := hook.AllEntries()
	require.Len(t, entries, 2)
	assert.Equal(t, logrus.WarnLevel, entries[0].Level)
	assert.Equal(t, "warn message", entries[0].Message)
	assert.Equal(t, logrus.ErrorLevel, entries[1].Level)
	assert.Equal(t, "error message", entries[1].Message)
}

// --- Structured Fields ---

func TestWithField_AddsFieldToLogEntry(t *testing.T) {
	sl, hook := newTestLogger(t, logrus.DebugLevel)
	ctx := context.Background()

	child := sl.WithField("component", "scheduler")
	child.Info(ctx, "task started")

	require.Len(t, hook.AllEntries(), 1)
	entry := hook.LastEntry()
	assert.Equal(t, "scheduler", entry.Data["component"])
	// Original default fields should still be present.
	assert.Equal(t, "test-service", entry.Data["service"])
}

func TestWithFields_MergesMultipleFields(t *testing.T) {
	sl, hook := newTestLogger(t, logrus.DebugLevel)
	ctx := context.Background()

	child := sl.WithFields(Fields{
		"project_id": "proj-123",
		"service_id": "svc-456",
	})
	child.Info(ctx, "deploying")

	require.Len(t, hook.AllEntries(), 1)
	entry := hook.LastEntry()
	assert.Equal(t, "proj-123", entry.Data["project_id"])
	assert.Equal(t, "svc-456", entry.Data["service_id"])
	assert.Equal(t, "test-service", entry.Data["service"])
}

func TestWithField_DoesNotMutateParent(t *testing.T) {
	sl, hook := newTestLogger(t, logrus.DebugLevel)
	ctx := context.Background()

	_ = sl.WithField("extra", "child-only")
	sl.Info(ctx, "parent log")

	require.Len(t, hook.AllEntries(), 1)
	entry := hook.LastEntry()
	_, hasExtra := entry.Data["extra"]
	assert.False(t, hasExtra, "parent logger must not contain child fields")
}

func TestWithError_AttachesErrorField(t *testing.T) {
	sl, hook := newTestLogger(t, logrus.DebugLevel)
	ctx := context.Background()

	child := sl.WithError(errors.New("connection refused"))
	child.Error(ctx, "database unreachable")

	require.Len(t, hook.AllEntries(), 1)
	entry := hook.LastEntry()
	assert.Equal(t, "connection refused", entry.Data["error"])
}

// --- Context Propagation ---

func TestRequestIDExtractedFromContext(t *testing.T) {
	sl, hook := newTestLogger(t, logrus.DebugLevel)

	ctx := context.WithValue(context.Background(), requestIDCtxKey, "req-abc-123")
	sl.Info(ctx, "handling request")

	require.Len(t, hook.AllEntries(), 1)
	entry := hook.LastEntry()
	assert.Equal(t, "req-abc-123", entry.Data["request_id"])
}

func TestNilContextDoesNotPanic(t *testing.T) {
	sl, hook := newTestLogger(t, logrus.DebugLevel)

	// Passing nil context must not cause a panic.
	assert.NotPanics(t, func() {
		sl.Info(nil, "nil context message") //nolint:staticcheck // intentional nil context for test
	})

	require.Len(t, hook.AllEntries(), 1)
	assert.Equal(t, "nil context message", hook.LastEntry().Message)
}

// --- Inline Fields ---

func TestInlineFieldsAppendedToEntry(t *testing.T) {
	sl, hook := newTestLogger(t, logrus.DebugLevel)
	ctx := context.Background()

	sl.Info(ctx, "build completed",
		Field{Key: "duration_ms", Value: 1234},
		Field{Key: "artifact", Value: "myapp:latest"},
	)

	require.Len(t, hook.AllEntries(), 1)
	entry := hook.LastEntry()
	assert.Equal(t, 1234, entry.Data["duration_ms"])
	assert.Equal(t, "myapp:latest", entry.Data["artifact"])
}

// --- RequestID Middleware ---

func TestRequestIDMiddleware_GeneratesIDWhenMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestIDMiddleware())
	router.GET("/test", func(c *gin.Context) {
		reqID, exists := c.Get(RequestIDKey)
		assert.True(t, exists, "request_id should be set in gin context")
		assert.NotEmpty(t, reqID)
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, w.Header().Get("X-Request-ID"), "response should contain X-Request-ID header")
}

func TestRequestIDMiddleware_PreservesExistingHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestIDMiddleware())

	existingID := "custom-id-999"

	router.GET("/test", func(c *gin.Context) {
		reqID, _ := c.Get(RequestIDKey)
		assert.Equal(t, existingID, reqID, "middleware should preserve incoming X-Request-ID")

		// Also verify it was propagated into the request context.
		ctxVal := c.Request.Context().Value(requestIDCtxKey)
		assert.Equal(t, existingID, ctxVal, "request context should carry the request ID")

		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Request-ID", existingID)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, existingID, w.Header().Get("X-Request-ID"))
}

// --- Helper Functions ---

func TestFieldHelpers(t *testing.T) {
	t.Run("String", func(t *testing.T) {
		f := String("key", "val")
		assert.Equal(t, "key", f.Key)
		assert.Equal(t, "val", f.Value)
	})

	t.Run("Int", func(t *testing.T) {
		f := Int("port", 8080)
		assert.Equal(t, "port", f.Key)
		assert.Equal(t, 8080, f.Value)
	})

	t.Run("Float64", func(t *testing.T) {
		f := Float64("ratio", 0.95)
		assert.Equal(t, "ratio", f.Key)
		assert.Equal(t, 0.95, f.Value)
	})

	t.Run("Bool", func(t *testing.T) {
		f := Bool("enabled", true)
		assert.Equal(t, "enabled", f.Key)
		assert.Equal(t, true, f.Value)
	})

	t.Run("Duration", func(t *testing.T) {
		f := Duration("latency", 150*time.Millisecond)
		assert.Equal(t, "latency", f.Key)
		assert.Equal(t, "150ms", f.Value)
	})

	t.Run("Error", func(t *testing.T) {
		f := Error("err", errors.New("timeout"))
		assert.Equal(t, "err", f.Key)
		assert.Equal(t, "timeout", f.Value)
	})
}

// --- ParseLogLevel / DefaultLogConfig ---

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected logrus.Level
	}{
		{"debug", logrus.DebugLevel},
		{"info", logrus.InfoLevel},
		{"warn", logrus.WarnLevel},
		{"warning", logrus.WarnLevel},
		{"error", logrus.ErrorLevel},
		{"fatal", logrus.FatalLevel},
		{"panic", logrus.PanicLevel},
		{"invalid", logrus.InfoLevel},
		{"", logrus.InfoLevel},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.expected, ParseLogLevel(tc.input))
		})
	}
}

func TestDefaultLogConfig(t *testing.T) {
	cfg := DefaultLogConfig()
	require.NotNil(t, cfg)
	assert.Equal(t, "info", cfg.Level)
	assert.Equal(t, "json", cfg.Format)
	assert.Equal(t, "stdout", cfg.Output)
	assert.Equal(t, "enclii-switchyard", cfg.ServiceName)
	assert.Equal(t, "0.1.0", cfg.Version)
	assert.Equal(t, "development", cfg.Environment)
	assert.True(t, cfg.TracingEnabled)
	assert.Equal(t, 0.1, cfg.TracingSampler)
}

// --- Global Logger Get/Set ---

func TestGetSetLogger(t *testing.T) {
	// Save and restore the package-level defaultLogger to avoid cross-test side effects.
	prev := defaultLogger
	defer func() { defaultLogger = prev }()

	sl, _ := newTestLogger(t, logrus.InfoLevel)
	SetLogger(sl)
	assert.Equal(t, sl, GetLogger(), "GetLogger should return the logger set via SetLogger")
}

func TestGetSetLogLevel(t *testing.T) {
	prev := defaultLogger
	defer func() { defaultLogger = prev }()

	sl, _ := newTestLogger(t, logrus.InfoLevel)
	SetLogger(sl)

	assert.Equal(t, logrus.InfoLevel, GetLogLevel())

	SetLogLevel(logrus.DebugLevel)
	assert.Equal(t, logrus.DebugLevel, GetLogLevel())
}
