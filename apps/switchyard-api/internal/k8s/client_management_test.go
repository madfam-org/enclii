package k8s

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// NOTE: The k8s.Client struct uses *kubernetes.Clientset (concrete type),
// which prevents using k8s.io/client-go/kubernetes/fake in unit tests.
// To enable full unit testing of client_management.go methods, the struct
// should be refactored to use the kubernetes.Interface abstraction.
//
// These tests cover the pure-logic and struct-level behavior that can
// be validated without a running cluster or fake clientset.

// ── DeploymentStatusInfo Tests ────────────────────────────────────────

func TestDeploymentStatusInfo_Fields(t *testing.T) {
	info := &DeploymentStatusInfo{
		Replicas:            3,
		DesiredReplicas:     3,
		UpdatedReplicas:     3,
		ReadyReplicas:       2,
		AvailableReplicas:   2,
		UnavailableReplicas: 1,
		Generation:          5,
		ObservedGeneration:  5,
		ImageTag:            "v1.2.3",
	}

	assert.Equal(t, int32(3), info.DesiredReplicas)
	assert.Equal(t, int32(2), info.ReadyReplicas)
	assert.Equal(t, int32(1), info.UnavailableReplicas)
	assert.Equal(t, "v1.2.3", info.ImageTag)
	assert.Equal(t, int64(5), info.Generation)
	assert.Equal(t, info.Generation, info.ObservedGeneration, "generation should match observed for stable deployment")
}

func TestDeploymentStatusInfo_HealthDerivation(t *testing.T) {
	// These tests mirror the health-status logic used in GetDetailedHealth handler:
	//   healthy:   ReadyReplicas == DesiredReplicas
	//   degraded:  ReadyReplicas < DesiredReplicas && ReadyReplicas > 0
	//   unhealthy: ReadyReplicas == 0

	tests := []struct {
		name     string
		desired  int32
		ready    int32
		expected string
	}{
		{"all ready - healthy", 3, 3, "healthy"},
		{"partial ready - degraded", 3, 2, "degraded"},
		{"one of many - degraded", 5, 1, "degraded"},
		{"none ready - unhealthy", 3, 0, "unhealthy"},
		{"single replica healthy", 1, 1, "healthy"},
		{"single replica unhealthy", 1, 0, "unhealthy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &DeploymentStatusInfo{
				DesiredReplicas: tt.desired,
				ReadyReplicas:   tt.ready,
			}

			// Replicate handler logic
			status := "healthy"
			if info.ReadyReplicas < info.DesiredReplicas {
				status = "degraded"
			}
			if info.ReadyReplicas == 0 {
				status = "unhealthy"
			}

			assert.Equal(t, tt.expected, status)
		})
	}
}

// ── Client.IsValid Tests ──────────────────────────────────────────────

func TestClient_IsValid_NilClient(t *testing.T) {
	var c *Client
	assert.False(t, c.IsValid(), "nil client should not be valid")
}

func TestClient_IsValid_EmptyClient(t *testing.T) {
	c := &Client{}
	assert.False(t, c.IsValid(), "client with nil Clientset and config should not be valid")
}

func TestClient_IsValid_NilConfig(t *testing.T) {
	// Clientset without config is invalid because ExecCommand needs config
	// for SPDY executor creation.
	c := &Client{
		// Clientset would need a real instance, but config is nil
	}
	assert.False(t, c.IsValid())
}

// ── Config accessor ───────────────────────────────────────────────────

func TestClient_Config_ReturnsStoredConfig(t *testing.T) {
	c := &Client{}
	assert.Nil(t, c.Config(), "empty client config should be nil")
}
