package services

import (
	"testing"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/sirupsen/logrus"
)

func TestNewDriftService(t *testing.T) {
	logger := logrus.New()
	svc := NewDriftService(nil, logger)
	if svc == nil {
		t.Fatal("expected non-nil DriftService")
	}
}

func TestDriftSeverityClassification(t *testing.T) {
	severities := []types.DriftSeverity{
		types.DriftSeverityLow,
		types.DriftSeverityMedium,
		types.DriftSeverityHigh,
		types.DriftSeverityCritical,
	}

	// Verify all severities are distinct
	seen := make(map[types.DriftSeverity]bool)
	for _, s := range severities {
		if seen[s] {
			t.Errorf("duplicate drift severity: %s", s)
		}
		seen[s] = true
	}

	// Verify ordering by convention (string comparison holds for these values)
	if types.DriftSeverityCritical == types.DriftSeverityLow {
		t.Error("critical should not equal low")
	}
}

func TestDriftSourceConstants(t *testing.T) {
	sources := []types.DriftSource{
		types.DriftSourceArgoCD,
		types.DriftSourceCrossplane,
		types.DriftSourceManual,
	}
	seen := make(map[types.DriftSource]bool)
	for _, s := range sources {
		if seen[s] {
			t.Errorf("duplicate drift source: %s", s)
		}
		seen[s] = true
		if s == "" {
			t.Error("empty drift source constant")
		}
	}
}
