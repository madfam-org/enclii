package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
)

func TestNewRollbackCommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewRollbackCommand(cfg)
	require.NotNil(t, cmd)

	assert.Equal(t, "rollback [service]", cmd.Use)

	// Verify flags exist with correct defaults
	envFlag := cmd.Flags().Lookup("env")
	require.NotNil(t, envFlag)
	assert.Equal(t, "dev", envFlag.DefValue)

	toFlag := cmd.Flags().Lookup("to")
	require.NotNil(t, toFlag)
	assert.Equal(t, "", toFlag.DefValue)

	// Verify shorthand flags
	assert.Equal(t, "e", envFlag.Shorthand)
	assert.Equal(t, "t", toFlag.Shorthand)
}

func TestRollbackService_NoServiceName(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	err := rollbackService(cfg, "", "dev", "")
	require.Error(t, err)

	// Should be a ValidationError
	var validationErr *exitcodes.ValidationError
	assert.ErrorAs(t, err, &validationErr)
	assert.Contains(t, err.Error(), "service name is required")
}
