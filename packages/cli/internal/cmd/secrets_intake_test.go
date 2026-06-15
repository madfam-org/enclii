package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func TestNewSecretsCommand_IncludesIntake(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev", APIToken: "tok"}
	cmd := NewSecretsCommand(cfg)
	intake := findSubcommand(cmd, "intake")
	require.NotNil(t, intake)

	names := make([]string, 0, len(intake.Commands()))
	for _, sub := range intake.Commands() {
		names = append(names, sub.Name())
	}
	assert.Contains(t, names, "targets")
	assert.Contains(t, names, "submit")
	assert.Contains(t, names, "status")
}

func TestParseKeyValueLines(t *testing.T) {
	out, err := parseKeyValueLines([]string{
		"# comment",
		"",
		"VAST_API_KEY=abc123",
		" OTHER = spaced ",
	})
	require.NoError(t, err)
	assert.Equal(t, "abc123", out["VAST_API_KEY"])
	assert.Equal(t, "spaced", out["OTHER"])

	_, err = parseKeyValueLines([]string{"not-a-pair"})
	require.Error(t, err)

	_, err = parseKeyValueLines([]string{"", "# only comments"})
	require.Error(t, err)
}
