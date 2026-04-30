package cmd

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateCodeVerifier(t *testing.T) {
	verifier, err := generateCodeVerifier()
	require.NoError(t, err)
	assert.NotEmpty(t, verifier)
	// 32 bytes base64url-encoded produces 43 characters (no padding with RawURLEncoding)
	assert.Len(t, verifier, 43)

	// Verify it is valid base64url (can be decoded without error)
	decoded, err := base64.RawURLEncoding.DecodeString(verifier)
	require.NoError(t, err)
	assert.Len(t, decoded, 32)
}

func TestGenerateCodeVerifier_Unique(t *testing.T) {
	v1, err := generateCodeVerifier()
	require.NoError(t, err)

	v2, err := generateCodeVerifier()
	require.NoError(t, err)

	assert.NotEqual(t, v1, v2, "two verifier generations should produce different values")
}

func TestGenerateCodeChallenge(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	challenge := generateCodeChallenge(verifier)
	assert.NotEmpty(t, challenge)

	// Verify it is valid base64url
	decoded, err := base64.RawURLEncoding.DecodeString(challenge)
	require.NoError(t, err)
	// SHA-256 produces 32 bytes
	assert.Len(t, decoded, 32)

	// Manually compute expected challenge for the known verifier
	hash := sha256.Sum256([]byte(verifier))
	expected := base64.RawURLEncoding.EncodeToString(hash[:])
	assert.Equal(t, expected, challenge)
}

func TestGenerateCodeChallenge_Deterministic(t *testing.T) {
	verifier := "test-verifier-value-12345"
	c1 := generateCodeChallenge(verifier)
	c2 := generateCodeChallenge(verifier)
	assert.Equal(t, c1, c2, "same verifier should always produce the same challenge")
}

func TestGenerateState(t *testing.T) {
	state, err := generateState()
	require.NoError(t, err)
	assert.NotEmpty(t, state)
	// 16 bytes base64url-encoded produces 22 characters (no padding with RawURLEncoding)
	assert.Len(t, state, 22)

	// Verify it is valid base64url
	decoded, err := base64.RawURLEncoding.DecodeString(state)
	require.NoError(t, err)
	assert.Len(t, decoded, 16)
}

func TestGenerateState_Unique(t *testing.T) {
	s1, err := generateState()
	require.NoError(t, err)

	s2, err := generateState()
	require.NoError(t, err)

	assert.NotEqual(t, s1, s2, "two state generations should produce different values")
}

func TestBuildAuthURL(t *testing.T) {
	issuer := "https://auth.example.com"
	redirectURI := "http://127.0.0.1:8080/callback"
	state := "random-state-value"
	codeChallenge := "challenge-value"
	clientID := "test-client-id"

	result := buildAuthURL(issuer, redirectURI, state, codeChallenge, clientID)

	// Should start with issuer + authorizePath
	assert.Contains(t, result, issuer+authorizePath)

	// Should contain all required OAuth params
	assert.Contains(t, result, "client_id="+clientID)
	assert.Contains(t, result, "response_type=code")
	assert.Contains(t, result, "code_challenge="+codeChallenge)
	assert.Contains(t, result, "code_challenge_method=S256")
	assert.Contains(t, result, "state="+state)
	assert.Contains(t, result, "scope=")

	// Verify the scope value contains expected scopes
	assert.Contains(t, result, "openid")
}

func TestBuildAuthURL_EncodesParams(t *testing.T) {
	issuer := "https://auth.example.com"
	redirectURI := "http://127.0.0.1:8080/callback?extra=value"
	state := "state with spaces"
	codeChallenge := "challenge+special"
	clientID := "client/id"

	result := buildAuthURL(issuer, redirectURI, state, codeChallenge, clientID)

	// url.Values.Encode() should handle URL encoding
	// Spaces become + in query string encoding
	assert.NotContains(t, result, " ", "spaces should be encoded")
	// The redirect_uri should be URL-encoded
	assert.Contains(t, result, "redirect_uri=")
}

func TestExchangeCodeForTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, tokenPath, r.URL.Path)

		err := r.ParseForm()
		require.NoError(t, err)

		// Verify form data
		assert.Equal(t, "authorization_code", r.FormValue("grant_type"))
		assert.Equal(t, "test-client", r.FormValue("client_id"))
		assert.Equal(t, "auth-code-123", r.FormValue("code"))
		assert.Equal(t, "http://127.0.0.1:8080/callback", r.FormValue("redirect_uri"))
		assert.Equal(t, "test-verifier", r.FormValue("code_verifier"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(TokenResponse{
			AccessToken:  "access-token-value",
			RefreshToken: "refresh-token-value",
			TokenType:    "Bearer",
			ExpiresIn:    3600,
			IDToken:      "id-token-value",
		})
	}))
	defer server.Close()

	tokens, err := exchangeCodeForTokens(
		server.URL,
		"auth-code-123",
		"http://127.0.0.1:8080/callback",
		"test-verifier",
		"test-client",
	)

	require.NoError(t, err)
	require.NotNil(t, tokens)
	assert.Equal(t, "access-token-value", tokens.AccessToken)
	assert.Equal(t, "refresh-token-value", tokens.RefreshToken)
	assert.Equal(t, "Bearer", tokens.TokenType)
	assert.Equal(t, 3600, tokens.ExpiresIn)
	assert.Equal(t, "id-token-value", tokens.IDToken)
}

