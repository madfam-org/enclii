package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

// =============================================================================
// IP Filtering Tests
// =============================================================================

func TestIPFilteringMiddleware(t *testing.T) {
	tests := []struct {
		name           string
		allowedIPs     []string
		blockedIPs     []string
		clientIP       string
		expectedStatus int
	}{
		{
			name:           "no filtering",
			allowedIPs:     nil,
			blockedIPs:     nil,
			clientIP:       "192.168.1.1:12345",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "allowed IP",
			allowedIPs:     []string{"192.168.1.0/24"},
			blockedIPs:     nil,
			clientIP:       "192.168.1.100:12345",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "not in allowed list",
			allowedIPs:     []string{"192.168.1.0/24"},
			blockedIPs:     nil,
			clientIP:       "10.0.0.1:12345",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "blocked IP",
			allowedIPs:     nil,
			blockedIPs:     []string{"10.0.0.0/8"},
			clientIP:       "10.0.0.1:12345",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "not blocked",
			allowedIPs:     nil,
			blockedIPs:     []string{"10.0.0.0/8"},
			clientIP:       "192.168.1.1:12345",
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &SecurityConfig{
				AllowedIPs: tt.allowedIPs,
				BlockedIPs: tt.blockedIPs,
			}
			middleware := NewSecurityMiddleware(config)

			router := gin.New()
			router.Use(middleware.IPFilteringMiddleware())
			router.GET("/test", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"status": "ok"})
			})

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/test", nil)
			req.RemoteAddr = tt.clientIP
			router.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("got status %d, want %d", w.Code, tt.expectedStatus)
			}
		})
	}
}

func TestIPFilteringMiddleware_InvalidCIDR(t *testing.T) {
	config := &SecurityConfig{
		AllowedIPs: []string{"invalid-cidr"},
		BlockedIPs: []string{"also-invalid"},
	}
	middleware := NewSecurityMiddleware(config)

	router := gin.New()
	router.Use(middleware.IPFilteringMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Should still work (invalid CIDRs are logged and skipped)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", w.Code, http.StatusOK)
	}
}

// =============================================================================
// CORS Tests
// =============================================================================

func TestCORSMiddleware(t *testing.T) {
	tests := []struct {
		name              string
		allowedOrigins    []string
		requestOrigin     string
		method            string
		expectedStatus    int
		expectAllowOrigin bool
	}{
		{
			name:              "wildcard origin",
			allowedOrigins:    []string{"*"},
			requestOrigin:     "https://example.com",
			method:            "GET",
			expectedStatus:    http.StatusOK,
			expectAllowOrigin: true,
		},
		{
			name:              "allowed origin",
			allowedOrigins:    []string{"https://example.com"},
			requestOrigin:     "https://example.com",
			method:            "GET",
			expectedStatus:    http.StatusOK,
			expectAllowOrigin: true,
		},
		{
			name:              "not allowed origin",
			allowedOrigins:    []string{"https://example.com"},
			requestOrigin:     "https://evil.com",
			method:            "GET",
			expectedStatus:    http.StatusForbidden,
			expectAllowOrigin: false,
		},
		{
			name:              "preflight request",
			allowedOrigins:    []string{"https://example.com"},
			requestOrigin:     "https://example.com",
			method:            "OPTIONS",
			expectedStatus:    http.StatusNoContent,
			expectAllowOrigin: true,
		},
		{
			// Audit 2026-04-23 H6: empty allowlist + AllowCredentials=true must
			// NOT emit Access-Control-Allow-Origin. The request is processed
			// (no abort — server-to-server callers with no Origin header still
			// work) but the browser will block any credentialed response.
			name:              "no allowed origins with credentials — fails closed",
			allowedOrigins:    nil,
			requestOrigin:     "https://example.com",
			method:            "GET",
			expectedStatus:    http.StatusOK,
			expectAllowOrigin: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &SecurityConfig{
				AllowedOrigins:   tt.allowedOrigins,
				AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
				AllowedHeaders:   []string{"Authorization", "Content-Type"},
				AllowCredentials: true,
				MaxAge:           3600,
			}
			middleware := NewSecurityMiddleware(config)

			router := gin.New()
			router.Use(middleware.CORSMiddleware())
			router.GET("/test", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"status": "ok"})
			})
			router.OPTIONS("/test", func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			w := httptest.NewRecorder()
			req, _ := http.NewRequest(tt.method, "/test", nil)
			req.Header.Set("Origin", tt.requestOrigin)
			router.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("got status %d, want %d", w.Code, tt.expectedStatus)
			}

			if tt.expectAllowOrigin {
				allowOrigin := w.Header().Get("Access-Control-Allow-Origin")
				if allowOrigin == "" {
					t.Error("Access-Control-Allow-Origin header missing")
				}
			}

			if tt.expectAllowOrigin && w.Code == http.StatusOK {
				if w.Header().Get("Access-Control-Allow-Methods") == "" {
					t.Error("Access-Control-Allow-Methods header missing")
				}
				if w.Header().Get("Access-Control-Allow-Headers") == "" {
					t.Error("Access-Control-Allow-Headers header missing")
				}
				if w.Header().Get("Access-Control-Allow-Credentials") != "true" {
					t.Error("Access-Control-Allow-Credentials should be true")
				}
				if w.Header().Get("Access-Control-Max-Age") != "3600" {
					t.Errorf("Access-Control-Max-Age = %s, want 3600", w.Header().Get("Access-Control-Max-Age"))
				}
			}
		})
	}
}

