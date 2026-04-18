package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// TraceIDFields returns zap fields stamped with trace_id/span_id when the
// context carries an active span. Empty slice when no span is active so
// it's safe to splice into every logger call:
//
//	logger.With(telemetry.TraceIDFields(ctx)...).Info("...")
func TraceIDFields(ctx context.Context) []zap.Field {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return nil
	}
	return []zap.Field{
		zap.String("trace_id", sc.TraceID().String()),
		zap.String("span_id", sc.SpanID().String()),
	}
}
