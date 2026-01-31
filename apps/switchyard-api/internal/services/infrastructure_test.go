package services

import (
	"testing"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/sirupsen/logrus"
)

func TestNewInfrastructureService(t *testing.T) {
	logger := logrus.New()
	svc := NewInfrastructureService(nil, nil, logger)
	if svc == nil {
		t.Fatal("expected non-nil InfrastructureService")
	}
}

func TestManagementPolicyValidation(t *testing.T) {
	validPolicies := map[types.ManagementPolicy]bool{
		types.ManagementPolicyFullControl:    true,
		types.ManagementPolicyObserveOnly:    true,
		types.ManagementPolicyOrphanOnDelete: true,
	}

	tests := []struct {
		name   string
		policy types.ManagementPolicy
		valid  bool
	}{
		{"FullControl", types.ManagementPolicyFullControl, true},
		{"ObserveOnly", types.ManagementPolicyObserveOnly, true},
		{"OrphanOnDelete", types.ManagementPolicyOrphanOnDelete, true},
		{"empty", "", false},
		{"invalid", "ReadOnly", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := validPolicies[tt.policy]
			if ok != tt.valid {
				t.Errorf("policy %q: want valid=%v, got %v", tt.policy, tt.valid, ok)
			}
		})
	}
}

func TestSyncStatusConstants(t *testing.T) {
	statuses := []types.SyncStatus{
		types.SyncStatusSynced,
		types.SyncStatusOutOfSync,
		types.SyncStatusUnknown,
		types.SyncStatusError,
	}
	seen := make(map[types.SyncStatus]bool)
	for _, s := range statuses {
		if seen[s] {
			t.Errorf("duplicate sync status: %s", s)
		}
		seen[s] = true
	}
}
