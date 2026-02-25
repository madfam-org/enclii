package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/sirupsen/logrus"
)

// APITokenValidator interface for validating API tokens
// This avoids circular dependency with the db package
type APITokenValidator interface {
	ValidateTokenForAuth(ctx context.Context, rawToken string) (*db.APITokenInfo, error)
	UpdateLastUsed(ctx context.Context, id uuid.UUID, ip string) error
}

type JWTManager struct {
	privateKey      *rsa.PrivateKey
	publicKey       *rsa.PublicKey
	tokenDuration   time.Duration
	refreshDuration time.Duration
	repos           *db.Repositories
	cache           SessionRevoker // For session revocation

	// External JWKS validation (for CLI/API direct access with external tokens)
	externalJWKSURL   string
	externalIssuer    string
	externalAudience  string
	externalJWKSCache *jwksCache

	// Admin email mapping for external tokens (grants admin role based on email)
	adminEmails map[string]bool

	// API Token validation (for CLI/CI/CD access)
	apiTokenValidator APITokenValidator
}

// jwksCache caches external JWKS keys with TTL and stale-while-revalidate support
type jwksCache struct {
	mu               sync.RWMutex
	keys             map[string]*rsa.PublicKey // kid -> public key
	expiresAt        time.Time
	cacheTTL         time.Duration
	lastFetchTime    time.Time
	lastFetchError   error
	consecutiveFails int
	staleThreshold   time.Duration // Warn if cache is stale beyond this
}

// SessionRevoker defines the interface for revoking sessions
// This is typically implemented using Redis/cache for fast lookups
type SessionRevoker interface {
	RevokeSession(ctx context.Context, sessionID string, ttl time.Duration) error
	IsSessionRevoked(ctx context.Context, sessionID string) (bool, error)
}

type Claims struct {
	UserID     uuid.UUID `json:"user_id"`
	Email      string    `json:"email"`
	Role       string    `json:"role"`
	ProjectIDs []string  `json:"project_ids,omitempty"`
	SessionID  string    `json:"session_id"` // Unique session identifier for revocation
	TokenType  string    `json:"token_type"` // "access" or "refresh"
	jwt.RegisteredClaims
}

type TokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	TokenType    string    `json:"token_type"`
	// IDPToken is the access token from the identity provider (e.g., Janua)
	// Used for calling IDP-specific APIs like OAuth account linking
	IDPToken string `json:"idp_token,omitempty"`
	// IDPTokenExpiresAt is when the IDP token expires
	IDPTokenExpiresAt *time.Time `json:"idp_token_expires_at,omitempty"`
}

type User struct {
	ID         uuid.UUID `json:"id"`
	Email      string    `json:"email"`
	Name       string    `json:"name"`
	Role       string    `json:"role"`
	ProjectIDs []string  `json:"project_ids"`
	CreatedAt  time.Time `json:"created_at"`
	Active     bool      `json:"active"`
}

func NewJWTManager(tokenDuration, refreshDuration time.Duration, repos *db.Repositories, cache SessionRevoker) (*JWTManager, error) {
	privateKey, err := loadOrGenerateRSAKey()
	if err != nil {
		return nil, fmt.Errorf("failed to load/generate RSA key: %w", err)
	}

	return &JWTManager{
		privateKey:      privateKey,
		publicKey:       &privateKey.PublicKey,
		tokenDuration:   tokenDuration,
		refreshDuration: refreshDuration,
		repos:           repos,
		cache:           cache,
	}, nil
}

// NewJWTManagerWithExternalJWKS creates a JWT manager with external JWKS validation support
func NewJWTManagerWithExternalJWKS(
	tokenDuration, refreshDuration time.Duration,
	repos *db.Repositories,
	cache SessionRevoker,
	externalJWKSURL string,
	externalIssuer string,
	jwksCacheTTL time.Duration,
) (*JWTManager, error) {
	manager, err := NewJWTManager(tokenDuration, refreshDuration, repos, cache)
	if err != nil {
		return nil, err
	}

	if externalJWKSURL != "" {
		manager.externalJWKSURL = externalJWKSURL
		manager.externalIssuer = externalIssuer
		manager.externalAudience = os.Getenv("ENCLII_JANUA_AUDIENCE")
		if manager.externalAudience == "" {
			manager.externalAudience = "enclii-api"
		}
		manager.externalJWKSCache = &jwksCache{
			keys:           make(map[string]*rsa.PublicKey),
			cacheTTL:       jwksCacheTTL,
			staleThreshold: time.Hour, // Warn if cache is stale for more than 1 hour
		}
		logrus.WithFields(logrus.Fields{
			"jwks_url": externalJWKSURL,
			"issuer":   externalIssuer,
			"audience": manager.externalAudience,
		}).Info("External JWKS validation enabled")
	}

	// Load admin emails from environment variable (comma-separated)
	// This grants admin role to users with these emails from external tokens
	manager.adminEmails = make(map[string]bool)
	if adminEmailsEnv := os.Getenv("ENCLII_ADMIN_EMAILS"); adminEmailsEnv != "" {
		for _, email := range strings.Split(adminEmailsEnv, ",") {
			email = strings.TrimSpace(email)
			if email != "" {
				manager.adminEmails[email] = true
				logrus.WithField("email", email).Info("Registered admin email for external tokens")
			}
		}
	}

	return manager, nil
}

