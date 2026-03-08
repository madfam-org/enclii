package auth

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sirupsen/logrus"
)

// ExternalClaims represents claims from external JWT tokens (e.g., Janua)
type ExternalClaims struct {
	Email       string `json:"email"`
	Name        string `json:"name"`
	TenantID    string `json:"tenant_id,omitempty"`
	FoundryTier string `json:"foundry_tier,omitempty"`
	jwt.RegisteredClaims
}

// ValidateExternalToken validates a token against the external JWKS (e.g., Janua)
// Returns the claims if valid, nil otherwise
func (j *JWTManager) ValidateExternalToken(tokenString string) (*ExternalClaims, error) {
	if j.externalJWKSCache == nil || j.externalJWKSURL == "" {
		return nil, fmt.Errorf("external JWKS validation not configured")
	}

	// Parse token header to get kid
	token, _, err := new(jwt.Parser).ParseUnverified(tokenString, &ExternalClaims{})
	if err != nil {
		return nil, fmt.Errorf("failed to parse token header: %w", err)
	}

	kid, ok := token.Header["kid"].(string)
	if !ok {
		kid = "" // Some providers don't use kid
	}

	// Get public key from cache or fetch
	publicKey, err := j.getExternalPublicKey(kid)
	if err != nil {
		return nil, fmt.Errorf("failed to get external public key: %w", err)
	}

	// Parse and validate token with external key
	parsedToken, err := jwt.ParseWithClaims(tokenString, &ExternalClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return publicKey, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to validate token: %w", err)
	}

	if !parsedToken.Valid {
		return nil, fmt.Errorf("token is not valid")
	}

	claims, ok := parsedToken.Claims.(*ExternalClaims)
	if !ok {
		return nil, fmt.Errorf("failed to parse claims")
	}

	// Verify issuer if configured
	if j.externalIssuer != "" && claims.Issuer != j.externalIssuer {
		return nil, fmt.Errorf("invalid issuer: expected %s, got %s", j.externalIssuer, claims.Issuer)
	}

	// Verify audience if configured (per-client token isolation)
	if j.externalAudience != "" {
		audValid := false
		for _, aud := range claims.Audience {
			if aud == j.externalAudience {
				audValid = true
				break
			}
		}
		if !audValid {
			return nil, fmt.Errorf("invalid audience: expected %s, got %v", j.externalAudience, claims.Audience)
		}
	}

	logrus.WithFields(logrus.Fields{
		"email":  claims.Email,
		"issuer": claims.Issuer,
		"sub":    claims.Subject,
	}).Debug("External token validated successfully")

	return claims, nil
}

// getExternalPublicKey retrieves the public key for the given kid from cache or fetches from JWKS
func (j *JWTManager) getExternalPublicKey(kid string) (*rsa.PublicKey, error) {
	j.externalJWKSCache.mu.RLock()
	if time.Now().Before(j.externalJWKSCache.expiresAt) {
		if key, ok := j.externalJWKSCache.keys[kid]; ok {
			j.externalJWKSCache.mu.RUnlock()
			return key, nil
		}
		// If no kid specified, try first key
		if kid == "" && len(j.externalJWKSCache.keys) > 0 {
			for _, key := range j.externalJWKSCache.keys {
				j.externalJWKSCache.mu.RUnlock()
				return key, nil
			}
		}
	}
	j.externalJWKSCache.mu.RUnlock()

	// Fetch fresh JWKS
	if err := j.refreshExternalJWKS(); err != nil {
		return nil, err
	}

	j.externalJWKSCache.mu.RLock()
	defer j.externalJWKSCache.mu.RUnlock()

	if key, ok := j.externalJWKSCache.keys[kid]; ok {
		return key, nil
	}
	// If no kid specified or not found, try first key
	if len(j.externalJWKSCache.keys) > 0 {
		for _, key := range j.externalJWKSCache.keys {
			return key, nil
		}
	}

	return nil, fmt.Errorf("key not found in JWKS: %s", kid)
}

