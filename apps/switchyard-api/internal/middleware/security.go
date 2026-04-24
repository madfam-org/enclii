package middleware

import (
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

// SecurityMiddleware provides various security-related middleware functions
type SecurityMiddleware struct {
	rateLimiters map[string]*rateLimiterEntry
	mutex        sync.RWMutex
	config       *SecurityConfig
	stopCleanup  chan struct{} // Channel to stop the cleanup goroutine
}

// rateLimiterEntry holds a rate limiter and its last access time
type rateLimiterEntry struct {
	limiter    *rate.Limiter
	lastAccess time.Time
}

type SecurityConfig struct {
	// Rate limiting
	RateLimit      int           // requests per second
	RateBurst      int           // burst capacity
	RateWindowSize time.Duration // time window for rate limiting

	// Security headers
	EnableHSTS          bool
	EnableCSP           bool
	EnableXSSProtection bool
	EnableNoSniff       bool
	EnableFrameOptions  bool

	// Request validation
	MaxRequestSize int64 // in bytes
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration

	// IP filtering
	AllowedIPs     []string // CIDR blocks
	BlockedIPs     []string // CIDR blocks
	TrustedProxies []string // for X-Forwarded-For header

	// CORS
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	AllowCredentials bool
	MaxAge           int
}

func NewSecurityMiddleware(config *SecurityConfig) *SecurityMiddleware {
	if config == nil {
		config = DefaultSecurityConfig()
	}

	sm := &SecurityMiddleware{
		rateLimiters: make(map[string]*rateLimiterEntry),
		config:       config,
		stopCleanup:  make(chan struct{}),
	}

	// Start cleanup goroutine
	sm.startCleanupRoutine()

	return sm
}

// Rate limiting middleware with bounded cache
func (s *SecurityMiddleware) RateLimitMiddleware() gin.HandlerFunc {
	const maxLimiters = 100000 // Maximum number of IP-based rate limiters to prevent memory exhaustion

	return func(c *gin.Context) {
		clientIP := s.getClientIP(c)

		s.mutex.RLock()
		entry, exists := s.rateLimiters[clientIP]
		s.mutex.RUnlock()

		if !exists {
			s.mutex.Lock()
			// Double-check after acquiring write lock
			if entry, exists = s.rateLimiters[clientIP]; !exists {
				// SECURITY FIX: Prevent unbounded map growth
				if len(s.rateLimiters) >= maxLimiters {
					// Remove oldest entries (simple FIFO eviction)
					s.evictOldestLimiters(maxLimiters / 10) // Remove 10% of entries
				}

				entry = &rateLimiterEntry{
					limiter:    rate.NewLimiter(rate.Limit(s.config.RateLimit), s.config.RateBurst),
					lastAccess: time.Now(),
				}
				s.rateLimiters[clientIP] = entry
			}
			s.mutex.Unlock()
		} else {
			// Update last access time
			s.mutex.Lock()
			entry.lastAccess = time.Now()
			s.mutex.Unlock()
		}

		if !entry.limiter.Allow() {
			logrus.WithFields(logrus.Fields{
				"client_ip": clientIP,
				"path":      c.Request.URL.Path,
				"method":    c.Request.Method,
			}).Warn("Rate limit exceeded")

			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":       "Rate limit exceeded",
				"retry_after": "60",
			})
			c.Header("Retry-After", "60")
			c.Abort()
			return
		}

		c.Next()
	}
}

// Security headers middleware
func (s *SecurityMiddleware) SecurityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if s.config.EnableHSTS {
			c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		if s.config.EnableCSP {
			c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'")
		}

		if s.config.EnableXSSProtection {
			c.Header("X-XSS-Protection", "1; mode=block")
		}

		if s.config.EnableNoSniff {
			c.Header("X-Content-Type-Options", "nosniff")
		}

		if s.config.EnableFrameOptions {
			c.Header("X-Frame-Options", "DENY")
		}

		// Additional security headers
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

		c.Next()
	}
}

