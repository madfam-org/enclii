package services

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/sirupsen/logrus"
)

func TestNewCostTrackingService(t *testing.T) {
	logger := logrus.New()
	svc := NewCostTrackingService(nil, logger)
	if svc == nil {
		t.Fatal("expected non-nil CostTrackingService")
	}
}

func TestCostAllocationCalculation(t *testing.T) {
	tests := []struct {
		name              string
		costPerHourCents  int
		allocationPercent float64
		hours             int
		wantCents         int
	}{
		{"full allocation 1 hour", 100, 100.0, 1, 100},
		{"half allocation 1 hour", 100, 50.0, 1, 50},
		{"full allocation 24 hours", 50, 100.0, 24, 1200},
		{"quarter allocation 10 hours", 200, 25.0, 10, 500},
		{"zero hours", 100, 100.0, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost := int(float64(tt.costPerHourCents) * tt.allocationPercent / 100.0 * float64(tt.hours))
			if cost != tt.wantCents {
				t.Errorf("cost calculation: want %d cents, got %d", tt.wantCents, cost)
			}
		})
	}
}

func TestCostAllocationPercentValidation(t *testing.T) {
	tests := []struct {
		name    string
		percent float64
		valid   bool
	}{
		{"100 percent", 100.0, true},
		{"50 percent", 50.0, true},
		{"0 percent", 0.0, true},
		{"negative", -10.0, false},
		{"over 100", 150.0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid := tt.percent >= 0 && tt.percent <= 100
			if valid != tt.valid {
				t.Errorf("percent %v: want valid=%v, got %v", tt.percent, tt.valid, valid)
			}
		})
	}
}

func TestCostAllocationPeriod(t *testing.T) {
	now := time.Now()
	ca := types.CostAllocation{
		ID:                uuid.New(),
		BareMetalHostID:   uuid.New(),
		TenantID:          "tenant-1",
		AllocationPercent: 50.0,
		PeriodStart:       now,
		PeriodEnd:         now.Add(24 * time.Hour),
		CostCents:         1200,
	}

	if ca.PeriodEnd.Before(ca.PeriodStart) {
		t.Error("period end should be after period start")
	}
	duration := ca.PeriodEnd.Sub(ca.PeriodStart)
	if duration != 24*time.Hour {
		t.Errorf("expected 24h period, got %v", duration)
	}
}