// refreshExternalJWKS fetches the JWKS from the external provider with graceful failure handling
// Implements stale-while-revalidate: uses cached keys if fetch fails
func (j *JWTManager) refreshExternalJWKS() error {
	j.externalJWKSCache.mu.Lock()
	defer j.externalJWKSCache.mu.Unlock()

	logrus.WithField("url", j.externalJWKSURL).Debug("Fetching external JWKS")

	// Create HTTP client with timeout
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(j.externalJWKSURL)
	if err != nil {
		return j.handleJWKSFetchError(fmt.Errorf("failed to fetch JWKS: %w", err))
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return j.handleJWKSFetchError(fmt.Errorf("JWKS endpoint returned status %d", resp.StatusCode))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return j.handleJWKSFetchError(fmt.Errorf("failed to read JWKS response: %w", err))
	}

	var jwks struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			Use string `json:"use"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}

	if err := json.Unmarshal(body, &jwks); err != nil {
		return j.handleJWKSFetchError(fmt.Errorf("failed to parse JWKS: %w", err))
	}

	// Parse each key
	newKeys := make(map[string]*rsa.PublicKey)
	for _, key := range jwks.Keys {
		if key.Kty != "RSA" {
			continue
		}

		// Decode n and e
		nBytes, err := base64.RawURLEncoding.DecodeString(key.N)
		if err != nil {
			logrus.WithError(err).WithField("kid", key.Kid).Warn("Failed to decode key modulus")
			continue
		}

		eBytes, err := base64.RawURLEncoding.DecodeString(key.E)
		if err != nil {
			logrus.WithError(err).WithField("kid", key.Kid).Warn("Failed to decode key exponent")
			continue
		}

		// Convert to big.Int
		n := new(big.Int).SetBytes(nBytes)
		e := 0
		for _, b := range eBytes {
			e = e<<8 + int(b)
		}

		pubKey := &rsa.PublicKey{N: n, E: e}
		newKeys[key.Kid] = pubKey

		logrus.WithField("kid", key.Kid).Debug("Loaded external JWKS key")
	}

	if len(newKeys) == 0 {
		return j.handleJWKSFetchError(fmt.Errorf("no valid RSA keys found in JWKS"))
	}

	// Success - update cache and reset failure counters
	j.externalJWKSCache.keys = newKeys
	j.externalJWKSCache.expiresAt = time.Now().Add(j.externalJWKSCache.cacheTTL)
	j.externalJWKSCache.lastFetchTime = time.Now()
	j.externalJWKSCache.lastFetchError = nil
	j.externalJWKSCache.consecutiveFails = 0

	logrus.WithField("key_count", len(newKeys)).Info("External JWKS cache refreshed")

	return nil
}

// handleJWKSFetchError handles JWKS fetch failures with stale-while-revalidate semantics
// If we have cached keys, continue using them and log a warning
// Must be called with mutex held
func (j *JWTManager) handleJWKSFetchError(err error) error {
	j.externalJWKSCache.consecutiveFails++
	j.externalJWKSCache.lastFetchError = err

	// Check if we have cached keys to fall back to
	if len(j.externalJWKSCache.keys) > 0 {
		cacheAge := time.Since(j.externalJWKSCache.lastFetchTime)

		// Log warning about stale cache
		logrus.WithFields(logrus.Fields{
			"error":             err.Error(),
			"consecutive_fails": j.externalJWKSCache.consecutiveFails,
			"cache_age":         cacheAge.String(),
			"cached_keys":       len(j.externalJWKSCache.keys),
		}).Warn("JWKS fetch failed, using cached keys (stale-while-revalidate)")

		// Alert if cache is stale beyond threshold
		if cacheAge > j.externalJWKSCache.staleThreshold {
			logrus.WithFields(logrus.Fields{
				"cache_age":         cacheAge.String(),
				"stale_threshold":   j.externalJWKSCache.staleThreshold.String(),
				"consecutive_fails": j.externalJWKSCache.consecutiveFails,
			}).Error("CRITICAL: JWKS cache is stale beyond threshold - authentication may fail if keys rotate")
		}

		// Return nil to indicate we can continue with cached keys
		return nil
	}

	// No cached keys available - this is a hard failure
	logrus.WithFields(logrus.Fields{
		"error":             err.Error(),
		"consecutive_fails": j.externalJWKSCache.consecutiveFails,
	}).Error("JWKS fetch failed and no cached keys available")

	return err
}

// GetJWKSCacheStatus returns the current status of the JWKS cache for monitoring
func (j *JWTManager) GetJWKSCacheStatus() map[string]interface{} {
	if j.externalJWKSCache == nil {
		return nil
	}

	j.externalJWKSCache.mu.RLock()
	defer j.externalJWKSCache.mu.RUnlock()

	status := map[string]interface{}{
		"key_count":         len(j.externalJWKSCache.keys),
		"cache_ttl":         j.externalJWKSCache.cacheTTL.String(),
		"consecutive_fails": j.externalJWKSCache.consecutiveFails,
	}

	if !j.externalJWKSCache.lastFetchTime.IsZero() {
		status["last_fetch_time"] = j.externalJWKSCache.lastFetchTime.Format(time.RFC3339)
		status["cache_age_seconds"] = time.Since(j.externalJWKSCache.lastFetchTime).Seconds()
	}

	if !j.externalJWKSCache.expiresAt.IsZero() {
		status["expires_at"] = j.externalJWKSCache.expiresAt.Format(time.RFC3339)
		status["expired"] = time.Now().After(j.externalJWKSCache.expiresAt)
	}

	if j.externalJWKSCache.lastFetchError != nil {
		status["last_error"] = j.externalJWKSCache.lastFetchError.Error()
	}

	return status
}

// HasExternalJWKS returns true if external JWKS validation is configured
func (j *JWTManager) HasExternalJWKS() bool {
	return j.externalJWKSCache != nil && j.externalJWKSURL != ""
}

// SetAPITokenValidator sets the API token validator for API token authentication
// This enables authentication via API tokens (enclii_xxx format) in addition to JWT
func (j *JWTManager) SetAPITokenValidator(validator APITokenValidator) {
	j.apiTokenValidator = validator
}

// HasAPITokenValidator returns true if API token validation is configured
func (j *JWTManager) HasAPITokenValidator() bool {
	return j.apiTokenValidator != nil
}

// GetExternalIssuer returns the expected issuer for external tokens
func (j *JWTManager) GetExternalIssuer() string {
	return j.externalIssuer
}