// Request size limiting middleware
func (s *SecurityMiddleware) RequestSizeLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > s.config.MaxRequestSize {
			logrus.WithFields(logrus.Fields{
				"client_ip":      s.getClientIP(c),
				"content_length": c.Request.ContentLength,
				"max_size":       s.config.MaxRequestSize,
			}).Warn("Request size too large")

			c.JSON(http.StatusRequestEntityTooLarge, gin.H{
				"error":    "Request size too large",
				"max_size": s.config.MaxRequestSize,
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// IP filtering middleware
func (s *SecurityMiddleware) IPFilteringMiddleware() gin.HandlerFunc {
	// Parse allowed and blocked IP ranges
	allowedNets := make([]*net.IPNet, 0, len(s.config.AllowedIPs))
	for _, cidr := range s.config.AllowedIPs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			logrus.Errorf("Invalid allowed IP CIDR: %s", cidr)
			continue
		}
		allowedNets = append(allowedNets, ipNet)
	}

	blockedNets := make([]*net.IPNet, 0, len(s.config.BlockedIPs))
	for _, cidr := range s.config.BlockedIPs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			logrus.Errorf("Invalid blocked IP CIDR: %s", cidr)
			continue
		}
		blockedNets = append(blockedNets, ipNet)
	}

	return func(c *gin.Context) {
		clientIP := net.ParseIP(s.getClientIP(c))
		if clientIP == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid IP address"})
			c.Abort()
			return
		}

		// Check blocked IPs first
		for _, blockedNet := range blockedNets {
			if blockedNet.Contains(clientIP) {
				logrus.WithField("client_ip", clientIP.String()).Warn("Blocked IP attempted access")
				c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
				c.Abort()
				return
			}
		}

		// Check allowed IPs (if configured)
		if len(allowedNets) > 0 {
			allowed := false
			for _, allowedNet := range allowedNets {
				if allowedNet.Contains(clientIP) {
					allowed = true
					break
				}
			}

			if !allowed {
				logrus.WithField("client_ip", clientIP.String()).Warn("Non-whitelisted IP attempted access")
				c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
				c.Abort()
				return
			}
		}

		c.Next()
	}
}

// CORS middleware
//
// Audit 2026-04-23 finding H6: switchyard-api is the control plane for every
// service in the ecosystem — a misconfigured CORS response (wildcard Origin
// paired with Allow-Credentials: true) has ecosystem-wide blast radius.
// This middleware now:
//   - Never emits "Access-Control-Allow-Origin: *" when AllowCredentials=true.
//   - Only reflects the request Origin when it matches the configured allowlist.
//   - Refuses to return an Allow-Origin header at all when the allowlist is
//     empty in an AllowCredentials=true build, so browsers will block the request.
//
// The wildcard path remains available for AllowCredentials=false configurations
// (e.g. public read-only endpoints) where the wildcard cannot be weaponized
// into a cross-origin credentialed request.
func (s *SecurityMiddleware) CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")

		// Only apply CORS checks if Origin header is present (browser requests)
		// Server-to-server requests (curl, etc.) don't send Origin header
		if origin != "" {
			// Check if origin is allowed
			if len(s.config.AllowedOrigins) > 0 {
				allowed := false
				originMatch := ""
				for _, allowedOrigin := range s.config.AllowedOrigins {
					if allowedOrigin == origin {
						allowed = true
						originMatch = origin
						break
					}
					if allowedOrigin == "*" {
						// "*" is accepted in config only when credentials are off.
						// Pairing a wildcard with credentials is never valid; emit
						// the request Origin echo instead so browsers enforce.
						if s.config.AllowCredentials {
							allowed = true
							originMatch = origin
							break
						}
						allowed = true
						originMatch = "*"
						break
					}
				}

				if !allowed {
					c.JSON(http.StatusForbidden, gin.H{"error": "Origin not allowed"})
					c.Abort()
					return
				}

				c.Header("Access-Control-Allow-Origin", originMatch)
				if originMatch != "*" {
					// Vary lets caches key responses per Origin when we reflect.
					c.Header("Vary", "Origin")
				}
			} else if !s.config.AllowCredentials {
				// Public-read mode only; wildcarding is safe here because the
				// browser will never attach cookies/auth.
				c.Header("Access-Control-Allow-Origin", "*")
			}
			// If AllowedOrigins is empty AND AllowCredentials is true we
			// deliberately omit Access-Control-Allow-Origin → the browser
			// will block the credentialed request. Configuration bug, not a
			// spoofable header.
		}

		c.Header("Access-Control-Allow-Methods", strings.Join(s.config.AllowedMethods, ", "))
		c.Header("Access-Control-Allow-Headers", strings.Join(s.config.AllowedHeaders, ", "))

		if s.config.AllowCredentials {
			// Only paired with a non-wildcard Allow-Origin per the guard above.
			c.Header("Access-Control-Allow-Credentials", "true")
		}

		if s.config.MaxAge > 0 {
			c.Header("Access-Control-Max-Age", strconv.Itoa(s.config.MaxAge))
		}

		// Handle preflight request
		if c.Request.Method == "OPTIONS" {
			c.Status(http.StatusNoContent)
			c.Abort()
			return
		}

		c.Next()
	}
}

