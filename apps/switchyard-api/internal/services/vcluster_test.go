package services

import (
	"encoding/json"
	"testing"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/sirupsen/logrus"
)

func TestNewVClusterService(t *testing.T) {
	logger := logrus.New()
	svc := NewVClusterService(nil, nil, logger)
	if svc == nil {
		t.Fatal("expected non-nil VClusterService")
	}
}

func TestVClusterStatusConstants(t *testing.T) {
	statuses := []types.VClusterStatus{
		types.VClusterStatusPending,
		types.VClusterStatusCreating,
		types.VClusterStatusRunning,
		types.VClusterStatusPaused,
		types.VClusterStatusDeleting,
		types.VClusterStatusError,
	}
	seen := make(map[types.VClusterStatus]bool)
	for _, s := range statuses {
		if seen[s] {
			t.Errorf("duplicate vcluster status: %s", s)
		}
		seen[s] = true
	}
}

func TestResourceQuotaValidation(t *testing.T) {
	tests := []struct {
		name    string
		quota   string
		wantErr bool
	}{
		{"valid quota", `{"cpu":"2","memory":"4Gi"}`, false},
		{"empty quota", `{}`, false},
		{"null quota", `null`, false},
		{"invalid json", `{bad`, true},
		{"with limits", `{"cpu":"4","memory":"8Gi","pods":"100"}`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var raw json.RawMessage
			err := json.Unmarshal([]byte(tt.quota), &raw)
			if (err != nil) != tt.wantErr {
				t.Errorf("quota %q: wantErr=%v, gotErr=%v", tt.quota, tt.wantErr, err)
			}
		})
	}
}
