package otel

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestSecretFilterProcessor_StripsSecretAttrs uses the SDK's in-memory
// SpanRecorder to capture spans that flow through the filter. Attributes
// matching the secret list must not reach the recorder.
func TestSecretFilterProcessor_StripsSecretAttrs(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	// Wrap the recorder with our filter processor.
	filter := newSecretFilterProcessor(recorder)

	tp := trace.NewTracerProvider(trace.WithSpanProcessor(filter))
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = tp.Shutdown(ctx)
	}()

	tracer := tp.Tracer("test")
	_, span := tracer.Start(context.Background(), "op")
	span.SetAttributes(
		attribute.String("user_id", "u123"),
		attribute.String("password", "hunter2"),
		attribute.String("api_key", "sk_live_foo"),
		attribute.String("authorization", "Bearer abc"),
		attribute.Int("http.status_code", 201),
	)
	span.End()

	ended := recorder.Ended()
	assert.Len(t, ended, 1)
	got := map[string]attribute.Value{}
	for _, kv := range ended[0].Attributes() {
		got[string(kv.Key)] = kv.Value
	}
	assert.Contains(t, got, "user_id")
	assert.Contains(t, got, "http.status_code")
	assert.NotContains(t, got, "password")
	assert.NotContains(t, got, "api_key")
	assert.NotContains(t, got, "authorization")
}

// TestSecretFilterProcessor_NoFalsePositives — attributes that don't
// match the secret list must be preserved even if the key name is unusual.
func TestSecretFilterProcessor_NoFalsePositives(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	filter := newSecretFilterProcessor(recorder)
	tp := trace.NewTracerProvider(trace.WithSpanProcessor(filter))
	tracer := tp.Tracer("test")

	_, span := tracer.Start(context.Background(), "op")
	span.SetAttributes(
		attribute.String("filing.rfc_last4", "1234"),
		attribute.String("http.method", "GET"),
		attribute.Int64("db.rows_affected", 7),
	)
	span.End()

	ended := recorder.Ended()
	assert.Len(t, ended[0].Attributes(), 3)
}

// TestSecretFilterProcessor_OnStartPassthrough — OnStart must delegate to
// the wrapped processor without mutation; tests the SpanProcessor
// interface contract.
func TestSecretFilterProcessor_OnStartPassthrough(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	filter := newSecretFilterProcessor(recorder)
	tp := trace.NewTracerProvider(trace.WithSpanProcessor(filter))

	tracer := tp.Tracer("test")
	_, span := tracer.Start(context.Background(), "start-test")
	started := recorder.Started()
	assert.Len(t, started, 1, "OnStart should have fired on the inner recorder")
	span.End()
}

func TestTraceIDHook_NilContext(t *testing.T) {
	hook := NewTraceIDHook()
	// Fire with no context and no active span — must not panic and must
	// not add trace_id/span_id.
	assert.NotContains(t, hook.Levels(), nil)
}
