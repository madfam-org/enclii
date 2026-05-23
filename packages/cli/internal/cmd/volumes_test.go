package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func TestNewVolumesCommand(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	cmd := NewVolumesCommand(cfg)
	require.NotNil(t, cmd)

	assert.Equal(t, "volumes", cmd.Use)
	assert.Equal(t, []string{"volume", "storage", "pvc"}, cmd.Aliases)

	subNames := make([]string, len(cmd.Commands()))
	for i, sc := range cmd.Commands() {
		subNames[i] = sc.Name()
	}
	assert.Contains(t, subNames, "list")
	assert.Contains(t, subNames, "set")
	assert.Contains(t, subNames, "add")
	assert.Contains(t, subNames, "clear")
}
