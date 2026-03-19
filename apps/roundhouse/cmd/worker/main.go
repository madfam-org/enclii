package main

import (
	"context"
	"log"
	"os"

	"github.com/madfam-org/enclii/apps/roundhouse/internal/config"
	"github.com/madfam-org/enclii/apps/roundhouse/internal/queue"
	"github.com/madfam-org/enclii/apps/roundhouse/internal/worker"
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
	if cfg.Registry == "" {
		logger.Fatal("REGISTRY is required")
	}

	// Initialize Redis queue
	redisQueue, err := queue.NewRedisQueue(cfg.RedisURL, logger)
	if err != nil {
		logger.Fatal("failed to connect to Redis", zap.Error(err))
	}
	defer func() { _ = redisQueue.Close() }()

	logger.Info("connected to Redis")

	// Ensure build work directory exists (only needed for Docker mode)
	if cfg.BuildMode == "docker" {
		if err := os.MkdirAll(cfg.BuildWorkDir, 0755); err != nil {
			logger.Fatal("failed to create build work directory", zap.Error(err))
		}
	}

	// Create processor with appropriate builder
	processor, err := worker.NewProcessor(cfg, redisQueue, logger)
	if err != nil {
		logger.Fatal("failed to create processor", zap.Error(err))
	}

	logger.Info("starting Roundhouse worker",
		zap.String("build_mode", cfg.BuildMode),
		zap.String("work_dir", cfg.BuildWorkDir),
		zap.Int("max_concurrent", cfg.MaxConcurrentBuilds),
		zap.Duration("timeout", cfg.BuildTimeout),
	)

	ctx := context.Background()
	if err := processor.Start(ctx); err != nil {
		logger.Fatal("worker failed", zap.Error(err))
		os.Exit(1)
	}

	logger.Info("worker shutdown complete")
}
