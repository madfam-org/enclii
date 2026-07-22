package cmd

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func TestNewPsCommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewPsCommand(cfg)
	require.NotNil(t, cmd)

	assert.Equal(t, "ps", cmd.Use)

	// Verify flags exist with correct defaults
	envFlag := cmd.Flags().Lookup("env")
	require.NotNil(t, envFlag)
	assert.Equal(t, "dev", envFlag.DefValue)

	projectFlag := cmd.Flags().Lookup("project")
	require.NotNil(t, projectFlag)
	assert.Equal(t, "", projectFlag.DefValue)

	// Verify shorthand flags
	assert.Equal(t, "e", envFlag.Shorthand)
	assert.Equal(t, "p", projectFlag.Shorthand)
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{
			name:     "less than one minute",
			duration: 45 * time.Second,
			expected: "45s",
		},
		{
			name:     "less than one hour",
			duration: 30 * time.Minute,
			expected: "30m",
		},
		{
			name:     "less than 24 hours",
			duration: 5*time.Hour + 30*time.Minute,
			expected: "5h 30m",
		},
		{
			name:     "more than 24 hours",
			duration: 3*24*time.Hour + 12*time.Hour,
			expected: "3d 12h",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDuration(tt.duration)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetStatusColor(t *testing.T) {
	// Green for running
	assert.Equal(t, "\033[32m", getStatusColor("running"))

	// Yellow for pending
	assert.Equal(t, "\033[33m", getStatusColor("pending"))

	// Red for failed
	assert.Equal(t, "\033[31m", getStatusColor("failed"))

	// White for unknown/default
	assert.Equal(t, "\033[37m", getStatusColor("unknown"))
}

func TestGetHealthColor(t *testing.T) {
	// Green for healthy
	assert.Equal(t, "\033[32m", getHealthColor("healthy"))

	// Red for unhealthy
	assert.Equal(t, "\033[31m", getHealthColor("unhealthy"))

	// Yellow for default/unknown
	assert.Equal(t, "\033[33m", getHealthColor("unknown"))
}

func TestApplyRuntimeHealthOverridesUnknownDeploymentHealth(t *testing.T) {
	status := ServiceStatus{
		Name:     "bloom-scroll-api",
		Status:   "running",
		Health:   "unknown",
		Replicas: "1/1",
		Version:  "argocd-abc1234",
		Uptime:   "2h",
	}

	applyRuntimeHealth(&status, client.ServiceHealth{
		ServiceID: "svc-api",
		Status:    "healthy",
		PodCount:  2,
		ReadyPods: 2,
	})

	assert.Equal(t, "running", status.Status)
	assert.Equal(t, "healthy", status.Health)
	assert.Equal(t, "2/2", status.Replicas)
	assert.Equal(t, "argocd-abc1234", status.Version)
}

func TestApplyRuntimeHealthMapsStatusWhenDeploymentStatusMissing(t *testing.T) {
	status := ServiceStatus{
		Name:     "worker",
		Status:   "unknown",
		Health:   "unknown",
		Replicas: "0/0",
	}

	applyRuntimeHealth(&status, client.ServiceHealth{
		ServiceID: "svc-worker",
		Status:    "degraded",
		PodCount:  3,
		ReadyPods: 1,
	})

	assert.Equal(t, "pending", status.Status)
	assert.Equal(t, "degraded", status.Health)
	assert.Equal(t, "1/3", status.Replicas)
}
