package api

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// Unit tests for pure functions and simple validation
// Handler tests that require full dependency setup are in handlers_integration_test.go

func TestHandlerSetup(t *testing.T) {
	// Verify test mode is set correctly
	if gin.Mode() != gin.TestMode {
		t.Errorf("Expected gin test mode, got %s", gin.Mode())
	}
}

func TestAdminServiceSetters(t *testing.T) {
	h := &Handler{}
	logger := logrus.New()

	// Verify setters don't panic with valid services
	h.SetBareMetalService(services.NewBareMetalService(nil, nil, logger))
	if h.bareMetalService == nil {
		t.Error("expected bareMetalService to be set")
	}

	h.SetClusterAdminService(services.NewClusterAdminService(nil, logger))
	if h.clusterAdminService == nil {
		t.Error("expected clusterAdminService to be set")
	}

	h.SetInfrastructureService(services.NewInfrastructureService(nil, nil, logger))
	if h.infrastructureService == nil {
		t.Error("expected infrastructureService to be set")
	}

	h.SetVClusterService(services.NewVClusterService(nil, nil, logger))
	if h.vclusterService == nil {
		t.Error("expected vclusterService to be set")
	}

	h.SetPlacementService(services.NewPlacementService(nil, logger))
	if h.placementService == nil {
		t.Error("expected placementService to be set")
	}

	h.SetDriftService(services.NewDriftService(nil, logger))
	if h.driftService == nil {
		t.Error("expected driftService to be set")
	}

	h.SetCostTrackingService(services.NewCostTrackingService(nil, logger))
	if h.costTrackingService == nil {
		t.Error("expected costTrackingService to be set")
	}
}

func TestAdminServiceSettersNil(t *testing.T) {
	h := &Handler{}

	// Verify setters don't panic with nil
	h.SetBareMetalService(nil)
	h.SetClusterAdminService(nil)
	h.SetInfrastructureService(nil)
	h.SetVClusterService(nil)
	h.SetPlacementService(nil)
	h.SetDriftService(nil)
	h.SetCostTrackingService(nil)
}
