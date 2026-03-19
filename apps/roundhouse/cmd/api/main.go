package main

import (
	"log"
	"os"

	"github.com/madfam-org/enclii/apps/roundhouse/internal/api"
	"github.com/madfam-org/enclii/apps/roundhouse/internal/config"
	"github.com/madfam-org/enclii/apps/roundhouse/internal/queue"
	"go.uber.org/zap"
)

func main() {
	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("failed to initialize logger: %v", err)
	}
	defer func() { _ = logger.Sync() }()

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		logger.Fatal("failed to load config", zap.Error(err))
	}

	// Validate required config
	if cfg.RedisURL == "" {
		logger.Fatal("REDIS_URL or REDIS_HOST is required")
	}

	// Initialize Redis queue
	redisQueue, err := queue.NewRedisQueue(cfg.RedisURL, logger)
	if err != nil {
		logger.Fatal("failed to connect to Redis", zap.Error(err))
	}
	defer func() { _ = redisQueue.Close() }()

	logger.Info("connected to Redis")

	// Create API server
	server := api.NewServer(redisQueue, &api.ServerConfig{
		GitHubWebhookSecret:    cfg.GitHubWebhookSecret,
		GitLabWebhookSecret:    cfg.GitLabWebhookSecret,
		BitbucketWebhookSecret: cfg.BitbucketWebhookSecret,
		InternalAPIKey:         cfg.SwitchyardAPIKey,
	}, logger)

	// Start server
	addr := ":" + cfg.APIPort
	logger.Info("starting Roundhouse API",
		zap.String("port", cfg.APIPort),
	)

	if err := server.Run(addr); err != nil {
		logger.Fatal("server failed", zap.Error(err))
		os.Exit(1)
	}
}
