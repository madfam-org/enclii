// Package telemetry wires OpenTelemetry for Waybill (api + aggregator).
// See apps/switchyard-api/internal/telemetry for the reference comment.
package telemetry

import (
	"context"
	"os"
	"time"

	"go.uber.org/zap"

	mstel "github.com/madfam-org/enclii/packages/otel-go"
)

const ServiceName = "waybill"

func Setup(ctx context.Context, environment string, logger *zap.Logger) func(context.Context) error {
	return SetupWithName(ctx, ServiceName, environment, logger)
}

func SetupWithName(ctx context.Context, name, environment string, logger *zap.Logger) func(context.Context) error {
	version := os.Getenv("SERVICE_VERSION")
	if version == "" {
		version = os.Getenv("ENCLII_BUILD_SHA")
	}
	shutdown, err := mstel.SetupOTel(ctx, mstel.Config{
		ServiceName:     name,
		ServiceVersion:  version,
		Environment:     environment,
		Namespace:       "enclii",
		ShutdownTimeout: 5 * time.Second,
	})
	if err != nil && logger != nil {
		logger.Warn("OpenTelemetry setup failed (continuing without traces)", zap.Error(err))
	} else if logger != nil {
		logger.Info("OpenTelemetry initialized", zap.String("service", name))
	}
	return shutdown
}

// TraceIDFields returns zap fields stamped with trace_id/span_id when the
// context carries an active span. When no span is active, returns an
// empty slice so it's safe to splice into every Info/Error call.
//
// Usage:
//
//	logger.With(telemetry.TraceIDFields(ctx)...).Info("...")
//
// For logrus-based services, the TraceIDHook in packages/otel-go handles
// this automatically. Zap doesn't expose the same hook surface, so each
// log call must opt in explicitly.
func TraceIDFields(ctx context.Context) []zap.Field {
	sc := traceSpanContext(ctx)
	if !sc.IsValid() {
		return nil
	}
	return []zap.Field{
		zap.String("trace_id", sc.TraceID().String()),
		zap.String("span_id", sc.SpanID().String()),
	}
}
