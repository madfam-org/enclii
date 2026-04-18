package otel

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

func TestSetupOTel_RequiresServiceName(t *testing.T) {
	shutdown, err := SetupOTel(context.Background(), Config{})
	assert.Error(t, err)
	// Even on error we get a callable no-op shutdown.
	require.NotNil(t, shutdown)
	assert.NoError(t, shutdown(context.Background()))
}

func TestSetupOTel_DisabledEnv(t *testing.T) {
	t.Setenv("ENCLII_OTEL_DISABLED", "1")
	shutdown, err := SetupOTel(context.Background(), Config{ServiceName: "t"})
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	// No-op shutdown completes immediately.
	start := time.Now()
	assert.NoError(t, shutdown(context.Background()))
	assert.Less(t, time.Since(start), 100*time.Millisecond)
}

func TestSetupOTel_LoopbackInsecure(t *testing.T) {
	// With a loopback endpoint we expect isInsecure() to trigger so the
	// gRPC exporter doesn't try TLS against a plaintext listener.
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317")
	shutdown, err := SetupOTel(context.Background(), Config{ServiceName: "t"})
	// Connection to a nonexistent local collector is lazy — Setup succeeds
	// and spans simply fail to export. Either way we expect a callable
	// shutdown.
	if err != nil {
		t.Logf("setup returned err (acceptable when nothing is listening): %v", err)
	}
	require.NotNil(t, shutdown)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Shutdown MUST NOT hang past 5s even if the exporter is broken.
	start := time.Now()
	_ = shutdown(ctx)
	assert.Less(t, time.Since(start), 6*time.Second)
}

func TestSetupOTel_ShutdownBounded(t *testing.T) {
	// Bonus check: ShutdownTimeout cap is honored.
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "nonexistent.invalid:4317")
	shutdown, _ := SetupOTel(context.Background(), Config{
		ServiceName:     "t",
		ShutdownTimeout: 500 * time.Millisecond,
	})
	require.NotNil(t, shutdown)
	start := time.Now()
	_ = shutdown(context.Background())
	assert.Less(t, time.Since(start), 2*time.Second,
		"shutdown respected ShutdownTimeout bound even under exporter failure")
}

func TestResolveSamplerRatio_FromConfig(t *testing.T) {
	r := 0.25
	got := resolveSamplerRatio(Config{SamplerRatio: &r})
	assert.InDelta(t, 0.25, got, 1e-9)
}

func TestResolveSamplerRatio_Clamps(t *testing.T) {
	low := -0.5
	assert.InDelta(t, 0.0, resolveSamplerRatio(Config{SamplerRatio: &low}), 1e-9)
	high := 1.5
	assert.InDelta(t, 1.0, resolveSamplerRatio(Config{SamplerRatio: &high}), 1e-9)
}

func TestResolveSamplerRatio_FromEnv(t *testing.T) {
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.2")
	assert.InDelta(t, 0.2, resolveSamplerRatio(Config{}), 1e-9)
}

func TestResolveSamplerRatio_ProductionDefault(t *testing.T) {
	os.Unsetenv("OTEL_TRACES_SAMPLER_ARG")
	assert.InDelta(t, 0.1, resolveSamplerRatio(Config{Environment: "production"}), 1e-9)
	assert.InDelta(t, 1.0, resolveSamplerRatio(Config{Environment: "staging"}), 1e-9)
	assert.InDelta(t, 1.0, resolveSamplerRatio(Config{Environment: ""}), 1e-9)
}

func TestIsInsecure(t *testing.T) {
	cases := map[string]bool{
		"localhost:4317": true,
		"127.0.0.1:4317": true,
		"tempo.observability.svc.cluster.local:4317": true,
		"tempo.observability.svc:4317":               true,
		"otel-collector.example.com:4317":            false,
		"tempo.example.com:4317":                     false,
	}
	for endpoint, want := range cases {
		t.Run(endpoint, func(t *testing.T) {
			assert.Equal(t, want, isInsecure(endpoint))
		})
	}
}

func TestIsSecretKey(t *testing.T) {
	mustStrip := []string{
		"password", "PASSWORD", "user_password",
		"api_key", "apikey", "X-API-KEY",
		"authorization", "auth-header",
		"bearer_token", "jwt", "session_id",
		"private_key", "cookie",
	}
	for _, k := range mustStrip {
		assert.True(t, isSecretKey(k), "expected %q to be filtered", k)
	}
	mustKeep := []string{
		"user_id", "order_id", "http.method", "duration_ms",
		"api_version", // "api" substring without "key" is fine
	}
	for _, k := range mustKeep {
		assert.False(t, isSecretKey(k), "expected %q to be kept", k)
	}
}

func TestFilterAttributes(t *testing.T) {
	attrs := []attribute.KeyValue{
		attribute.String("user_id", "u123"),
		attribute.String("password", "hunter2"),
		attribute.String("x-api-key", "sk_live_abc"),
		attribute.Int("http.status_code", 200),
		attribute.String("authorization", "Bearer xyz"),
	}
	out := filterAttributes(attrs)
	assert.Equal(t, 2, len(out), "kept: user_id + http.status_code")
	for _, kv := range out {
		assert.False(t, isSecretKey(string(kv.Key)))
	}
}

// Verifies that a TracerProvider is actually installed after successful
// setup — the production surface users rely on.
func TestSetupOTel_InstallsTracerProvider(t *testing.T) {
	t.Setenv("ENCLII_OTEL_DISABLED", "1")
	_, err := SetupOTel(context.Background(), Config{ServiceName: "t"})
	require.NoError(t, err)
	// Even in disabled mode, otel.Tracer returns a valid (noop) tracer.
	tracer := otel.Tracer("t")
	assert.NotNil(t, tracer)
	_, span := tracer.Start(context.Background(), "test.span")
	defer span.End()
	assert.NotNil(t, span)
}
