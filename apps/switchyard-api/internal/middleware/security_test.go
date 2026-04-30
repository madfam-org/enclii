package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func init() {
	// Set Gin to test mode
	gin.SetMode(gin.TestMode)
}

// =============================================================================
// Security Middleware Creation Tests
// =============================================================================

func TestNewSecurityMiddleware(t *testing.T) {
	tests := []struct {
		name   string
		config *SecurityConfig
	}{
		{
			name:   "with nil config",
			config: nil,
		},
		{
			name: "with custom config",
			config: &SecurityConfig{
				RateLimit:      50,
				RateBurst:      100,
				MaxRequestSize: 5 << 20, // 5MB
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware := NewSecurityMiddleware(tt.config)

			if middleware == nil {
				t.Error("NewSecurityMiddleware() returned nil")
				return
			}

			if middleware.rateLimiters == nil {
				t.Error("rateLimiters map is nil")
			}

			if middleware.config == nil {
				t.Error("config is nil")
			}

			// If nil config was passed, should use defaults
			if tt.config == nil {
				if middleware.config.RateLimit != 100 {
					t.Errorf("Default RateLimit = %d, want 100", middleware.config.RateLimit)
				}
			}
		})
	}
}

// =============================================================================
// Rate Limiting Tests
// =============================================================================

func TestRateLimitMiddleware(t *testing.T) {
	config := &SecurityConfig{
		RateLimit: 2, // 2 requests per second
		RateBurst: 2, // 2 burst capacity
	}
	middleware := NewSecurityMiddleware(config)

	router := gin.New()
	router.Use(middleware.RateLimitMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// First 2 requests should succeed (within burst limit)
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d: got status %d, want %d", i+1, w.Code, http.StatusOK)
		}
	}

	// Third request should be rate limited
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	router.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Rate limit request: got status %d, want %d", w.Code, http.StatusTooManyRequests)
	}

	if w.Header().Get("Retry-After") != "60" {
		t.Errorf("Retry-After header = %s, want 60", w.Header().Get("Retry-After"))
	}
}

func TestRateLimitMiddleware_DifferentIPs(t *testing.T) {
	config := &SecurityConfig{
		RateLimit: 1,
		RateBurst: 1,
	}
	middleware := NewSecurityMiddleware(config)

	router := gin.New()
	router.Use(middleware.RateLimitMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Requests from different IPs should not share rate limits
	ips := []string{"192.168.1.1:12345", "192.168.1.2:12346", "192.168.1.3:12347"}

	for _, ip := range ips {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		req.RemoteAddr = ip
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request from %s: got status %d, want %d", ip, w.Code, http.StatusOK)
		}
	}
}

// =============================================================================
// Security Headers Tests
// =============================================================================

func TestSecurityHeadersMiddleware(t *testing.T) {
	config := &SecurityConfig{
		EnableHSTS:          true,
		EnableCSP:           true,
		EnableXSSProtection: true,
		EnableNoSniff:       true,
		EnableFrameOptions:  true,
	}
	middleware := NewSecurityMiddleware(config)

	router := gin.New()
	router.Use(middleware.SecurityHeadersMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	expectedHeaders := map[string]string{
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
		"Content-Security-Policy":   "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'",
		"X-XSS-Protection":          "1; mode=block",
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
		"Permissions-Policy":        "geolocation=(), microphone=(), camera=()",
	}

	for header, expected := range expectedHeaders {
		if got := w.Header().Get(header); got != expected {
			t.Errorf("Header %s = %s, want %s", header, got, expected)
		}
	}
}

func TestSecurityHeadersMiddleware_Disabled(t *testing.T) {
	config := &SecurityConfig{
		EnableHSTS:          false,
		EnableCSP:           false,
		EnableXSSProtection: false,
		EnableNoSniff:       false,
		EnableFrameOptions:  false,
	}
	middleware := NewSecurityMiddleware(config)

	router := gin.New()
	router.Use(middleware.SecurityHeadersMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	// Optional security headers should not be present
	optionalHeaders := []string{
		"Strict-Transport-Security",
		"Content-Security-Policy",
		"X-XSS-Protection",
		"X-Content-Type-Options",
		"X-Frame-Options",
	}

	for _, header := range optionalHeaders {
		if w.Header().Get(header) != "" {
			t.Errorf("Header %s should not be set when disabled", header)
		}
	}

	// These headers should always be present
	if w.Header().Get("Referrer-Policy") == "" {
		t.Error("Referrer-Policy header missing")
	}
}

// =============================================================================
// Request Size Limit Tests
// =============================================================================

func TestRequestSizeLimitMiddleware(t *testing.T) {
	config := &SecurityConfig{
		MaxRequestSize: 100, // 100 bytes
	}
	middleware := NewSecurityMiddleware(config)

	router := gin.New()
	router.Use(middleware.RequestSizeLimitMiddleware())
	router.POST("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	tests := []struct {
		name           string
		contentLength  int64
		expectedStatus int
	}{
		{"small request", 50, http.StatusOK},
		{"exactly at limit", 100, http.StatusOK},
		{"too large", 101, http.StatusRequestEntityTooLarge},
		{"much too large", 1000, http.StatusRequestEntityTooLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			body := bytes.NewReader(make([]byte, tt.contentLength))
			req, _ := http.NewRequest("POST", "/test", body)
			req.ContentLength = tt.contentLength
			router.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("got status %d, want %d", w.Code, tt.expectedStatus)
			}
		})
	}
}

