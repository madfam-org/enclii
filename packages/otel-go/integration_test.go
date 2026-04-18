package otel

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
)

// mockOTLPTraceServer implements the OTLP gRPC trace service and records
// every ExportTraceServiceRequest. Stands in for Tempo in integration
// tests — we get the full exporter path exercised without needing the
// real Tempo binary.
type mockOTLPTraceServer struct {
	collectortrace.UnimplementedTraceServiceServer
	mu   sync.Mutex
	reqs []*collectortrace.ExportTraceServiceRequest
}

func (m *mockOTLPTraceServer) Export(ctx context.Context, req *collectortrace.ExportTraceServiceRequest) (*collectortrace.ExportTraceServiceResponse, error) {
	m.mu.Lock()
	m.reqs = append(m.reqs, req)
	m.mu.Unlock()
	return &collectortrace.ExportTraceServiceResponse{}, nil
}

func (m *mockOTLPTraceServer) spans() []*tracev1.Span {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*tracev1.Span
	for _, r := range m.reqs {
		for _, rs := range r.GetResourceSpans() {
			for _, ss := range rs.GetScopeSpans() {
				out = append(out, ss.GetSpans()...)
			}
		}
	}
	return out
}

func startMockCollector(t *testing.T) (endpoint string, mock *mockOTLPTraceServer) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := grpc.NewServer()
	mock = &mockOTLPTraceServer{}
	collectortrace.RegisterTraceServiceServer(server, mock)
	go func() { _ = server.Serve(lis) }()

	t.Cleanup(func() {
		server.GracefulStop()
		_ = lis.Close()
	})

	return lis.Addr().String(), mock
}

// TestSetupOTel_EndToEnd — full integration: boot the SDK pointing at a
// mock OTLP collector, emit a span with both safe and secret attributes,
// verify the collector received the span AND the secret attributes were
// filtered before export.
func TestSetupOTel_EndToEnd(t *testing.T) {
	endpoint, mock := startMockCollector(t)

	ratio := 1.0
	shutdown, err := SetupOTel(context.Background(), Config{
		ServiceName:     "integration-test",
		ServiceVersion:  "v0.0.1",
		Environment:     "test",
		Endpoint:        endpoint,
		Insecure:        true,
		SamplerRatio:    &ratio,
		ShutdownTimeout: 3 * time.Second,
	})
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	_, span := StartSpan(context.Background(), "integration-test", "work")
	span.SetAttributes(
		attribute.String("user_id", "u42"),
		attribute.String("password", "hunter2"),
		attribute.String("api_key", "sk_live_nope"),
		attribute.String("http.method", "POST"),
	)
	span.End()

	// Force flush so the mock sees the span before we assert.
	ctx2, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, shutdown(ctx2))

	spans := mock.spans()
	require.NotEmpty(t, spans, "collector should have received at least one span")

	var target *tracev1.Span
	for _, s := range spans {
		if s.GetName() == "work" {
			target = s
			break
		}
	}
	require.NotNil(t, target, "collector received the named span")

	gotKeys := map[string]string{}
	for _, kv := range target.GetAttributes() {
		gotKeys[kv.GetKey()] = kv.GetValue().GetStringValue()
	}
	assert.Contains(t, gotKeys, "user_id")
	assert.Contains(t, gotKeys, "http.method")
	assert.NotContains(t, gotKeys, "password", "password MUST be filtered")
	assert.NotContains(t, gotKeys, "api_key", "api_key MUST be filtered")

	// Resource attributes should include the service.name we configured.
	var foundServiceName bool
	for _, rs := range mock.reqs {
		for _, r := range rs.GetResourceSpans() {
			for _, attr := range r.GetResource().GetAttributes() {
				if attr.GetKey() == "service.name" && attr.GetValue().GetStringValue() == "integration-test" {
					foundServiceName = true
				}
			}
		}
	}
	assert.True(t, foundServiceName, "exported resource has service.name=integration-test")
}
