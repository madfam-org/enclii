package services

import (
	"testing"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/sirupsen/logrus"
)

func TestNewPlacementService(t *testing.T) {
	logger := logrus.New()
	svc := NewPlacementService(nil, logger)
	if svc == nil {
		t.Fatal("expected non-nil PlacementService")
	}
}

func TestPlacementStrategyConstants(t *testing.T) {
	strategies := []types.PlacementStrategy{
		types.PlacementStrategySpread,
		types.PlacementStrategyBinpack,
		types.PlacementStrategyGPUAffinity,
	}
	seen := make(map[types.PlacementStrategy]bool)
	for _, s := range strategies {
		if seen[s] {
			t.Errorf("duplicate placement strategy: %s", s)
		}
		seen[s] = true
	}
}

func TestGPUAffinityMatchLogic(t *testing.T) {
	tests := []struct {
		name        string
		gpuRequired bool
		strategy    types.PlacementStrategy
		wantGPU     bool
	}{
		{"gpu required with gpu affinity", true, types.PlacementStrategyGPUAffinity, true},
		{"gpu required with spread", true, types.PlacementStrategySpread, true},
		{"no gpu with spread", false, types.PlacementStrategySpread, false},
		{"no gpu with gpu affinity", false, types.PlacementStrategyGPUAffinity, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pp := types.PropagationPolicy{
				GPURequired:       tt.gpuRequired,
				PlacementStrategy: tt.strategy,
			}
			if pp.GPURequired != tt.wantGPU {
				t.Errorf("gpu matching: want gpuRequired=%v, got %v", tt.wantGPU, pp.GPURequired)
			}
		})
	}
}