// =============================================================================
// Default Config Tests
// =============================================================================

func TestDefaultSecurityConfig(t *testing.T) {
	config := DefaultSecurityConfig()

	if config == nil {
		t.Fatal("DefaultSecurityConfig() returned nil")
	}

	// Verify default values
	if config.RateLimit != 100 {
		t.Errorf("RateLimit = %d, want 100", config.RateLimit)
	}

	if config.RateBurst != 200 {
		t.Errorf("RateBurst = %d, want 200", config.RateBurst)
	}

	if !config.EnableHSTS {
		t.Error("EnableHSTS should be true")
	}

	if !config.EnableCSP {
		t.Error("EnableCSP should be true")
	}

	if config.MaxRequestSize != 10<<20 {
		t.Errorf("MaxRequestSize = %d, want %d", config.MaxRequestSize, 10<<20)
	}

	if len(config.AllowedOrigins) == 0 {
		t.Error("AllowedOrigins should not be empty")
	}

	if len(config.AllowedMethods) == 0 {
		t.Error("AllowedMethods should not be empty")
	}

	if !config.AllowCredentials {
		t.Error("AllowCredentials should be true")
	}
}

// =============================================================================
// Allowed Origins Configuration Tests
// =============================================================================

func TestGetAllowedOrigins(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected []string
	}{
		{
			name:     "no environment variable",
			envValue: "",
			expected: []string{
				"http://localhost:3000",
				"http://localhost:8030",
				"http://localhost:8080",
				"http://localhost:8001",
				"http://127.0.0.1:3000",
				"http://127.0.0.1:8030",
				"http://127.0.0.1:8080",
				"http://127.0.0.1:8001",
			},
		},
		{
			name:     "single origin",
			envValue: "https://example.com",
			expected: []string{"https://example.com"},
		},
		{
			name:     "multiple origins",
			envValue: "https://example.com, https://app.example.com",
			expected: []string{"https://example.com", "https://app.example.com"},
		},
		{
			name:     "origins with whitespace",
			envValue: "  https://example.com  ,  https://app.example.com  ",
			expected: []string{"https://example.com", "https://app.example.com"},
		},
		{
			name:     "empty strings filtered",
			envValue: "https://example.com,,https://app.example.com",
			expected: []string{"https://example.com", "https://app.example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable
			if tt.envValue != "" {
				os.Setenv("ENCLII_ALLOWED_ORIGINS", tt.envValue)
				defer os.Unsetenv("ENCLII_ALLOWED_ORIGINS")
			} else {
				os.Unsetenv("ENCLII_ALLOWED_ORIGINS")
			}

			got := getAllowedOrigins()

			if len(got) != len(tt.expected) {
				t.Errorf("getAllowedOrigins() returned %d origins, want %d", len(got), len(tt.expected))
				return
			}

			for i, origin := range got {
				if origin != tt.expected[i] {
					t.Errorf("getAllowedOrigins()[%d] = %s, want %s", i, origin, tt.expected[i])
				}
			}
		})
	}
}

func TestCleanupRateLimiters(t *testing.T) {
	middleware := NewSecurityMiddleware(nil)

	// Add many rate limiters with old access times
	middleware.mutex.Lock()
	oldTime := time.Now().Add(-2 * time.Hour)
	for i := 0; i < 100; i++ {
		key := string(rune(i))
		middleware.rateLimiters[key] = &rateLimiterEntry{
			limiter:    nil, // Doesn't matter for this test
			lastAccess: oldTime,
		}
	}
	middleware.mutex.Unlock()

	if len(middleware.rateLimiters) != 100 {
		t.Errorf("Initial limiters count = %d, want 100", len(middleware.rateLimiters))
	}

	// Start cleanup manually
	middleware.CleanupRateLimiters()

	if len(middleware.rateLimiters) != 0 {
		t.Errorf("Limiters count after cleanup = %d, want 0", len(middleware.rateLimiters))
	}
}
