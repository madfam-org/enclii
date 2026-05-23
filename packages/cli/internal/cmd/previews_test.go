package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func TestNewPreviewsCommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewPreviewsCommand(cfg)
	require.NotNil(t, cmd)

	assert.Equal(t, "previews", cmd.Use)
	assert.Equal(t, []string{"preview"}, cmd.Aliases)

	subNames := make([]string, len(cmd.Commands()))
	for i, sc := range cmd.Commands() {
		subNames[i] = sc.Name()
	}
	assert.Contains(t, subNames, "list")
	assert.Contains(t, subNames, "get")
	assert.Contains(t, subNames, "close")
	assert.Contains(t, subNames, "wake")
	assert.Contains(t, subNames, "delete")
}

func TestNewPreviewsListCommand(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	parent := NewPreviewsCommand(cfg)
	cmd := findSubCommand(parent, "list")
	require.NotNil(t, cmd)

	assert.Equal(t, []string{"ls"}, cmd.Aliases)

	prFlag := cmd.Flags().Lookup("pr")
	require.NotNil(t, prFlag)
	assert.Equal(t, "0", prFlag.DefValue)
}

func TestShortSHA(t *testing.T) {
	assert.Equal(t, "abc1234", shortSHA("abc1234567890"))
	assert.Equal(t, "-", shortSHA(""))
}