// loadOrGenerateRSAKey loads an RSA private key from ENCLII_JWT_PRIVATE_KEY env var,
// or generates a new ephemeral one if not set. A persistent key is REQUIRED for
// multi-replica deployments so all pods validate tokens with the same key.
func loadOrGenerateRSAKey() (*rsa.PrivateKey, error) {
	if keyPEM := os.Getenv("ENCLII_JWT_PRIVATE_KEY"); keyPEM != "" {
		block, _ := pem.Decode([]byte(keyPEM))
		if block == nil {
			return nil, fmt.Errorf("ENCLII_JWT_PRIVATE_KEY: failed to decode PEM block")
		}

		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			// Try PKCS1 format as fallback
			rsaKey, err2 := x509.ParsePKCS1PrivateKey(block.Bytes)
			if err2 != nil {
				return nil, fmt.Errorf("ENCLII_JWT_PRIVATE_KEY: failed to parse key (PKCS8: %v, PKCS1: %v)", err, err2)
			}
			logrus.Info("JWT signing key loaded from ENCLII_JWT_PRIVATE_KEY (PKCS1)")
			return rsaKey, nil
		}

		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("ENCLII_JWT_PRIVATE_KEY: key is not RSA")
		}
		logrus.Info("JWT signing key loaded from ENCLII_JWT_PRIVATE_KEY (PKCS8)")
		return rsaKey, nil
	}

	// In production, ephemeral keys cause cross-replica 401s
	if env := os.Getenv("ENCLII_ENV"); env == "production" {
		return nil, fmt.Errorf("ENCLII_JWT_PRIVATE_KEY is required in production (env=%s); "+
			"run scripts/generate-jwt-secrets.sh to create the shared signing key", env)
	}

	logrus.Warn("ENCLII_JWT_PRIVATE_KEY not set — generating ephemeral RSA key (tokens will NOT survive pod restarts or work across replicas)")
	return rsa.GenerateKey(rand.Reader, 2048)
}

func (j *JWTManager) GenerateTokenPair(user *User) (*TokenPair, error) {
	now := time.Now()

	// Generate unique session ID for this token pair
	// This allows us to revoke both access and refresh tokens together
	sessionID := uuid.New().String()

	// Generate access token
	accessClaims := &Claims{
		UserID:     user.ID,
		Email:      user.Email,
		Role:       user.Role,
		ProjectIDs: user.ProjectIDs,
		SessionID:  sessionID,
		TokenType:  "access",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(j.tokenDuration)),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    "enclii-switchyard",
		},
	}

	accessToken := jwt.NewWithClaims(jwt.SigningMethodRS256, accessClaims)
	accessTokenString, err := accessToken.SignedString(j.privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign access token: %w", err)
	}

	// Generate refresh token with same session ID
	refreshClaims := &Claims{
		UserID:    user.ID,
		Email:     user.Email,
		Role:      user.Role,
		SessionID: sessionID,
		TokenType: "refresh",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(j.refreshDuration)),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    "enclii-switchyard",
		},
	}

	refreshToken := jwt.NewWithClaims(jwt.SigningMethodRS256, refreshClaims)
	refreshTokenString, err := refreshToken.SignedString(j.privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessTokenString,
		RefreshToken: refreshTokenString,
		ExpiresAt:    now.Add(j.tokenDuration),
		TokenType:    "Bearer",
	}, nil
}

