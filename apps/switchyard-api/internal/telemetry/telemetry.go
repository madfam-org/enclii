// Package telemetry is the switchyard-api glue for the shared MADFAM
// OpenTelemetry setup. It calls packages/otel-go.SetupOTel with the
// service-specific resource and wires the logrus trace-id hook.
//
// Keep this file tiny — per-module spans (reconciler, builder, API
// handlers) live alongside the modules that own them. The package's only
// job is to wire the SDK at boot and hand back a shutdown function.
package telemetry

import (
	"context"
	"os"
	"time"

	"github.com/sirupsen/logrus"

	mstel "github.com/madfam-org/enclii/packages/otel-go"
)

// ServiceName is the canonical service.name attribute for switchyard-api.
// Importing this constant (instead of duplicating the string) across the
// codebase prevents drift when spans come from different packages.
const ServiceName = "switchyard-api"

// Setup initializes OpenTelemetry for switchyard-api. The returned
// shutdown MUST be deferred from main. On failure it logs and returns a
// no-op shutdown — trace loss is acceptable, boot failure is not.
func Setup(ctx context.Context, environment string) func(context.Context) error {
	version := os.Getenv("SERVICE_VERSION")
	if version == "" {
		version = os.Getenv("ENCLII_BUILD_SHA")
	}
	shutdown, err := mstel.SetupOTel(ctx, mstel.Config{
		ServiceName:     ServiceName,
		ServiceVersion:  version,
		Environment:     environment,
		Namespace:       "enclii",
		ShutdownTimeout: 5 * time.Second,
	})
	if err != nil {
		logrus.WithError(err).Warn("OpenTelemetry setup failed (continuing without traces)")
	} else {
		logrus.WithField("service", ServiceName).Info("✓ OpenTelemetry initialized")
	}
	// Install the trace-id hook on the global logrus regardless of SDK
	// setup outcome — the hook is a no-op when no span is active.
	logrus.AddHook(mstel.NewTraceIDHook())
	return shutdown
}
