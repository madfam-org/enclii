package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewJWTManager(t *testing.T) {
	t.Run("creates manager successfully", func(t *testing.T) {
		manager, err := NewJWTManager(
			15*time.Minute,
			7*24*time.Hour,
			nil, // repos can be nil for basic tests
			nil, // cache can be nil
		)

		if err != nil {
			t.Fatalf("NewJWTManager() failed: %v", err)
		}

		if manager == nil {
			t.Fatal("NewJWTManager() returned nil manager")
		}

		if manager.privateKey == nil {
			t.Error("NewJWTManager() did not generate private key")
		}

		if manager.publicKey == nil {
			t.Error("NewJWTManager() did not set public key")
		}
	})

	t.Run("loads key from ENCLII_JWT_PRIVATE_KEY env", func(t *testing.T) {
		// Generate a test key in PEM format
		testKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("failed to generate test key: %v", err)
		}
		keyBytes, err := x509.MarshalPKCS8PrivateKey(testKey)
		if err != nil {
			t.Fatalf("failed to marshal key: %v", err)
		}
		pemBlock := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes})

		t.Setenv("ENCLII_JWT_PRIVATE_KEY", string(pemBlock))

		manager, err := NewJWTManager(15*time.Minute, 7*24*time.Hour, nil, nil)
		if err != nil {
			t.Fatalf("NewJWTManager() failed with env key: %v", err)
		}

		// Verify the loaded key matches by comparing public key modulus
		if manager.publicKey.N.Cmp(testKey.PublicKey.N) != 0 {
			t.Error("loaded key does not match the provided key")
		}
	})

	t.Run("sets correct token durations", func(t *testing.T) {
		tokenDur := 30 * time.Minute
		refreshDur := 14 * 24 * time.Hour

		manager, err := NewJWTManager(tokenDur, refreshDur, nil, nil)
		if err != nil {
			t.Fatalf("NewJWTManager() failed: %v", err)
		}

		if manager.tokenDuration != tokenDur {
			t.Errorf("Expected token duration %v, got %v", tokenDur, manager.tokenDuration)
		}

		if manager.refreshDuration != refreshDur {
			t.Errorf("Expected refresh duration %v, got %v", refreshDur, manager.refreshDuration)
		}
	})

	// Note: Actual repositories integration would require database setup
	// This test is commented out as it would need proper db.Repositories,
	// not testutil.MockRepositories
	/*
		t.Run("works with repositories", func(t *testing.T) {
			manager, err := NewJWTManager(
				15*time.Minute,
				7*24*time.Hour,
				repos,
				nil,
			)

			if err != nil {
				t.Fatalf("NewJWTManager() with repos failed: %v", err)
			}

			if manager == nil {
				t.Fatal("NewJWTManager() returned nil manager")
			}
		})
	*/
}

func TestJWTManager_GenerateTokenPair(t *testing.T) {
	manager, err := NewJWTManager(
		15*time.Minute,
		7*24*time.Hour,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewJWTManager() failed: %v", err)
	}

	user := &User{
		ID:    uuid.New(),
		Email: "test@example.com",
		Name:  "Test User",
		Role:  "developer",
	}

	t.Run("generates valid token pair", func(t *testing.T) {
		tokens, err := manager.GenerateTokenPair(user)

		if err != nil {
			t.Fatalf("GenerateTokenPair() failed: %v", err)
		}

		if tokens == nil {
			t.Fatal("GenerateTokenPair() returned nil")
		}

		if tokens.AccessToken == "" {
			t.Error("GenerateTokenPair() returned empty access token")
		}

		if tokens.RefreshToken == "" {
			t.Error("GenerateTokenPair() returned empty refresh token")
		}

		if tokens.ExpiresAt.IsZero() {
			t.Error("GenerateTokenPair() returned zero expiration time")
		}
	})

	t.Run("access token contains correct claims", func(t *testing.T) {
		tokens, err := manager.GenerateTokenPair(user)
		if err != nil {
			t.Fatalf("GenerateTokenPair() failed: %v", err)
		}

		claims, err := manager.ValidateToken(tokens.AccessToken)
		if err != nil {
			t.Fatalf("ValidateToken() failed: %v", err)
		}

		if claims.UserID != user.ID {
			t.Errorf("Expected UserID %v, got %v", user.ID, claims.UserID)
		}

		if claims.Email != user.Email {
			t.Errorf("Expected Email %s, got %s", user.Email, claims.Email)
		}

		if claims.Role != user.Role {
			t.Errorf("Expected Role %s, got %s", user.Role, claims.Role)
		}

		if claims.TokenType != "access" {
			t.Errorf("Expected TokenType 'access', got %s", claims.TokenType)
		}
	})
}

func TestJWTManager_ValidateToken(t *testing.T) {
	manager, err := NewJWTManager(
		15*time.Minute,
		7*24*time.Hour,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewJWTManager() failed: %v", err)
	}

	user := &User{
		ID:    uuid.New(),
		Email: "test@example.com",
		Name:  "Test User",
		Role:  "admin",
	}

	t.Run("validates valid access token", func(t *testing.T) {
		tokens, err := manager.GenerateTokenPair(user)
		if err != nil {
			t.Fatalf("GenerateTokenPair() failed: %v", err)
		}

		claims, err := manager.ValidateToken(tokens.AccessToken)
		if err != nil {
			t.Errorf("ValidateToken() failed for valid token: %v", err)
		}

		if claims == nil {
			t.Fatal("ValidateToken() returned nil claims")
		}

		if claims.UserID != user.ID {
			t.Errorf("Claims UserID mismatch: expected %v, got %v", user.ID, claims.UserID)
		}
	})

	t.Run("rejects invalid token", func(t *testing.T) {
		_, err := manager.ValidateToken("invalid.token.here")
		if err == nil {
			t.Error("ValidateToken() should reject invalid token")
		}
	})

	t.Run("rejects empty token", func(t *testing.T) {
		_, err := manager.ValidateToken("")
		if err == nil {
			t.Error("ValidateToken() should reject empty token")
		}
	})
}