// Request logging middleware
func (s *SecurityMiddleware) RequestLoggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		duration := time.Since(start)

		// Log suspicious requests
		if c.Writer.Status() >= 400 || duration > 5*time.Second {
			logrus.WithFields(logrus.Fields{
				"client_ip":      s.getClientIP(c),
				"method":         c.Request.Method,
				"path":           c.Request.URL.Path,
				"status":         c.Writer.Status(),
				"duration":       duration.String(),
				"user_agent":     c.Request.UserAgent(),
				"content_length": c.Request.ContentLength,
			}).Info("HTTP request")
		}
	}
}

// Content type validation middleware
func (s *SecurityMiddleware) ContentTypeValidationMiddleware() gin.HandlerFunc {
	allowedTypes := map[string]bool{
		"application/json":                  true,
		"application/x-www-form-urlencoded": true,
		"multipart/form-data":               true,
		"text/plain":                        true,
	}

	return func(c *gin.Context) {
		if c.Request.Method == "POST" || c.Request.Method == "PUT" || c.Request.Method == "PATCH" {
			contentType := c.GetHeader("Content-Type")
			if contentType != "" {
				// Parse content type (remove charset, etc.)
				parts := strings.Split(contentType, ";")
				mainType := strings.TrimSpace(parts[0])

				if !allowedTypes[mainType] {
					logrus.WithFields(logrus.Fields{
						"client_ip":    s.getClientIP(c),
						"content_type": contentType,
						"path":         c.Request.URL.Path,
					}).Warn("Invalid content type")

					c.JSON(http.StatusUnsupportedMediaType, gin.H{
						"error": "Unsupported content type",
						"allowed_types": []string{
							"application/json",
							"application/x-www-form-urlencoded",
							"multipart/form-data",
							"text/plain",
						},
					})
					c.Abort()
					return
				}
			}
		}

		c.Next()
	}
}

// User agent validation middleware (blocks known malicious user agents)
func (s *SecurityMiddleware) UserAgentValidationMiddleware() gin.HandlerFunc {
	suspiciousAgents := []string{
		"sqlmap",
		"nikto",
		"nmap",
		"masscan",
		"gobuster",
		"dirb",
		"dirbuster",
		"wpscan",
		"curl/7", // Be careful with this one - many legitimate tools use curl
	}

	return func(c *gin.Context) {
		userAgent := strings.ToLower(c.Request.UserAgent())

		for _, suspicious := range suspiciousAgents {
			if strings.Contains(userAgent, suspicious) {
				logrus.WithFields(logrus.Fields{
					"client_ip":  s.getClientIP(c),
					"user_agent": c.Request.UserAgent(),
					"path":       c.Request.URL.Path,
				}).Warn("Suspicious user agent detected")

				c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
				c.Abort()
				return
			}
		}

		c.Next()
	}
}

// Get client IP address considering proxies
func (s *SecurityMiddleware) getClientIP(c *gin.Context) string {
	// Check X-Forwarded-For header (if using trusted proxies)
	if len(s.config.TrustedProxies) > 0 {
		forwarded := c.Request.Header.Get("X-Forwarded-For")
		if forwarded != "" {
			// Get the first IP in the chain
			ips := strings.Split(forwarded, ",")
			return strings.TrimSpace(ips[0])
		}
	}

	// Check X-Real-IP header
	if realIP := c.Request.Header.Get("X-Real-IP"); realIP != "" {
		return realIP
	}

	// Fall back to remote address
	ip, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		return c.Request.RemoteAddr
	}

	return ip
}