// TestCORSNeverWildcardWithCredentials is a regression guard for audit
// 2026-04-23 finding H6. Browsers block responses that combine
// Access-Control-Allow-Origin: * with Access-Control-Allow-Credentials: true,
// but until this fix switchyard-api was willing to emit that combo — plus a
// stray Allow-Origin: * on the SSE log stream. This test asserts that no
// config path can produce the wildcard-plus-credentials response.
func TestCORSNeverWildcardWithCredentials(t *testing.T) {
	cases := []struct {
		name    string
		origins []string
	}{
		{"explicit wildcard in allowlist", []string{"*"}},
		{"empty allowlist", nil},
		{"wildcard alongside specific origin", []string{"*", "https://example.com"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := &SecurityConfig{
				AllowedOrigins:   tc.origins,
				AllowedMethods:   []string{"GET"},
				AllowedHeaders:   []string{"Authorization"},
				AllowCredentials: true,
			}
			m := NewSecurityMiddleware(config)
			router := gin.New()
			router.Use(m.CORSMiddleware())
			router.GET("/t", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"ok": true})
			})

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/t", nil)
			req.Header.Set("Origin", "https://caller.example.com")
			router.ServeHTTP(w, req)

			if got := w.Header().Get("Access-Control-Allow-Origin"); got == "*" {
				t.Fatalf("ACAO is wildcard while AllowCredentials=true: %q", got)
			}
			if w.Header().Get("Access-Control-Allow-Credentials") == "true" &&
				w.Header().Get("Access-Control-Allow-Origin") == "*" {
				t.Fatal("emitted both ACAC=true AND ACAO=* — forbidden by CORS spec")
			}
		})
	}
}

// =============================================================================
// Request Logging Tests
// =============================================================================

func TestRequestLoggingMiddleware(t *testing.T) {
	middleware := NewSecurityMiddleware(nil)

	router := gin.New()
	router.Use(middleware.RequestLoggingMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/error", func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "test error"})
	})

	// Normal request
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", w.Code, http.StatusOK)
	}

	// Error request (should trigger logging)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/error", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got status %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// =============================================================================
// Content Type Validation Tests
// =============================================================================

func TestContentTypeValidationMiddleware(t *testing.T) {
	middleware := NewSecurityMiddleware(nil)

	router := gin.New()
	router.Use(middleware.ContentTypeValidationMiddleware())
	router.POST("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	tests := []struct {
		name           string
		method         string
		contentType    string
		expectedStatus int
	}{
		{"GET request no validation", "GET", "", http.StatusOK},
		{"POST with JSON", "POST", "application/json", http.StatusOK},
		{"POST with form data", "POST", "application/x-www-form-urlencoded", http.StatusOK},
		{"POST with multipart", "POST", "multipart/form-data", http.StatusOK},
		{"POST with text", "POST", "text/plain", http.StatusOK},
		{"POST with JSON and charset", "POST", "application/json; charset=utf-8", http.StatusOK},
		{"POST with invalid type", "POST", "application/xml", http.StatusUnsupportedMediaType},
		{"POST with no content type", "POST", "", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(tt.method, "/test", nil)
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			router.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("got status %d, want %d", w.Code, tt.expectedStatus)
			}
		})
	}
}

// =============================================================================
// User Agent Validation Tests
// =============================================================================

