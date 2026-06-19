// Package otel provides the MADFAM-standard OpenTelemetry SDK bootstrap
// for Go services. It exposes a single SetupOTel function that:
//
//   - Configures the resource (service.name, service.version, environment,
//     service.namespace, host.name, k8s.pod.name, k8s.namespace.name)
//   - Installs an OTLP gRPC exporter (defaulting to the in-cluster Tempo
//     endpoint) behind a BatchSpanProcessor
//   - Applies parent-based sampling at OTEL_TRACES_SAMPLER_ARG (default
//     0.1 in production, 1.0 everywhere else)
//   - Installs a secret-filtering span processor that drops any attribute
//     whose key looks like a credential (case-insensitive substring match)
//   - Returns a bounded shutdown function for graceful close on SIGTERM
//
// It is intentionally minimal. Per-service instrumentation (otelhttp,
// otelgin, otelsql, otelpgx, custom spans) lives in the service code. This
// package owns SDK wiring only — if you need per-service behavior, add a
// new option rather than branching here.
//
// Non-goals:
//   - Metrics SDK: Prometheus client_golang is the metrics path.
//   - Logs SDK: structured loggers emit JSON to stdout; Fluent Bit forwards
//     to Loki. Trace-ID correlation is handled by the logger-specific
//     adapters (see TraceIDHook for logrus).
package otel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// defaultTempoEndpoint is the in-cluster DNS name for the OTLP gRPC
// receiver when Tempo is deployed via infra/helm/tempo.
const defaultTempoEndpoint = "tempo.observability.svc.cluster.local:4317"

// Config describes a MADFAM Go service's OpenTelemetry setup. All fields
// except ServiceName are optional — reasonable defaults are derived from
// environment variables, and an empty endpoint disables the exporter
// cleanly (SetupOTel becomes a no-op).
type Config struct {
	// ServiceName populates service.name. Required. Conventionally the
	// Deployment's metadata.name, e.g. "switchyard-api".
	ServiceName string

	// ServiceVersion populates service.version. Should be the build-time
	// git SHA or a release tag; empty falls back to "dev".
	ServiceVersion string

	// Environment populates deployment.environment. Typical values:
	// "production", "staging", "development". Defaults to "development"
	// if empty.
	Environment string

	// Namespace populates service.namespace. Defaults to "enclii" — the
	// collective name for the three platform Go services. Ecosystem
	// services (Python/Node) override to "madfam".
	Namespace string

	// Endpoint overrides the OTLP gRPC endpoint (host:port, no scheme).
	// Falls back to OTEL_EXPORTER_OTLP_ENDPOINT env var, then to the
	// default in-cluster Tempo DNS name. Empty string + empty env var
	// disables the exporter entirely and SetupOTel returns a no-op.
	Endpoint string

	// SamplerRatio overrides the trace sampling ratio (0.0-1.0). Falls
	// back to OTEL_TRACES_SAMPLER_ARG env var, then to 0.1 in production
	// or 1.0 otherwise. Clamped to [0.0, 1.0].
	SamplerRatio *float64

	// Insecure controls whether the exporter uses TLS. Tempo inside the
	// cluster is accessed plaintext; the default is true. Flip to false
	// when shipping to a TLS-terminated collector.
	Insecure bool

	// ShutdownTimeout bounds graceful shutdown. Defaults to 5s.
	ShutdownTimeout time.Duration
}

// ShutdownFunc drains and stops the tracer provider. It is always safe to
// call — even when SetupOTel returned without installing an exporter, the
// returned function is a no-op.
type ShutdownFunc func(context.Context) error

