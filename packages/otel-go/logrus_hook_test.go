package otel

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace"
)

// TestTraceIDHook_AddsFieldsWhenSpanActive — when a context carries an
// active span, the hook must inject trace_id and span_id into the log
// entry's data map.
func TestTraceIDHook_AddsFieldsWhenSpanActive(t *testing.T) {
	tp := trace.NewTracerProvider()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tp.Shutdown(ctx)
	}()
	tracer := tp.Tracer("hook-test")
	ctx, span := tracer.Start(context.Background(), "op")
	defer span.End()

	buf := &bytes.Buffer{}
	logger := logrus.New()
	logger.SetOutput(buf)
	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.AddHook(NewTraceIDHook())

	logger.WithContext(ctx).Info("something happened")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &parsed))
	assert.Contains(t, parsed, "trace_id", "trace_id must be present")
	assert.Contains(t, parsed, "span_id", "span_id must be present")
	assert.NotEmpty(t, parsed["trace_id"])
	assert.NotEmpty(t, parsed["span_id"])
}

// TestTraceIDHook_NoSpanNoFields — no active span means no trace_id
// injection. The log line is still valid JSON.
func TestTraceIDHook_NoSpanNoFields(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := logrus.New()
	logger.SetOutput(buf)
	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.AddHook(NewTraceIDHook())

	logger.Info("no context here")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &parsed))
	assert.NotContains(t, parsed, "trace_id")
	assert.NotContains(t, parsed, "span_id")
}

// TestTraceIDHook_WithContextButNoSpan — context present, no span inside;
// hook must not add fields.
func TestTraceIDHook_WithContextButNoSpan(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := logrus.New()
	logger.SetOutput(buf)
	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.AddHook(NewTraceIDHook())

	logger.WithContext(context.Background()).Warn("bare context")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &parsed))
	assert.NotContains(t, parsed, "trace_id")
}

// TestTraceIDHook_FiresOnAllLevels sanity — Levels() returns all logrus
// levels including Debug.
func TestTraceIDHook_AllLevels(t *testing.T) {
	hook := NewTraceIDHook()
	assert.ElementsMatch(t, logrus.AllLevels, hook.Levels())
}
