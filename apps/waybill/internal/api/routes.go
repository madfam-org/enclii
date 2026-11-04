package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.uber.org/zap"
)

// Server represents the API server
type Server struct {
	router       *gin.Engine
	handlers     *Handlers
	costHandlers *CostHandlers
	logger       *zap.Logger
}

// ServerConfig contains server configuration
type ServerConfig struct {
	// InternalAPIKey is the service key every caller presents on /internal
	// and /api/v1 (X-API-Key, or Authorization: Bearer). Empty never means
	// "open": both groups then answer 503 to every request. cmd/api refuses
	// to start without it, so an empty key here only happens in a caller
	// that builds the server by hand.
	InternalAPIKey string
}

// NewServer creates a new API server. costHandlers may be nil during early
// startup before the budgets store is wired — the server will simply omit
// those routes.
func NewServer(handlers *Handlers, costHandlers *CostHandlers, cfg *ServerConfig, logger *zap.Logger) *Server {
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.Use(gin.Recovery())
	// OTel: inbound SERVER span per request. Placed before the logger
	// middleware so trace_id is already on the context when we log.
	router.Use(otelgin.Middleware("waybill-api"))
	router.Use(requestLogger(logger))

	s := &Server{
		router:       router,
		handlers:     handlers,
		costHandlers: costHandlers,
		logger:       logger,
	}

	s.setupRoutes(cfg)

	return s
}

func (s *Server) setupRoutes(cfg *ServerConfig) {
	// Health check (no auth): kubelet probes carry no key.
	s.router.GET("/health", s.handlers.HealthCheck)
	s.router.GET("/ready", s.handlers.HealthCheck)

	var key string
	if cfg != nil {
		key = cfg.InternalAPIKey
	}
	auth := apiKeyAuth(key, s.logger)

	// Internal API: event ingest from switchyard-api, the Roundhouse worker
	// and Weighbridge.
	internal := s.router.Group("/internal")
	internal.Use(auth)
	{
		internal.POST("/events", s.handlers.RecordEvent)
		internal.POST("/events/batch", s.handlers.RecordEventBatch)
	}

	// Query API: read and budget calls, made only by switchyard-api, which
	// resolves the caller's project under its own RBAC before forwarding.
	api := s.router.Group("/api/v1")
	api.Use(auth)
	{
		// Usage
		api.GET("/projects/:project_id/usage/current", s.handlers.GetCurrentUsage)
		api.GET("/projects/:project_id/usage/history", s.handlers.GetUsageHistory)
		api.POST("/estimate", s.handlers.EstimateCost)

		// Billing
		api.GET("/projects/:project_id/invoices", s.handlers.GetInvoices)

		// Plans
		api.GET("/plans", s.handlers.GetPlans)

		// Budgets + cost queries (P2.2)
		if s.costHandlers != nil {
			s.costHandlers.Register(api)
		}
	}
}

// Run starts the server
func (s *Server) Run(addr string) error {
	s.logger.Info("starting API server", zap.String("addr", addr))
	return s.router.Run(addr)
}

// Router returns the underlying gin router for testing
func (s *Server) Router() *gin.Engine {
	return s.router
}

// Middleware

func requestLogger(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		method := c.Request.Method

		c.Next()

		status := c.Writer.Status()
		logger.Debug("request",
			zap.String("method", method),
			zap.String("path", path),
			zap.Int("status", status),
		)
	}
}

// apiKeyAuth admits a request only when it presents the service key, as
// X-API-Key or as Authorization: Bearer. It fails closed: with no key
// configured every request gets 503, so a missing secret can never turn
// into an open API.
func apiKeyAuth(apiKey string, logger *zap.Logger) gin.HandlerFunc {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		logger.Error("no service key configured: /internal and /api/v1 will refuse every request")
		return func(c *gin.Context) {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "service key not configured"})
		}
	}
	// Compare digests, so neither the comparison time nor an early length
	// mismatch says anything about the key.
	want := sha256.Sum256([]byte(apiKey))

	return func(c *gin.Context) {
		key := c.GetHeader("X-API-Key")
		if key == "" {
			key = c.GetHeader("Authorization")
			if len(key) > 7 && key[:7] == "Bearer " {
				key = key[7:]
			}
		}

		got := sha256.Sum256([]byte(key))
		if key == "" || subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
			// Who called, never what they sent.
			logger.Warn("rejected request without a valid service key",
				zap.String("method", c.Request.Method),
				zap.String("path", c.FullPath()),
				zap.String("user_agent", c.Request.UserAgent()),
				zap.String("client_ip", c.ClientIP()),
			)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		c.Next()
	}
}
