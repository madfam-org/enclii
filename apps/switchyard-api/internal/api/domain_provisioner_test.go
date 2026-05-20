package api

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/manifest"
	route "github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
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

type mockTunnelRoutesManager struct {
	routes map[string]*route.RouteSpec
}

func newMockTunnelRoutesManager() *mockTunnelRoutesManager {
	return &mockTunnelRoutesManager{routes: map[string]*route.RouteSpec{}}
}

func (m *mockTunnelRoutesManager) AddRoute(_ context.Context, spec *route.RouteSpec) error {
	m.routes[spec.Hostname] = spec
	return nil
}

func (m *mockTunnelRoutesManager) RemoveRoute(_ context.Context, hostname string) error {
	delete(m.routes, hostname)
	return nil
}

func (m *mockTunnelRoutesManager) ListRoutes(_ context.Context) ([]route.IngressRule, error) {
	routes := make([]route.IngressRule, 0, len(m.routes))
	for _, spec := range m.routes {
		routes = append(routes, route.IngressRule{
			Hostname: spec.Hostname,
			Service:  "http://" + spec.ServiceName + "." + spec.ServiceNamespace + ".svc.cluster.local:80",
		})
	}
	return routes, nil
}

func (m *mockTunnelRoutesManager) RouteExists(_ context.Context, hostname string) (bool, error) {
	_, ok := m.routes[hostname]
	return ok, nil
}

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

	cfg := &manifest.EncliiYAML{
		Spec: manifest.EncliiYAMLSpec{
			Domains: []manifest.EncliiYAMLDomain{},
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
	}, "production", 80)
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
	}, "production", 80)

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
	}, manifest.EncliiYAMLDomain{
		Name:        "invalid",
		Environment: "production",
	}, 80)
}

func TestResolveServiceNamespace_PrefersServiceK8sNamespace(t *testing.T) {
	namespace := "converge-dash"
	h := &Handler{
		logger: newNopLogger(),
	}

	got := h.resolveServiceNamespace(context.Background(), &types.Service{
		ID:           uuid.New(),
		Name:         "converge-web",
		K8sNamespace: &namespace,
	}, "production")

	if got != namespace {
		t.Fatalf("resolveServiceNamespace() = %q, want %q", got, namespace)
	}
}

func TestResolveServiceNamespace_StagingPrefersEnvironmentNamespace(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer func() { _ = database.Close() }()

	projectID := uuid.New()
	envID := uuid.New()
	now := time.Now()
	productionNamespace := "dhanam"

	mock.ExpectQuery("SELECT id, project_id, name, kube_namespace, created_at, updated_at FROM environments WHERE project_id = \\$1 AND name = \\$2").
		WithArgs(projectID, "staging").
		WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "name", "kube_namespace", "created_at", "updated_at"}).
			AddRow(envID, projectID, "staging", "enclii-dhanam-staging", now, now))

	h := &Handler{
		repos:  db.NewRepositories(database),
		logger: newNopLogger(),
	}

	got := h.resolveServiceNamespace(context.Background(), &types.Service{
		ID:           uuid.New(),
		ProjectID:    projectID,
		Name:         "dhanam-api",
		K8sNamespace: &productionNamespace,
	}, "staging")

	if got != "enclii-dhanam-staging" {
		t.Fatalf("resolveServiceNamespace(staging) = %q, want enclii-dhanam-staging", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestEnsureTunnelRouteUpdatesWrongExistingTarget(t *testing.T) {
	tunnelRoutes := newMockTunnelRoutesManager()
	tunnelRoutes.routes["staging-admin.dhan.am"] = &route.RouteSpec{
		Hostname:         "staging-admin.dhan.am",
		ServiceName:      "dhanam-admin",
		ServiceNamespace: "dhanam",
		ServicePort:      80,
	}
	stagingNamespace := "enclii-dhanam-staging"
	h := &Handler{
		tunnelRoutesService: tunnelRoutes,
		logger:              newNopLogger(),
	}

	h.ensureTunnelRoute(context.Background(), "staging-admin.dhan.am", &types.Service{
		ID:           uuid.New(),
		Name:         "dhanam-admin",
		K8sNamespace: &stagingNamespace,
	}, "staging", 80)

	spec := tunnelRoutes.routes["staging-admin.dhan.am"]
	if spec == nil {
		t.Fatalf("expected route spec to be recorded")
	}
	if spec.ServiceNamespace != stagingNamespace {
		t.Fatalf("ServiceNamespace = %q, want %q", spec.ServiceNamespace, stagingNamespace)
	}
}

func TestDefaultProductionNamespaceUsesProjectSlug(t *testing.T) {
	got := defaultProductionNamespace(&types.Project{
		Name: "Tulana Pricing",
		Slug: "tulana",
	})

	if got != "tulana" {
		t.Fatalf("defaultProductionNamespace() = %q, want tulana", got)
	}
}

func TestEnsureJunctionInfrastructureUsesResolvedServiceNamespace(t *testing.T) {
	tunnelRoutes := newMockTunnelRoutesManager()
	namespace := "tulana"
	h := &Handler{
		tunnelRoutesService: tunnelRoutes,
		logger:              newNopLogger(),
	}

	summary := h.ensureJunctionInfrastructure(context.Background(), "tulana-app.madfam.io", &types.Service{
		ID:           uuid.New(),
		Name:         "tulana-web",
		K8sNamespace: &namespace,
	})

	if !summary.TunnelRouteReady {
		t.Fatalf("expected tunnel route to be ready, got %#v", summary)
	}

	spec := tunnelRoutes.routes["tulana-app.madfam.io"]
	if spec == nil {
		t.Fatalf("expected route spec to be recorded")
	}
	if spec.ServiceNamespace != "tulana" {
		t.Fatalf("ServiceNamespace = %q, want tulana", spec.ServiceNamespace)
	}
	if spec.ServiceName != "tulana-web" {
		t.Fatalf("ServiceName = %q, want tulana-web", spec.ServiceName)
	}
}
