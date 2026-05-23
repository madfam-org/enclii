package middleware

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
)

// generateTestRSAKeys generates a RSA key pair for testing
func generateTestRSAKeys(t *testing.T) (*rsa.PrivateKey, *rsa.PublicKey) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	assert.NoError(t, err)
	return privateKey, &privateKey.PublicKey
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	privateKey, publicKey := generateTestRSAKeys(t)

	// Create a valid token with RS256
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub":   "user123",
		"email": "test@example.com",
		"roles": []string{"admin"},
		"exp":   time.Now().Add(time.Hour).Unix(),
	})

	tokenString, err := token.SignedString(privateKey)
	assert.NoError(t, err)

	// Create test router
	router := gin.New()
	auth := NewAuthMiddleware(publicKey)
	router.Use(auth.Middleware())
	router.GET("/test", func(c *gin.Context) {
		userID := c.GetString("user_id")
		c.JSON(http.StatusOK, gin.H{"user_id": userID})
	})

	// Test request with valid token
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "user123")
}

func TestAuthMiddleware_MissingToken(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	_, publicKey := generateTestRSAKeys(t)

	// Create test router
	router := gin.New()
	auth := NewAuthMiddleware(publicKey)
	router.Use(auth.Middleware())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// Test request without token
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	_, publicKey := generateTestRSAKeys(t)

	// Create test router
	router := gin.New()
	auth := NewAuthMiddleware(publicKey)
	router.Use(auth.Middleware())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// Test request with invalid token
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_PublicPath(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	_, publicKey := generateTestRSAKeys(t)

	// Create test router
	router := gin.New()
	auth := NewAuthMiddleware(publicKey)
	auth.AddPublicPath("/public")
	router.Use(auth.Middleware())
	router.GET("/public", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// Test request to public path without token
	req := httptest.NewRequest("GET", "/public", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthMiddleware_RoleRequirement(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	privateKey, publicKey := generateTestRSAKeys(t)

	// Create a token with user role
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub":   "user123",
		"email": "test@example.com",
		"roles": []string{"user"},
		"exp":   time.Now().Add(time.Hour).Unix(),
	})

	tokenString, err := token.SignedString(privateKey)
	assert.NoError(t, err)

	// Create test router
	router := gin.New()
	auth := NewAuthMiddleware(publicKey)
	auth.AddRoleRequirement("/admin", []string{"admin"})
	router.Use(auth.Middleware())
	router.GET("/admin", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// Test request with insufficient permissions
	req := httptest.NewRequest("GET", "/admin", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestAuthMiddleware_SEC007_IssuerRestrictedAdminFallback(t *testing.T) {
	// When ENCLII_OIDC_ISSUER is set, admin email fallback should only
	// apply to tokens from that issuer.
	gin.SetMode(gin.TestMode)
	privateKey, publicKey := generateTestRSAKeys(t)

	t.Setenv("ENCLII_OIDC_ISSUER", "https://auth.madfam.io")
	t.Setenv("ENCLII_ADMIN_EMAILS", "admin@madfam.io")

	// Token from a different issuer — should NOT get admin role via fallback
	foreignToken := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub":   "user-foreign",
		"email": "admin@madfam.io",
		"iss":   "https://evil-issuer.example.com",
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	foreignStr, err := foreignToken.SignedString(privateKey)
	assert.NoError(t, err)

	// Token from the configured issuer — should get admin role via fallback
	trustedToken := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub":   "user-trusted",
		"email": "admin@madfam.io",
		"iss":   "https://auth.madfam.io",
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	trustedStr, err := trustedToken.SignedString(privateKey)
	assert.NoError(t, err)

	auth := NewAuthMiddleware(publicKey)

	// Setup router with admin-only route
	router := gin.New()
	router.Use(auth.Middleware())
	router.GET("/admin", RequireRole("admin"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// Foreign issuer should be denied admin
	req := httptest.NewRequest("GET", "/admin", nil)
	req.Header.Set("Authorization", "Bearer "+foreignStr)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code, "foreign issuer should not get admin fallback")

	// Trusted issuer should be granted admin
	req2 := httptest.NewRequest("GET", "/admin", nil)
	req2.Header.Set("Authorization", "Bearer "+trustedStr)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code, "trusted issuer should get admin fallback")
}

func TestAuthMiddleware_IsConfiguredIssuer_EmptyRejectsForeignIssuer(t *testing.T) {
	// SEC-007: without a configured issuer, email-based admin elevation must not apply.
	gin.SetMode(gin.TestMode)
	_, publicKey := generateTestRSAKeys(t)

	t.Setenv("ENCLII_OIDC_ISSUER", "")
	auth := NewAuthMiddleware(publicKey)
	assert.False(t, auth.isConfiguredIssuer("https://any-issuer.example.com"))
	assert.False(t, auth.isConfiguredIssuer(""))
}

func TestAuthMiddleware_IsConfiguredIssuer_TrailingSlashTolerant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, publicKey := generateTestRSAKeys(t)

	t.Setenv("ENCLII_OIDC_ISSUER", "https://auth.madfam.io/")
	auth := NewAuthMiddleware(publicKey)
	assert.True(t, auth.isConfiguredIssuer("https://auth.madfam.io"))
	assert.True(t, auth.isConfiguredIssuer("https://auth.madfam.io/"))
	assert.False(t, auth.isConfiguredIssuer("https://evil.example.com"))
}

func TestAuthMiddleware_WrongSigningMethod(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	_, publicKey := generateTestRSAKeys(t)

	// Create a token with HMAC instead of RSA (should be rejected)
	secret := []byte("test-secret")
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   "user123",
		"email": "test@example.com",
		"roles": []string{"admin"},
		"exp":   time.Now().Add(time.Hour).Unix(),
	})

	tokenString, err := token.SignedString(secret)
	assert.NoError(t, err)

	// Create test router
	router := gin.New()
	auth := NewAuthMiddleware(publicKey)
	router.Use(auth.Middleware())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// Test request with HMAC token (should fail)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
