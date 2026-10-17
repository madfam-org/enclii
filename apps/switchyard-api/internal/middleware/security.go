package middleware

import (
	"math/rand/v2"
	"net"
	"net/http"
	"net/netip"
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

	// trustedProxies is config.TrustedProxies parsed once at construction.
	trustedProxies []netip.Prefix
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
	AllowedIPs []string // CIDR blocks
	BlockedIPs []string // CIDR blocks
	// TrustedProxies lists the peers (CIDRs or bare IPs) whose forwarded
	// headers are believed. Empty means nobody's: the peer address is used.
	// DefaultSecurityConfig reads ENCLII_TRUSTED_PROXY_CIDRS.
	TrustedProxies []string

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

		trustedProxies: parseTrustedProxies(config.TrustedProxies),
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

// getClientIP returns the address the rate limiter and the security logs key
// on. Forwarded headers are believed ONLY when the immediate peer
// (RemoteAddr) is inside SecurityConfig.TrustedProxies; from any other peer
// they are client-controlled and ignored. With no trusted proxies configured
// (the default) the peer address is the answer.
//
// From a trusted peer, in order:
//  1. CF-Connecting-IP, set by Cloudflare and passed through by cloudflared;
//  2. X-Forwarded-For, walked right to left, skipping trusted hops;
//  3. X-Real-IP;
//  4. the peer address.
func (s *SecurityMiddleware) getClientIP(c *gin.Context) string {
	peer := remoteHost(c.Request.RemoteAddr)
	peerAddr, err := netip.ParseAddr(peer)
	if err != nil || !s.isTrustedProxy(peerAddr) {
		return peer
	}

	if ip, ok := parseIP(c.Request.Header.Get("CF-Connecting-IP")); ok {
		return ip
	}
	if xff := c.Request.Header.Get("X-Forwarded-For"); xff != "" {
		hops := strings.Split(xff, ",")
		for i := len(hops) - 1; i >= 0; i-- {
			addr, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
			if err != nil {
				break // malformed chain: stop believing it
			}
			if i == 0 || !s.isTrustedProxy(addr) {
				return addr.Unmap().String()
			}
		}
	}
	if ip, ok := parseIP(c.Request.Header.Get("X-Real-IP")); ok {
		return ip
	}
	return peer
}

func (s *SecurityMiddleware) isTrustedProxy(addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, p := range s.trustedProxies {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// remoteHost strips the port from a RemoteAddr, tolerating a bare host.
func remoteHost(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

// parseIP returns the canonical form of a single IP header value.
func parseIP(v string) (string, bool) {
	addr, err := netip.ParseAddr(strings.TrimSpace(v))
	if err != nil {
		return "", false
	}
	return addr.Unmap().String(), true
}

// parseTrustedProxies turns CIDRs or bare IPs into prefixes. An entry that
// does not parse is logged and dropped, so a typo narrows trust rather than
// widening it.
func parseTrustedProxies(entries []string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(entries))
	for _, raw := range entries {
		e := strings.TrimSpace(raw)
		if e == "" {
			continue
		}
		if strings.Contains(e, "/") {
			p, err := netip.ParsePrefix(e)
			if err != nil {
				logrus.WithField("entry", e).Error("Ignoring invalid trusted proxy CIDR")
				continue
			}
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(e)
		if err != nil {
			logrus.WithField("entry", e).Error("Ignoring invalid trusted proxy address")
			continue
		}
		a = a.Unmap()
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	return out
}

// evictOldestLimiters removes the count least recently used rate limiters.
// Callers hold s.mutex. Selection is a quickselect on lastAccess with a
// random pivot, so the expected cost is linear in the number of limiters
// (the previous pairwise sort was quadratic and ran under the global lock,
// exactly when the map was full).
func (s *SecurityMiddleware) evictOldestLimiters(count int) {
	if count <= 0 || len(s.rateLimiters) == 0 {
		return
	}
	entries := make([]limiterAge, 0, len(s.rateLimiters))
	for ip, e := range s.rateLimiters {
		entries = append(entries, limiterAge{ip: ip, lastAccess: e.lastAccess})
	}
	if count > len(entries) {
		count = len(entries)
	}
	selectOldest(entries, count)
	for _, e := range entries[:count] {
		delete(s.rateLimiters, e.ip)
	}

	logrus.WithFields(logrus.Fields{
		"evicted_count": count,
		"remaining":     len(s.rateLimiters),
	}).Info("Evicted old rate limiters")
}

type limiterAge struct {
	ip         string
	lastAccess time.Time
}

// selectOldest partially orders entries so that entries[:k] hold the k
// oldest lastAccess values (in no particular order). Expected O(n).
func selectOldest(entries []limiterAge, k int) {
	lo, hi := 0, len(entries)-1
	for lo < hi {
		pivot := entries[lo+rand.IntN(hi-lo+1)].lastAccess // #nosec G404 -- pivot choice, not security
		// Three-way partition: [lo,lt) older, [lt,gt] equal, (gt,hi] newer.
		lt, i, gt := lo, lo, hi
		for i <= gt {
			switch t := entries[i].lastAccess; {
			case t.Before(pivot):
				entries[lt], entries[i] = entries[i], entries[lt]
				lt++
				i++
			case t.After(pivot):
				entries[i], entries[gt] = entries[gt], entries[i]
				gt--
			default:
				i++
			}
		}
		switch {
		case k < lt:
			hi = lt - 1
		case k > gt+1:
			lo = gt + 1
		default:
			return
		}
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
				s.CleanupRateLimiters()

			case <-s.stopCleanup:
				logrus.Info("Stopping rate limiter cleanup routine")
				return
			}
		}
	}()
}

// CleanupRateLimiters removes rate limiters that haven't been accessed recently
func (s *SecurityMiddleware) CleanupRateLimiters() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

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
		TrustedProxies:   getTrustedProxies(),
	}
}

// getTrustedProxies reads ENCLII_TRUSTED_PROXY_CIDRS (comma-separated CIDRs
// or IPs). Unset means no proxy is trusted and forwarded headers are ignored.
// Behind cloudflared, set it to the range the cloudflared pods connect from
// (the cluster pod CIDR) so the limiter keys on each visitor's
// CF-Connecting-IP instead of on the cloudflared pod.
func getTrustedProxies() []string {
	v := os.Getenv("ENCLII_TRUSTED_PROXY_CIDRS")
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return strings.Split(v, ",")
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
