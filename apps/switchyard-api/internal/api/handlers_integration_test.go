//go:build integration

package api

import (
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
)

func setupTestHandler() *Handler {
	gin.SetMode(gin.TestMode)

	// For integration tests, use nil for all dependencies
	// Full integration tests should use proper test database setup
	handler := NewHandler(
		nil, // repos
		&config.Config{Registry: "test-registry"},
		nil, // auth manager
		nil, // cache
		nil, // builder
		nil, // k8s client
		nil, // controller
		nil, // reconciler
		nil, // metrics
		nil, // logger
		nil, // validator
		nil, // provenance checker
		nil, // compliance exporter
		nil, // topology builder
		nil, // auth service
		nil, // project service
		nil, // deployment service
		nil, // deployment group service
		nil, // roundhouse client
	)

	// Admin services are set via optional setters (all nil for basic integration tests)
	// handler.SetBareMetalService(...)
	// handler.SetClusterAdminService(...)
	// handler.SetInfrastructureService(...)
	// handler.SetVClusterService(...)
	// handler.SetPlacementService(...)
	// handler.SetDriftService(...)
	// handler.SetCostTrackingService(...)

	return handler
}

func TestCreateProject(t *testing.T) {
	_ = setupTestHandler()
	t.Skip("TODO: Requires full handler dependencies - see tests/integration/")
}

func TestListProjects(t *testing.T) {
	_ = setupTestHandler()
	t.Skip("TODO: Requires full handler dependencies - see tests/integration/")
}

func TestGetProject(t *testing.T) {
	_ = setupTestHandler()
	t.Skip("TODO: Requires full handler dependencies - see tests/integration/")
}

// Admin endpoint integration test stubs

func TestListBareMetalHostsIntegration(t *testing.T) {
	_ = setupTestHandler()
	t.Skip("TODO: Requires admin service dependencies - see tests/integration/")
}

func TestRegisterClusterIntegration(t *testing.T) {
	_ = setupTestHandler()
	t.Skip("TODO: Requires admin service dependencies - see tests/integration/")
}

func TestListManagedResourcesIntegration(t *testing.T) {
	_ = setupTestHandler()
	t.Skip("TODO: Requires admin service dependencies - see tests/integration/")
}

func TestProvisionVClusterIntegration(t *testing.T) {
	_ = setupTestHandler()
	t.Skip("TODO: Requires admin service dependencies - see tests/integration/")
}

func TestCreatePropagationPolicyIntegration(t *testing.T) {
	_ = setupTestHandler()
	t.Skip("TODO: Requires admin service dependencies - see tests/integration/")
}

func TestListDriftEventsIntegration(t *testing.T) {
	_ = setupTestHandler()
	t.Skip("TODO: Requires admin service dependencies - see tests/integration/")
}

func TestGetCostSummaryIntegration(t *testing.T) {
	_ = setupTestHandler()
	t.Skip("TODO: Requires admin service dependencies - see tests/integration/")
}

// Benchmark tests also need proper setup
func BenchmarkCreateProject(b *testing.B) {
	b.Skip("TODO: Requires full handler dependencies for benchmarking")
}
