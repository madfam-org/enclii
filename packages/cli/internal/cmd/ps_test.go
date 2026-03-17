package cmd

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

	// Verify shorthand flags
	assert.Equal(t, "e", envFlag.Shorthand)
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
