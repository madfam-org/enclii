package api

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// nopLogger is a no-op Logger for testing.
type nopLogger struct{}

func (nopLogger) Debug(_ context.Context, _ string, _ ...logging.Field) {}
func (nopLogger) Info(_ context.Context, _ string, _ ...logging.Field)  {}
func (nopLogger) Warn(_ context.Context, _ string, _ ...logging.Field)  {}
func (nopLogger) Error(_ context.Context, _ string, _ ...logging.Field) {}
func (nopLogger) Fatal(_ context.Context, _ string, _ ...logging.Field) {}
func (n nopLogger) WithField(_ string, _ interface{}) logging.Logger    { return n }
func (n nopLogger) WithFields(_ logging.Fields) logging.Logger          { return n }
func (n nopLogger) WithError(_ error) logging.Logger                    { return n }
func (n nopLogger) WithContext(_ context.Context) logging.Logger        { return n }

func newNopLogger() logging.Logger { return nopLogger{} }

func TestIsValidDomain(t *testing.T) {
	tests := []struct {
		domain string
		want   bool
	}{
		{"api.example.com", true},
		{"sub.domain.co.uk", true},
		{"example.com", true},
		{"a.b.c.d.example.com", true},
		{"x123.example.com", true},
		{"", false},
		{"no-tld", false},
		{".starts-with-dot.com", false},
		{"ends-with-dot.com.", false},
		{"..", false},
		{"-starts-with-hyphen.com", false},
		{"a", false}, // no dot
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			if got := isValidDomain(tt.domain); got != tt.want {
				t.Errorf("isValidDomain(%q) = %v, want %v", tt.domain, got, tt.want)
			}
		})
	}
}

func TestProvisionDomainsFromYAML_NilConfig(t *testing.T) {
	h := &Handler{
		logger: newNopLogger(),
	}

	// nil config should return immediately without panic
	h.provisionDomainsFromYAML(context.Background(), &types.Service{
		ID:   uuid.New(),
		Name: "test-svc",
	}, nil)
}

func TestProvisionDomainsFromYAML_EmptyDomains(t *testing.T) {
	h := &Handler{
		logger: newNopLogger(),
	}

	cfg := &EncliiYAML{
		Spec: EncliiYAMLSpec{
			Domains: []EncliiYAMLDomain{},
		},
	}

	// Empty domains should return immediately without panic
	h.provisionDomainsFromYAML(context.Background(), &types.Service{
		ID:   uuid.New(),
		Name: "test-svc",
	}, cfg)
}

func TestEnsureTunnelRoute_NilService(t *testing.T) {
	h := &Handler{
		tunnelRoutesService: nil,
		logger:              newNopLogger(),
	}

	// nil tunnelRoutesService should return immediately without panic
	h.ensureTunnelRoute(context.Background(), "example.com", &types.Service{
		ID:   uuid.New(),
		Name: "test-svc",
	}, "production")
}

func TestEnsureDNSRecord_NilDomainSyncService(t *testing.T) {
	h := &Handler{
		domainSyncService: nil,
		logger:            newNopLogger(),
	}

	// nil domainSyncService should return immediately without panic
	h.ensureDNSRecord(context.Background(), "example.com")
}

func TestCleanupDomainsForService_NilGuards(t *testing.T) {
	// Verify that cleanup helper methods handle nil services gracefully
	h := &Handler{
		tunnelRoutesService: nil,
		domainSyncService:   nil,
		logger:              newNopLogger(),
	}

	// ensureTunnelRoute with nil tunnelRoutesService returns immediately
	h.ensureTunnelRoute(context.Background(), "test.com", &types.Service{
		ID:   uuid.New(),
		Name: "svc",
	}, "production")

	// ensureDNSRecord with nil domainSyncService returns immediately
	h.ensureDNSRecord(context.Background(), "test.com")
}

func TestProvisionSingleDomain_InvalidDomain(t *testing.T) {
	h := &Handler{
		logger: newNopLogger(),
	}

	// Invalid domain (no TLD) should be skipped without panic
	h.provisionSingleDomain(context.Background(), &types.Service{
		ID:   uuid.New(),
		Name: "test-svc",
	}, EncliiYAMLDomain{
		Name:        "invalid",
		Environment: "production",
	})
}
