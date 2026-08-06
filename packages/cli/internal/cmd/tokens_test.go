package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestDecodeTokenList_BareArray(t *testing.T) {
	// The Switchyard API returns a bare array from GET /v1/user/tokens
	// (c.JSON(http.StatusOK, tokens) in ListAPITokens). This shape used to
	// crash the CLI with "cannot unmarshal array".
	raw := []byte(`[{"id":"11111111-1111-1111-1111-111111111111","name":"ci","created_at":"2026-07-01T00:00:00Z"}]`)

	tokens, err := decodeTokenList(raw)

	require.NoError(t, err)
	require.Len(t, tokens, 1)
	assert.Equal(t, "ci", tokens[0].Name)
}

func TestDecodeTokenList_WrappedObject(t *testing.T) {
	raw := []byte(`{"tokens":[{"id":"1","name":"a","created_at":"2026-07-01T00:00:00Z"},{"id":"2","name":"b","created_at":"2026-07-02T00:00:00Z"}]}`)

	tokens, err := decodeTokenList(raw)

	require.NoError(t, err)
	require.Len(t, tokens, 2)
	assert.Equal(t, "a", tokens[0].Name)
	assert.Equal(t, "b", tokens[1].Name)
}

func TestDecodeTokenList_EmptyVariants(t *testing.T) {
	for _, raw := range []string{"", "null", "[]", `{"tokens":[]}`, "  \n"} {
		tokens, err := decodeTokenList([]byte(raw))
		require.NoError(t, err, "input=%q", raw)
		assert.Empty(t, tokens, "input=%q", raw)
	}
}

func TestDecodeTokenList_InvalidJSON(t *testing.T) {
	_, err := decodeTokenList([]byte(`{"tokens": "not-an-array"}`))
	assert.Error(t, err)

	_, err = decodeTokenList([]byte(`[{"id":`))
	assert.Error(t, err)
}

func TestTokensList_AgainstBareArrayServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/user/tokens", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"11111111-1111-1111-1111-111111111111","name":"ci-deploy","prefix":"enc_ab12","created_at":"2026-07-01T12:00:00Z"}]`))
	}))
	defer srv.Close()

	cfg := &config.Config{APIEndpoint: srv.URL, APIToken: "tok"}
	cmd := NewTokensCommand(cfg)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"list"})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "ci-deploy")
}

func TestExpiresInDays(t *testing.T) {
	cases := []struct {
		seconds int64
		want    int64
	}{
		{86400, 1},       // 24h
		{3600, 1},        // 1h rounds up, never silently 0 (= no expiry)
		{90 * 86400, 90}, // 90d
		{86401, 2},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, expiresInDays(c.seconds), "seconds=%d", c.seconds)
	}
}

func TestTokensCreate_SendsExpiresInDays(t *testing.T) {
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/user/tokens", r.URL.Path)
		require.Equal(t, http.MethodPost, r.Method)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"11111111-1111-1111-1111-111111111111","name":"ci","created_at":"2026-07-01T00:00:00Z","token":"enc_secret"}`))
	}))
	defer srv.Close()

	cfg := &config.Config{APIEndpoint: srv.URL, APIToken: "tok"}
	cmd := NewTokensCommand(cfg)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"create", "--name", "ci", "--expires-in", "90d"})

	require.NoError(t, cmd.Execute())
	// The server binds expires_in_days; expires_in_seconds was silently
	// dropped and produced never-expiring tokens.
	assert.Equal(t, float64(90), gotBody["expires_in_days"])
	_, hasLegacy := gotBody["expires_in_seconds"]
	assert.False(t, hasLegacy)
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