func TestUserAgentValidationMiddleware(t *testing.T) {
	middleware := NewSecurityMiddleware(nil)

	router := gin.New()
	router.Use(middleware.UserAgentValidationMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	tests := []struct {
		name           string
		userAgent      string
		expectedStatus int
	}{
		{"normal browser", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)", http.StatusOK},
		{"sqlmap", "sqlmap/1.0", http.StatusForbidden},
		{"nikto", "Nikto/2.1.6", http.StatusForbidden},
		{"nmap", "Nmap Scripting Engine", http.StatusForbidden},
		{"empty user agent", "", http.StatusOK},
		{"legitimate curl", "curl/8.0.1", http.StatusOK},
		{"suspicious curl", "curl/7.68.0", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/test", nil)
			req.Header.Set("User-Agent", tt.userAgent)
			router.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("got status %d, want %d", w.Code, tt.expectedStatus)
			}
		})
	}
}

// =============================================================================
// Client IP Detection Tests
// =============================================================================

func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name           string
		trustedProxies []string
		remoteAddr     string
		xForwardedFor  string
		xRealIP        string
		expectedIP     string
	}{
		{
			name:       "direct connection",
			remoteAddr: "192.168.1.100:12345",
			expectedIP: "192.168.1.100",
		},
		{
			name:       "X-Real-IP header",
			remoteAddr: "10.0.0.1:12345",
			xRealIP:    "192.168.1.100",
			expectedIP: "192.168.1.100",
		},
		{
			name:           "X-Forwarded-For with trusted proxy",
			trustedProxies: []string{"10.0.0.0/8"},
			remoteAddr:     "10.0.0.1:12345",
			xForwardedFor:  "192.168.1.100, 10.0.0.2",
			expectedIP:     "192.168.1.100",
		},
		{
			name:          "X-Forwarded-For without trusted proxy",
			remoteAddr:    "10.0.0.1:12345",
			xForwardedFor: "192.168.1.100",
			xRealIP:       "192.168.1.200",
			expectedIP:    "192.168.1.200",
		},
		{
			name:       "no port in remote addr",
			remoteAddr: "192.168.1.100",
			expectedIP: "192.168.1.100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &SecurityConfig{
				TrustedProxies: tt.trustedProxies,
			}
			middleware := NewSecurityMiddleware(config)

			router := gin.New()
			var capturedIP string
			router.GET("/test", func(c *gin.Context) {
				capturedIP = middleware.getClientIP(c)
				c.String(http.StatusOK, "ok")
			})

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/test", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xForwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.xForwardedFor)
			}
			if tt.xRealIP != "" {
				req.Header.Set("X-Real-IP", tt.xRealIP)
			}
			router.ServeHTTP(w, req)

			if capturedIP != tt.expectedIP {
				t.Errorf("getClientIP() = %s, want %s", capturedIP, tt.expectedIP)
			}
		})
	}
}

// =============================================================================
// Security Event Logging Tests
// =============================================================================

func TestLogSecurityEvent(t *testing.T) {
	// Test that LogSecurityEvent doesn't panic
	LogSecurityEvent(
		"test_event",
		"192.168.1.1",
		"Mozilla/5.0",
		"/test/path",
		"GET",
		"test message",
		200,
	)

	// If we got here without panicking, the test passes
}

// =============================================================================
// Concurrency Tests
// =============================================================================

func TestSecurityMiddleware_Concurrency(t *testing.T) {
	config := &SecurityConfig{
		RateLimit: 100,
		RateBurst: 200,
	}
	middleware := NewSecurityMiddleware(config)

	router := gin.New()
	router.Use(middleware.RateLimitMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Test concurrent requests from different IPs
	var wg sync.WaitGroup
	numGoroutines := 50
	requestsPerGoroutine := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				w := httptest.NewRecorder()
				req, _ := http.NewRequest("GET", "/test", nil)
				// Use different IP for each goroutine
				req.RemoteAddr = strings.Replace("192.168.1.X:12345", "X", string(rune(id)), 1)
				router.ServeHTTP(w, req)
			}
		}(i)
	}

	wg.Wait()

	// Verify rate limiters were created without race conditions
	middleware.mutex.RLock()
	numLimiters := len(middleware.rateLimiters)
	middleware.mutex.RUnlock()

	if numLimiters == 0 {
		t.Error("No rate limiters were created")
	}
}
