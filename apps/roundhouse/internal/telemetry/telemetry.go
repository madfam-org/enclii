// Package telemetry wires OpenTelemetry for the Roundhouse API + worker.
// See apps/switchyard-api/internal/telemetry for the reference comment.
package telemetry

import (
	"context"
	"os"
	"time"

	"go.uber.org/zap"

	mstel "github.com/madfam-org/enclii/packages/otel-go"
)

// ServiceName — Roundhouse runs as two processes (api, worker) that share
// the same service.name but can be distinguished by host.name / pod.name.
// If we later want distinct service.names (roundhouse-api vs
// roundhouse-worker), the constructors accept an override via
// SetupWithName.
const ServiceName = "roundhouse"

// Setup is the default bootstrap — service.name = "roundhouse".
func Setup(ctx context.Context, environment string, logger *zap.Logger) func(context.Context) error {
	return SetupWithName(ctx, ServiceName, environment, logger)
}

// SetupWithName lets the api and worker binaries register distinct
// service.name values if that turns out to be useful for the service
// graph (e.g., "roundhouse-api" vs "roundhouse-worker").
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
