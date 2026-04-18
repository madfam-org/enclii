package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

// traceSpanContext is a thin re-export so the main telemetry.go file
// can stay import-minimal. Separating this makes it trivial to extend
// with more trace helpers (baggage, propagation, etc.) later.
func traceSpanContext(ctx context.Context) trace.SpanContext {
	return trace.SpanContextFromContext(ctx)
}