func TestExchangeCodeForTokens_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(TokenResponse{
			Error:     "invalid_grant",
			ErrorDesc: "Authorization code has expired",
		})
	}))
	defer server.Close()

	tokens, err := exchangeCodeForTokens(
		server.URL,
		"expired-code",
		"http://127.0.0.1:8080/callback",
		"test-verifier",
		"test-client",
	)

	require.Error(t, err)
	assert.Nil(t, tokens)
	assert.Contains(t, err.Error(), "invalid_grant")
	assert.Contains(t, err.Error(), "Authorization code has expired")
}

func TestExchangeCodeForTokens_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	tokens, err := exchangeCodeForTokens(
		server.URL,
		"code",
		"http://127.0.0.1:8080/callback",
		"verifier",
		"client",
	)

	// The function tries to JSON decode the body, which will fail on plain text
	require.Error(t, err)
	assert.Nil(t, tokens)
}

func TestSaveAndLoadCredentials(t *testing.T) {
	// Use a temp directory as HOME to avoid touching real credentials
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	t.Cleanup(func() {
		os.Setenv("HOME", origHome)
	})

	now := time.Now().Truncate(time.Second) // Truncate for JSON round-trip precision

	creds := &Credentials{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		TokenType:    "Bearer",
		ExpiresAt:    now.Add(1 * time.Hour),
		Issuer:       "https://auth.example.com",
	}

	// Save credentials
	err := saveCredentials(creds)
	require.NoError(t, err)

	// Verify file was created with correct permissions
	credsPath := filepath.Join(tmpDir, ".enclii", "credentials.json")
	info, err := os.Stat(credsPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())

	// Load credentials back
	loaded, err := LoadCredentials()
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, creds.AccessToken, loaded.AccessToken)
	assert.Equal(t, creds.RefreshToken, loaded.RefreshToken)
	assert.Equal(t, creds.TokenType, loaded.TokenType)
	assert.Equal(t, creds.Issuer, loaded.Issuer)
	// Time comparison with tolerance for JSON marshaling
	assert.WithinDuration(t, creds.ExpiresAt, loaded.ExpiresAt, time.Second)
}

func TestLoadCredentials_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	t.Cleanup(func() {
		os.Setenv("HOME", origHome)
	})

	loaded, err := LoadCredentials()
	require.Error(t, err)
	assert.Nil(t, loaded)
	assert.True(t, os.IsNotExist(err))
}

func TestDecodeJWTClaims(t *testing.T) {
	// Build a valid JWT-shaped token (header.payload.signature)
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"email":"test@example.com","name":"Test User","sub":"user-123"}`))
	signature := base64.RawURLEncoding.EncodeToString([]byte("fake-signature"))

	token := header + "." + payload + "." + signature

	claims, err := decodeJWTClaims(token)
	require.NoError(t, err)
	require.NotNil(t, claims)

	assert.Equal(t, "test@example.com", claims["email"])
	assert.Equal(t, "Test User", claims["name"])
	assert.Equal(t, "user-123", claims["sub"])
}

func TestDecodeJWTClaims_InvalidFormat(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{"no dots", "justastringwithoutdots"},
		{"one dot", "part1.part2"},
		{"four parts", "a.b.c.d"},
		{"empty string", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims, err := decodeJWTClaims(tt.token)
			require.Error(t, err)
			assert.Nil(t, claims)
			assert.Contains(t, err.Error(), "invalid JWT format")
		})
	}
}

func TestDecodeJWTClaims_InvalidBase64(t *testing.T) {
	// Valid structure but second part is not valid base64
	token := "eyJhbGciOiJSUzI1NiJ9.!!!invalid-base64!!!.signature"

	claims, err := decodeJWTClaims(token)
	require.Error(t, err)
	assert.Nil(t, claims)
}

func TestGetCredentialsPath(t *testing.T) {
	path := getCredentialsPath()
	assert.NotEmpty(t, path)
	assert.Contains(t, path, ".enclii")
	assert.Contains(t, path, "credentials.json")
	assert.True(t, filepath.IsAbs(path), "credentials path should be absolute")
}

func TestGetDefaultIssuer(t *testing.T) {
	// Test default value when env var is not set
	origVal := os.Getenv("ENCLII_OIDC_ISSUER")
	os.Unsetenv("ENCLII_OIDC_ISSUER")
	t.Cleanup(func() {
		if origVal != "" {
			os.Setenv("ENCLII_OIDC_ISSUER", origVal)
		}
	})

	issuer := getDefaultIssuer()
	assert.Equal(t, "https://auth.madfam.io", issuer)

	// Test with custom env var
	os.Setenv("ENCLII_OIDC_ISSUER", "https://auth.custom.dev")
	issuer = getDefaultIssuer()
	assert.Equal(t, "https://auth.custom.dev", issuer)
}