func (j *JWTManager) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return j.publicKey, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	if !token.Valid {
		return nil, fmt.Errorf("token is not valid")
	}

	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, fmt.Errorf("failed to parse claims")
	}

	// Additional validation
	if claims.TokenType != "access" {
		return nil, fmt.Errorf("invalid token type")
	}

	// SECURITY: Check if session has been revoked (logout, security event, etc.)
	// Fail-open: if we can't verify session status (Redis unavailable), allow access
	// for availability. This prioritizes user experience over strict revocation checking
	// when Redis connectivity is intermittent. Explicit logout still works when Redis is up.
	// See: Investigation - app.enclii.dev Authentication Session Loss (Jan 2026)
	if j.cache != nil && claims.SessionID != "" {
		revoked, err := j.cache.IsSessionRevoked(context.Background(), claims.SessionID)
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"session_id": claims.SessionID,
				"user_id":    claims.UserID,
				"error":      err.Error(),
			}).Warn("Failed to check session revocation - allowing access (fail-open for availability)")
			// Continue without blocking - prioritize availability over strict revocation
			revoked = false
		}
		if revoked {
			return nil, fmt.Errorf("session has been revoked")
		}
	}

	return claims, nil
}

func (j *JWTManager) RefreshToken(refreshTokenString string) (*TokenPair, error) {
	claims, err := j.validateRefreshToken(refreshTokenString)
	if err != nil {
		// Audit: Log refresh failure
		LogTokenRefreshFailed(err.Error(), "")
		return nil, fmt.Errorf("invalid refresh token: %w", err)
	}

	// Revoke old session (token rotation for security)
	if j.cache != nil && claims.SessionID != "" {
		if err := j.cache.RevokeSession(context.Background(), claims.SessionID, j.refreshDuration); err != nil {
			logrus.WithError(err).Warn("Failed to revoke old session during token refresh")
			// Continue anyway - new tokens will be valid
		}
	}

	// Create user from claims
	user := &User{
		ID:         claims.UserID,
		Email:      claims.Email,
		Role:       claims.Role,
		ProjectIDs: claims.ProjectIDs,
		Active:     true,
	}

	newTokens, err := j.GenerateTokenPair(user)
	if err != nil {
		return nil, err
	}

	// Audit: Log successful token refresh
	LogTokenRefreshed(claims.UserID, claims.SessionID)

	return newTokens, nil
}

func (j *JWTManager) validateRefreshToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return j.publicKey, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	if !token.Valid {
		return nil, fmt.Errorf("token is not valid")
	}

	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, fmt.Errorf("failed to parse claims")
	}

	if claims.TokenType != "refresh" {
		return nil, fmt.Errorf("invalid token type")
	}

	return claims, nil
}

// RevokeSession revokes a session by session ID
// The TTL should match the longest-lived token in the session (typically refresh token duration)
func (j *JWTManager) RevokeSession(ctx context.Context, sessionID string) error {
	if j.cache == nil {
		return fmt.Errorf("session revocation not available: cache not configured")
	}

	// Revoke for the duration of the refresh token (longest-lived)
	err := j.cache.RevokeSession(ctx, sessionID, j.refreshDuration)
	if err != nil {
		return fmt.Errorf("failed to revoke session: %w", err)
	}

	logrus.Infof("Session revoked: %s", sessionID)
	return nil
}

// RevokeSessionFromToken extracts the session ID from a token and revokes it
func (j *JWTManager) RevokeSessionFromToken(ctx context.Context, tokenString string) error {
	// Parse token without full validation (we just need the session ID)
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return j.publicKey, nil
	})

	if err != nil {
		return fmt.Errorf("failed to parse token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || claims.SessionID == "" {
		return fmt.Errorf("invalid token or missing session ID")
	}

	return j.RevokeSession(ctx, claims.SessionID)
}

// ExportPublicKey exports the public key for verification by other services
func (j *JWTManager) ExportPublicKey() (string, error) {
	pubKeyBytes, err := x509.MarshalPKIXPublicKey(j.publicKey)
	if err != nil {
		return "", fmt.Errorf("failed to marshal public key: %w", err)
	}

	pubKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: pubKeyBytes,
	})

	return string(pubKeyPEM), nil
}

// GetJWKS returns the JSON Web Key Set for token verification
// This allows external services to verify tokens we issue
func (j *JWTManager) GetJWKS() map[string]interface{} {
	// Convert RSA public key to JWK format
	// The public key components: n (modulus) and e (exponent)
	n := base64.RawURLEncoding.EncodeToString(j.publicKey.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(j.publicKey.E)).Bytes())

	return map[string]interface{}{
		"keys": []map[string]interface{}{
			{
				"kty": "RSA",
				"use": "sig",
				"alg": "RS256",
				"kid": "enclii-jwt-key-1",
				"n":   n, // RSA modulus
				"e":   e, // RSA public exponent
			},
		},
	}
}
