package main

import (
	"math/rand"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/cache"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
)

// initRedisWithRetry attempts to connect to Redis with exponential backoff.
// This handles the K8s startup timing race condition where Redis may not be
// ready when the API pod starts. Returns nil if connection ultimately fails
// (cache will be disabled, but API continues working).
func initRedisWithRetry(cfg *config.Config) cache.CacheService {
	cacheConfig := &cache.CacheConfig{
		Host:                      cfg.RedisHost,
		Port:                      cfg.RedisPort,
		Password:                  cfg.RedisPassword,
		DB:                        0,
		MaxRetries:                3,
		PoolSize:                  10,
		IdleTimeout:               5 * time.Minute,
		ReadTimeout:               3 * time.Second,
		WriteTimeout:              3 * time.Second,
		DefaultTTL:                cache.MediumTTL,
		SentinelEnabled:           cfg.RedisSentinelEnabled,
		SentinelAddrs:             cfg.RedisSentinelAddrs,
		SentinelMasterName:        cfg.RedisSentinelMasterName,
		SessionRevocationFailMode: cfg.SessionRevocationFailMode,
	}

	// Retry configuration
	const maxRetries = 5
	baseDelay := 2 * time.Second
	maxDelay := 30 * time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		cs, err := cache.NewRedisCache(cacheConfig)
		if err == nil {
			logrus.Infof("Redis connected successfully on attempt %d", attempt)
			return cs
		}

		if attempt == maxRetries {
			logrus.Warnf("Redis connection failed after %d attempts: %v (cache disabled)", maxRetries, err)
			return nil
		}

		// Calculate delay with exponential backoff: 2s, 4s, 8s, 16s, 30s (capped)
		delay := baseDelay * time.Duration(1<<(attempt-1))
		if delay > maxDelay {
			delay = maxDelay
		}
		// Add 10% jitter to prevent thundering herd when multiple pods restart
		jitter := time.Duration(float64(delay) * 0.1 * rand.Float64()) // #nosec G404 -- jitter, not security
		delay += jitter

		logrus.Warnf("Redis connection attempt %d/%d failed: %v (retrying in %v)",
			attempt, maxRetries, err, delay.Round(time.Millisecond))

		time.Sleep(delay)
	}
	return nil
}
