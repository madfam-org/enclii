package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func TestNewTokensCommand(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev", APIToken: "tok"}
	cmd := NewTokensCommand(cfg)
	require.NotNil(t, cmd)
	assert.Equal(t, "tokens", cmd.Use)
	assert.Equal(t, []string{"token"}, cmd.Aliases)
}

func TestTokensCommand_HasExpectedSubcommands(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev", APIToken: "tok"}
	cmd := NewTokensCommand(cfg)

	expected := []string{"list", "get", "create", "revoke"}
	names := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		names[sub.Name()] = true
	}
	for _, want := range expected {
		assert.True(t, names[want], "missing subcommand: %s", want)
	}
	assert.Len(t, cmd.Commands(), len(expected))
}

func TestTokensCreate_HasRequiredAndDefaults(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev", APIToken: "tok"}
	cmd := NewTokensCommand(cfg)
	createCmd := findSubcommand(cmd, "create")
	require.NotNil(t, createCmd)

	assert.NotNil(t, createCmd.Flags().Lookup("name"))
	expFlag := createCmd.Flags().Lookup("expires-in")
	require.NotNil(t, expFlag)
	assert.Equal(t, "90d", expFlag.DefValue)
	assert.NotNil(t, createCmd.Flags().Lookup("scopes"))
	assert.NotNil(t, createCmd.Flags().Lookup("json"))
}

func TestTokensRevoke_HasForceFlag(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev", APIToken: "tok"}
	cmd := NewTokensCommand(cfg)
	rev := findSubcommand(cmd, "revoke")
	require.NotNil(t, rev)
	assert.Equal(t, []string{"rm", "delete"}, rev.Aliases)
	assert.NotNil(t, rev.Flags().Lookup("force"))
}

func TestParseExpiresIn(t *testing.T) {
	cases := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"24h", 86400, false},
		{"1h", 3600, false},
		{"30d", 30 * 86400, false},
		{"90d", 90 * 86400, false},
		{"", 0, false},
		{"0d", 0, true},
		{"-1h", 0, true},
		{"banana", 0, true},
		{"5x", 0, true},
	}
	for _, c := range cases {
		got, err := parseExpiresIn(c.in)
		if c.wantErr {
			assert.Error(t, err, "input=%q", c.in)
			continue
		}
		assert.NoError(t, err, "input=%q", c.in)
		assert.Equal(t, c.want, got, "input=%q", c.in)
	}
}