func TestJWTManager_RefreshToken(t *testing.T) {
	manager, err := NewJWTManager(
		15*time.Minute,
		7*24*time.Hour,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewJWTManager() failed: %v", err)
	}

	user := &User{
		ID:    uuid.New(),
		Email: "refresh@example.com",
		Name:  "Refresh User",
		Role:  "developer",
	}

	t.Run("refreshes valid refresh token", func(t *testing.T) {
		tokens, err := manager.GenerateTokenPair(user)
		if err != nil {
			t.Fatalf("GenerateTokenPair() failed: %v", err)
		}

		newTokens, err := manager.RefreshToken(tokens.RefreshToken)
		if err != nil {
			t.Errorf("RefreshToken() failed: %v", err)
		}

		if newTokens == nil {
			t.Fatal("RefreshToken() returned nil")
		}

		if newTokens.AccessToken == "" {
			t.Error("RefreshToken() returned empty access token")
		}

		if newTokens.RefreshToken == "" {
			t.Error("RefreshToken() returned empty refresh token")
		}

		// New tokens should be different from old ones
		if newTokens.AccessToken == tokens.AccessToken {
			t.Error("RefreshToken() returned same access token")
		}
	})

	t.Run("rejects invalid refresh token", func(t *testing.T) {
		_, err := manager.RefreshToken("invalid.refresh.token")
		if err == nil {
			t.Error("RefreshToken() should reject invalid token")
		}
	})
}

func TestJWTManager_GetJWKS(t *testing.T) {
	manager, err := NewJWTManager(
		15*time.Minute,
		7*24*time.Hour,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewJWTManager() failed: %v", err)
	}

	t.Run("returns valid JWKS", func(t *testing.T) {
		jwks := manager.GetJWKS()

		if jwks == nil {
			t.Fatal("GetJWKS() returned nil")
		}

		keys, ok := jwks["keys"]
		if !ok {
			t.Fatal("GetJWKS() missing 'keys' field")
		}

		keysArray, ok := keys.([]map[string]interface{})
		if !ok {
			t.Fatal("GetJWKS() 'keys' is not an array")
		}

		if len(keysArray) == 0 {
			t.Fatal("GetJWKS() returned empty keys array")
		}

		key := keysArray[0]

		// Check required JWK fields
		if key["kty"] != "RSA" {
			t.Error("JWKS key type should be RSA")
		}

		if key["use"] != "sig" {
			t.Error("JWKS use should be 'sig'")
		}

		if key["alg"] != "RS256" {
			t.Error("JWKS algorithm should be RS256")
		}

		if _, ok := key["n"]; !ok {
			t.Error("JWKS missing modulus 'n'")
		}

		if _, ok := key["e"]; !ok {
			t.Error("JWKS missing exponent 'e'")
		}
	})
}

func TestJWTManager_ExportPublicKey(t *testing.T) {
	manager, err := NewJWTManager(
		15*time.Minute,
		7*24*time.Hour,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewJWTManager() failed: %v", err)
	}

	t.Run("exports valid PEM public key", func(t *testing.T) {
		pem, err := manager.ExportPublicKey()

		if err != nil {
			t.Fatalf("ExportPublicKey() failed: %v", err)
		}

		if pem == "" {
			t.Fatal("ExportPublicKey() returned empty string")
		}

		// Check PEM format - should contain BEGIN and END markers
		if !strings.Contains(pem, "BEGIN") || !strings.Contains(pem, "END") {
			t.Error("ExportPublicKey() does not return valid PEM format")
		}

		// Should have reasonable length (RSA keys are typically 400+ chars in PEM)
		if len(pem) < 100 {
			t.Error("ExportPublicKey() returned suspiciously short PEM")
		}
	})
}

func TestJWTManager_RevokeSession(t *testing.T) {
	manager, err := NewJWTManager(
		15*time.Minute,
		7*24*time.Hour,
		nil,
		&MockSessionRevoker{},
	)
	if err != nil {
		t.Fatalf("NewJWTManager() failed: %v", err)
	}

	ctx := context.Background()

	t.Run("revokes session successfully", func(t *testing.T) {
		sessionID := uuid.New().String()
		err := manager.RevokeSession(ctx, sessionID)

		if err != nil {
			t.Errorf("RevokeSession() failed: %v", err)
		}
	})

	t.Run("returns error without cache", func(t *testing.T) {
		managerNoCache, err := NewJWTManager(
			15*time.Minute,
			7*24*time.Hour,
			nil,
			nil, // No cache
		)
		if err != nil {
			t.Fatalf("NewJWTManager() failed: %v", err)
		}

		// Should return error when cache is not available
		err = managerNoCache.RevokeSession(ctx, "some-session-id")
		if err == nil {
			t.Error("RevokeSession() should error without cache")
		}
	})
}

// MockSessionRevoker is a mock implementation for testing
type MockSessionRevoker struct {
	revoked map[string]bool
}

func (m *MockSessionRevoker) RevokeSession(ctx context.Context, sessionID string, duration time.Duration) error {
	if m.revoked == nil {
		m.revoked = make(map[string]bool)
	}
	m.revoked[sessionID] = true
	return nil
}

func (m *MockSessionRevoker) IsSessionRevoked(ctx context.Context, sessionID string) (bool, error) {
	if m.revoked == nil {
		return false, nil
	}
	return m.revoked[sessionID], nil
}
