package api

import (
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.uber.org/zap"

	"github.com/madfam-org/enclii/apps/roundhouse/internal/queue"
	"github.com/madfam-org/enclii/apps/roundhouse/internal/webhook"
)

// Server represents the API server
type Server struct {
	router   *gin.Engine
	handlers *Handlers
	logger   *zap.Logger
}

// NewServer creates a new API server
func NewServer(q *queue.RedisQueue, cfg *ServerConfig, logger *zap.Logger) *Server {
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.Use(gin.Recovery())
	// OTel: emit a SERVER span for every inbound request before the logger
	// so log lines captured during request processing carry trace_id.
	router.Use(otelgin.Middleware("roundhouse"))
	router.Use(requestLogger(logger))

	handlers := NewHandlers(q, logger)

	s := &Server{
		router:   router,
		handlers: handlers,
		logger:   logger,
	}

	s.setupRoutes(cfg)

	return s
}

// ServerConfig contains server configuration
type ServerConfig struct {
	GitHubWebhookSecret    string
	GitLabWebhookSecret    string
	BitbucketWebhookSecret string
	InternalAPIKey         string
	SwitchyardURL          string
	SwitchyardAPIKey       string
	PreviewsEnabled        bool
}

func (s *Server) setupRoutes(cfg *ServerConfig) {
	// Health check (no auth)
	s.router.GET("/health", s.handlers.HealthCheck)
	s.router.GET("/ready", s.handlers.HealthCheck)

	// Webhook endpoints (signature validation)
	webhooks := s.router.Group("/webhooks")
	{
		// GitHub webhook with preview environment integration
		githubHandler := webhook.NewGitHubHandlerWithConfig(&webhook.GitHubHandlerConfig{
			Secret:           cfg.GitHubWebhookSecret,
			SwitchyardURL:    cfg.SwitchyardURL,
			SwitchyardAPIKey: cfg.SwitchyardAPIKey,
			PreviewsEnabled:  cfg.PreviewsEnabled,
		}, s.logger)
		webhooks.POST("/github", githubHandler.Handle)

		// GitLab and Bitbucket handlers would go here
		// webhooks.POST("/gitlab", gitlabHandler.Handle)
		// webhooks.POST("/bitbucket", bitbucketHandler.Handle)
	}

	// Internal API (for Switchyard)
	internal := s.router.Group("/internal")
	if cfg.InternalAPIKey != "" {
		internal.Use(apiKeyAuth(cfg.InternalAPIKey))
	}
	{
		internal.POST("/enqueue", s.handlers.Enqueue)
	}

	// Admin API (authenticated)
	api := s.router.Group("/api/v1")
	// Add authentication middleware here in production
	{
		// Jobs
		api.GET("/jobs", s.handlers.ListJobs)
		api.GET("/jobs/:id", s.handlers.GetJob)
		api.GET("/jobs/:id/logs", s.handlers.StreamLogs)
		api.POST("/jobs/:id/cancel", s.handlers.CancelJob)
		api.POST("/jobs/:id/retry", s.handlers.RetryJob)

		// Workers
		api.GET("/workers", s.handlers.GetWorkers)

		// Stats
		api.GET("/stats", s.handlers.GetStats)
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
