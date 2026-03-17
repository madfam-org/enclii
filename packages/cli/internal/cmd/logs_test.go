package cmd

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func TestNewLogsCommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewLogsCommand(cfg)
	require.NotNil(t, cmd)

	assert.Equal(t, "logs [service]", cmd.Use)

	// Verify flags exist with correct defaults
	followFlag := cmd.Flags().Lookup("follow")
	require.NotNil(t, followFlag)
	assert.Equal(t, "false", followFlag.DefValue)

	envFlag := cmd.Flags().Lookup("env")
	require.NotNil(t, envFlag)
	assert.Equal(t, "dev", envFlag.DefValue)

	linesFlag := cmd.Flags().Lookup("lines")
	require.NotNil(t, linesFlag)
	assert.Equal(t, "100", linesFlag.DefValue)

	sinceFlag := cmd.Flags().Lookup("since")
	require.NotNil(t, sinceFlag)
	assert.Equal(t, "", sinceFlag.DefValue)

	timestampsFlag := cmd.Flags().Lookup("timestamps")
	require.NotNil(t, timestampsFlag)
	assert.Equal(t, "false", timestampsFlag.DefValue)

	fileFlag := cmd.Flags().Lookup("file")
	require.NotNil(t, fileFlag)
	assert.Equal(t, "service.yaml", fileFlag.DefValue)

	// Verify shorthand flags
	assert.Equal(t, "f", followFlag.Shorthand)
	assert.Equal(t, "e", envFlag.Shorthand)
	assert.Equal(t, "n", linesFlag.Shorthand)
	assert.Equal(t, "F", fileFlag.Shorthand)
}

func TestParseSinceDuration(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectDelta time.Duration
		expectErr   bool
	}{
		{
			name:        "5 minutes",
			input:       "5m",
			expectDelta: 5 * time.Minute,
			expectErr:   false,
		},
		{
			name:        "1 hour",
			input:       "1h",
			expectDelta: 1 * time.Hour,
			expectErr:   false,
		},
		{
			name:        "24 hours",
			input:       "24h",
			expectDelta: 24 * time.Hour,
			expectErr:   false,
		},
		{
			name:      "invalid duration",
			input:     "invalid",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := time.Now()
			result, err := parseSinceDuration(tt.input)

			if tt.expectErr {
				require.Error(t, err)
				assert.Nil(t, result)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)

			// The result should be approximately (now - duration)
			expected := before.Add(-tt.expectDelta)
			assert.WithinDuration(t, expected, *result, 2*time.Second)
		})
	}
}

func TestResolveServiceName_WithArg(t *testing.T) {
	cfg := &config.Config{
		Project: "my-project",
	}

	serviceName, projectSlug, err := resolveServiceName("my-service", "service.yaml", cfg)
	require.NoError(t, err)
	assert.Equal(t, "my-service", serviceName)
	assert.Equal(t, "my-project", projectSlug)
}

func TestResolveServiceName_Empty(t *testing.T) {
	cfg := &config.Config{
		Project: "my-project",
	}

	// No service name and a spec file that does not exist
	serviceName, _, err := resolveServiceName("", "nonexistent-service.yaml", cfg)
	require.Error(t, err)
	assert.Empty(t, serviceName)
	assert.Contains(t, err.Error(), "service name required")
}