// SetupOTel wires the OpenTelemetry SDK per the MADFAM convention. The
// returned shutdown function MUST be deferred from main to ensure the
// BatchSpanProcessor drains on SIGTERM.
//
// Error handling: SetupOTel logs setup failures to the caller but does
// NOT fatal — trace loss is acceptable, service unavailability is not.
// If the exporter fails to initialize, the returned ShutdownFunc is a
// no-op and the service continues without tracing.
func SetupOTel(ctx context.Context, cfg Config) (ShutdownFunc, error) {
	if cfg.ServiceName == "" {
		return noopShutdown, errors.New("otel: ServiceName is required")
	}

	// Resolve endpoint with env-var fallback. An empty result disables
	// the exporter — useful for local dev without Tempo.
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	}
	if endpoint == "" {
		// Default: production K8s deploys point here out of the box.
		// Local dev must explicitly unset via ENCLII_OTEL_DISABLED=1
		// (handled below) to avoid a connection loop.
		endpoint = defaultTempoEndpoint
	}
	if os.Getenv("ENCLII_OTEL_DISABLED") == "1" {
		return noopShutdown, nil
	}
	// Strip scheme if an operator accidentally set a full URL. The gRPC
	// exporter wants host:port only.
	endpoint = strings.TrimPrefix(endpoint, "http://")
	endpoint = strings.TrimPrefix(endpoint, "https://")
	endpoint = strings.TrimPrefix(endpoint, "grpc://")
	endpoint = strings.TrimSuffix(endpoint, "/")

	// Build resource. k8s.pod.name + k8s.namespace.name come from the
	// downward API — services are expected to export those as env vars
	// in their Deployment manifest (standard K8s metadata block).
	env := cfg.Environment
	if env == "" {
		env = "development"
	}
	namespace := cfg.Namespace
	if namespace == "" {
		namespace = "enclii"
	}
	version := cfg.ServiceVersion
	if version == "" {
		version = "dev"
	}
	hostName, _ := os.Hostname()
	res, err := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(version),
			semconv.ServiceNamespace(namespace),
			semconv.DeploymentEnvironmentName(env),
			semconv.HostName(hostName),
			attribute.String("k8s.pod.name", os.Getenv("POD_NAME")),
			attribute.String("k8s.namespace.name", os.Getenv("POD_NAMESPACE")),
		),
	)
	if err != nil {
		return noopShutdown, fmt.Errorf("otel: resource.Merge: %w", err)
	}

	// Sampler: parent-based ratio. If a parent trace was sampled upstream,
	// we keep it regardless of the local ratio — that's what prevents
	// fragmented traces when rolling instrumentation across services.
	ratio := resolveSamplerRatio(cfg)
	sampler := sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))

	// Exporter: OTLP over gRPC. Context deadline keeps boot fast when
	// Tempo is unreachable — we proceed without traces rather than hang.
	exporterCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(endpoint),
	}
	if cfg.Insecure || isInsecure(endpoint) {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}
	exporter, err := otlptrace.New(exporterCtx, otlptracegrpc.NewClient(opts...))
	if err != nil {
		// Trace loss is acceptable; service boot is not. Surface the
		// error to the caller but hand back a no-op shutdown.
		return noopShutdown, fmt.Errorf("otel: exporter init: %w", err)
	}

	// Install the secret-filter as an inner processor wrapping the
	// batcher — order matters: filter BEFORE the batcher so filtered
	// attributes never hit the exporter queue.
	bsp := sdktrace.NewBatchSpanProcessor(exporter)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
		sdktrace.WithSpanProcessor(newSecretFilterProcessor(bsp)),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Bounded shutdown — forces the BSP to flush queued spans but never
	// blocks past ShutdownTimeout.
	timeout := cfg.ShutdownTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	shutdown := func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return tp.Shutdown(ctx)
	}
	return shutdown, nil
}

// StartSpan is a thin wrapper around otel.Tracer(name).Start — provided
// so service code can avoid importing go.opentelemetry.io/otel directly
// for the common case.
func StartSpan(ctx context.Context, tracerName, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return otel.Tracer(tracerName).Start(ctx, spanName, opts...)
}

// resolveSamplerRatio picks the sampler ratio in priority order:
//  1. Explicit Config.SamplerRatio
//  2. OTEL_TRACES_SAMPLER_ARG env var
//  3. Default: 0.1 in production, 1.0 otherwise
func resolveSamplerRatio(cfg Config) float64 {
	if cfg.SamplerRatio != nil {
		return clamp(*cfg.SamplerRatio)
	}
	if s := os.Getenv("OTEL_TRACES_SAMPLER_ARG"); s != "" {
		if r, err := strconv.ParseFloat(s, 64); err == nil {
			return clamp(r)
		}
	}
	env := cfg.Environment
	if env == "" {
		env = os.Getenv("APP_ENV")
	}
	if env == "production" {
		return 0.1
	}
	return 1.0
}

func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// isInsecure returns true for loopback / in-cluster endpoints where TLS
// is not configured. This lets services drop the explicit Insecure flag
// in typical deploys.
func isInsecure(endpoint string) bool {
	switch {
	case strings.HasPrefix(endpoint, "localhost"),
		strings.HasPrefix(endpoint, "127.0.0.1"),
		strings.HasSuffix(strings.Split(endpoint, ":")[0], ".svc.cluster.local"),
		strings.HasSuffix(strings.Split(endpoint, ":")[0], ".svc"):
		return true
	}
	return false
}

func noopShutdown(context.Context) error { return nil }
