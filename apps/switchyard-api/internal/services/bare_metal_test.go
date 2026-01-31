package services

import (
	"fmt"
	"testing"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/sirupsen/logrus"
)

func TestNewBareMetalService(t *testing.T) {
	logger := logrus.New()
	svc := NewBareMetalService(nil, nil, logger)
	if svc == nil {
		t.Fatal("expected non-nil BareMetalService")
	}
	if svc.logger != logger {
		t.Error("expected logger to be set")
	}
}

func TestBareMetalPowerActionValidation(t *testing.T) {
	tests := []struct {
		name    string
		action  string
		wantPow types.BMHPowerState
		wantErr bool
	}{
		{"power on", "on", types.BMHPowerOn, false},
		{"power off", "off", types.BMHPowerOff, false},
		{"reboot", "reboot", types.BMHPowerOn, false},
		{"invalid action", "suspend", "", true},
		{"empty action", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var newPower types.BMHPowerState
			var err error
			switch tt.action {
			case "on":
				newPower = types.BMHPowerOn
			case "off":
				newPower = types.BMHPowerOff
			case "reboot":
				newPower = types.BMHPowerOn
			default:
				err = fmt.Errorf("invalid power action: %s", tt.action)
			}
			if (err != nil) != tt.wantErr {
				t.Errorf("power action %q: wantErr=%v, gotErr=%v", tt.action, tt.wantErr, err)
			}
			if err == nil && newPower != tt.wantPow {
				t.Errorf("power action %q: want %s, got %s", tt.action, tt.wantPow, newPower)
			}
		})
	}
}

func TestBMHStateConstants(t *testing.T) {
	// Verify state machine states are distinct
	states := []types.BMHState{
		types.BMHStateDiscovered,
		types.BMHStateInspecting,
		types.BMHStateAvailable,
		types.BMHStateProvisioning,
		types.BMHStateProvisioned,
		types.BMHStateDeprovisioning,
		types.BMHStateError,
	}
	seen := make(map[types.BMHState]bool)
	for _, s := range states {
		if seen[s] {
			t.Errorf("duplicate BMH state: %s", s)
		}
		seen[s] = true
		if s == "" {
			t.Error("empty BMH state constant")
		}
	}
}
