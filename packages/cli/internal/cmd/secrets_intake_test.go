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

	provision := findSubcommand(cmd, "provision")
	require.NotNil(t, provision)
	oidc := findSubcommand(provision, "oidc")
	require.NotNil(t, oidc)
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

func TestParseGenerateKeys(t *testing.T) {
	out, err := parseGenerateKeys("internal_api_key")
	require.NoError(t, err)
	assert.Equal(t, []string{"internal_api_key"}, out)

	out, err = parseGenerateKeys(" map_absence_feed_key , map_absence_feed_url ")
	require.NoError(t, err)
	assert.Equal(t, []string{"map_absence_feed_key", "map_absence_feed_url"}, out)

	// Unset is not an error — it just means "generate nothing".
	out, err = parseGenerateKeys("")
	require.NoError(t, err)
	assert.Nil(t, out)

	// Duplicates are rejected case-insensitively: the server would 400 anyway,
	// and failing here happens before any secret is prompted for.
	_, err = parseGenerateKeys("internal_api_key,INTERNAL_API_KEY")
	require.Error(t, err)

	// A flag that was set but names nothing is a typo, not a no-op.
	_, err = parseGenerateKeys(" , ")
	require.Error(t, err)
}

func TestSecretsIntakeSubmit_HasGenerateFlag(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev", APIToken: "tok"}
	submit := findSubcommand(findSubcommand(NewSecretsCommand(cfg), "intake"), "submit")
	require.NotNil(t, submit)

	flag := submit.Flags().Lookup("generate")
	require.NotNil(t, flag, "submit must expose --generate")
	assert.Equal(t, "", flag.DefValue)
	assert.Contains(t, submit.Long, "--generate")
}