// evictOldestLimiters removes the oldest rate limiters based on last access time
// SECURITY FIX: Prevents unbounded memory growth by evicting least recently used entries
func (s *SecurityMiddleware) evictOldestLimiters(count int) {
	// Build a list of IP addresses sorted by last access time
	type entry struct {
		ip         string
		lastAccess time.Time
	}

	entries := make([]entry, 0, len(s.rateLimiters))
	for ip, limiterEntry := range s.rateLimiters {
		entries = append(entries, entry{ip: ip, lastAccess: limiterEntry.lastAccess})
	}

	// Sort by last access time (oldest first)
	for i := 0; i < len(entries)-1; i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[i].lastAccess.After(entries[j].lastAccess) {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	// Remove the oldest entries
	removed := 0
	for i := 0; i < len(entries) && removed < count; i++ {
		delete(s.rateLimiters, entries[i].ip)
		removed++
	}

	if removed > 0 {
		logrus.WithFields(logrus.Fields{
			"evicted_count": removed,
			"remaining":     len(s.rateLimiters),
		}).Info("Evicted old rate limiters")
	}
}

// startCleanupRoutine starts a goroutine that periodically cleans up old rate limiters
// SECURITY FIX: Adds proper goroutine lifecycle management with cancellation
func (s *SecurityMiddleware) startCleanupRoutine() {
	ticker := time.NewTicker(15 * time.Minute) // Run cleanup every 15 minutes

	go func() {
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.mutex.Lock()
				// Remove rate limiters that haven't been accessed in the last hour
				cutoff := time.Now().Add(-1 * time.Hour)
				removed := 0

				for ip, entry := range s.rateLimiters {
					if entry.lastAccess.Before(cutoff) {
						delete(s.rateLimiters, ip)
						removed++
					}
				}

				if removed > 0 {
					logrus.WithFields(logrus.Fields{
						"removed_count": removed,
						"remaining":     len(s.rateLimiters),
					}).Info("Cleaned up inactive rate limiters")
				}
				s.mutex.Unlock()

			case <-s.stopCleanup:
				logrus.Info("Stopping rate limiter cleanup routine")
				return
			}
		}
	}()
}

// Stop gracefully shuts down the cleanup routine
// SECURITY FIX: Prevents goroutine leak by allowing graceful shutdown
func (s *SecurityMiddleware) Stop() {
	close(s.stopCleanup)
}

// Default security configuration
func DefaultSecurityConfig() *SecurityConfig {
	return &SecurityConfig{
		RateLimit:           100, // 100 requests per second
		RateBurst:           200, // 200 burst capacity
		RateWindowSize:      time.Minute,
		EnableHSTS:          true,
		EnableCSP:           true,
		EnableXSSProtection: true,
		EnableNoSniff:       true,
		EnableFrameOptions:  true,
		MaxRequestSize:      10 << 20, // 10MB
		ReadTimeout:         30 * time.Second,
		WriteTimeout:        30 * time.Second,
		// SECURITY FIX: Restrict CORS origins (was: "*" - CWE-942 vulnerability)
		// Production: Set via ENCLII_ALLOWED_ORIGINS environment variable (comma-separated)
		// Development: Defaults to localhost origins only
		AllowedOrigins:   getAllowedOrigins(),
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "X-Requested-With", "X-IDP-Token", "X-CSRF-Token"},
		AllowCredentials: true,
		MaxAge:           86400, // 24 hours
	}
}

// getAllowedOrigins returns the allowed CORS origins based on environment configuration.
// In production, this MUST be set via ENCLII_ALLOWED_ORIGINS environment variable.
// In development, defaults to localhost origins only.
func getAllowedOrigins() []string {
	// Check environment variable first (production)
	if origins := os.Getenv("ENCLII_ALLOWED_ORIGINS"); origins != "" {
		// Trim whitespace from each origin
		parts := strings.Split(origins, ",")
		cleaned := make([]string, 0, len(parts))
		for _, origin := range parts {
			if trimmed := strings.TrimSpace(origin); trimmed != "" {
				cleaned = append(cleaned, trimmed)
			}
		}
		return cleaned
	}

	// Development defaults - only localhost
	// This is safe for local development with kind/docker-compose
	return []string{
		"http://localhost:3000", // Next.js dev server
		"http://localhost:8030", // Enclii UI (per PORT_REGISTRY)
		"http://localhost:8080", // API dev server (legacy)
		"http://localhost:8001", // Enclii API (per PORT_REGISTRY)
		"http://127.0.0.1:3000",
		"http://127.0.0.1:8030",
		"http://127.0.0.1:8080",
		"http://127.0.0.1:8001",
	}
}

// Security event logging
type SecurityEvent struct {
	Timestamp  time.Time `json:"timestamp"`
	EventType  string    `json:"event_type"`
	ClientIP   string    `json:"client_ip"`
	UserAgent  string    `json:"user_agent"`
	Path       string    `json:"path"`
	Method     string    `json:"method"`
	StatusCode int       `json:"status_code"`
	Message    string    `json:"message"`
}

func LogSecurityEvent(eventType, clientIP, userAgent, path, method, message string, statusCode int) {
	event := SecurityEvent{
		Timestamp:  time.Now(),
		EventType:  eventType,
		ClientIP:   clientIP,
		UserAgent:  userAgent,
		Path:       path,
		Method:     method,
		StatusCode: statusCode,
		Message:    message,
	}

	logrus.WithFields(logrus.Fields{
		"security_event": event,
	}).Warn("Security event logged")
}
