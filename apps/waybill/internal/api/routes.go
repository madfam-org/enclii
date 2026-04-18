package api

import (
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.uber.org/zap"
)

// Server represents the API server
type Server struct {
	router   *gin.Engine
	handlers *Handlers
	logger   *zap.Logger
}

// ServerConfig contains server configuration
type ServerConfig struct {
	InternalAPIKey string
}

// NewServer creates a new API server
func NewServer(handlers *Handlers, cfg *ServerConfig, logger *zap.Logger) *Server {
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.Use(gin.Recovery())
	// OTel: inbound SERVER span per request. Placed before the logger
	// middleware so trace_id is already on the context when we log.
	router.Use(otelgin.Middleware("waybill-api"))
	router.Use(requestLogger(logger))

	s := &Server{
		router:   router,
		handlers: handlers,
		logger:   logger,
	}

	s.setupRoutes(cfg)

	return s
}

func (s *Server) setupRoutes(cfg *ServerConfig) {
	// Health check (no auth)
	s.router.GET("/health", s.handlers.HealthCheck)
	s.router.GET("/ready", s.handlers.HealthCheck)

	// Internal API (for Switchyard/Roundhouse)
	internal := s.router.Group("/internal")
	if cfg.InternalAPIKey != "" {
		internal.Use(apiKeyAuth(cfg.InternalAPIKey))
	}
	{
		internal.POST("/events", s.handlers.RecordEvent)
		internal.POST("/events/batch", s.handlers.RecordEventBatch)
	}

	// Public API (authenticated via JWT from Switchyard)
	api := s.router.Group("/api/v1")
	// Add JWT validation middleware in production
	{
		// Usage
		api.GET("/projects/:project_id/usage/current", s.handlers.GetCurrentUsage)
		api.GET("/projects/:project_id/usage/history", s.handlers.GetUsageHistory)
		api.POST("/estimate", s.handlers.EstimateCost)

		// Billing
		api.GET("/projects/:project_id/invoices", s.handlers.GetInvoices)

		// Plans
		api.GET("/plans", s.handlers.GetPlans)
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

func apiKeyAuth(apiKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.GetHeader("X-API-Key")
		if key == "" {
			key = c.GetHeader("Authorization")
			if len(key) > 7 && key[:7] == "Bearer " {
				key = key[7:]
			}
		}

		if key != apiKey {
			c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
			return
		}

		c.Next()
	}
}
