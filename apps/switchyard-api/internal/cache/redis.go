package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// Prometheus metrics for Redis observability
var (
	redisPingLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "enclii_redis_ping_seconds",
			Help:    "Redis ping latency in seconds",
			Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
		},
		[]string{"status"},
	)
	redisConnectionState = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "enclii_redis_connected",
		Help: "Redis connection state (1=connected, 0=disconnected)",
	})
	redisReconnectTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "enclii_redis_reconnect_total",
			Help: "Total number of Redis reconnection attempts",
		},
		[]string{"status"},
	)
)

type CacheService interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) error
	Exists(ctx context.Context, keys ...string) (int64, error)
	Expire(ctx context.Context, key string, ttl time.Duration) error

	// Hash operations
	HGet(ctx context.Context, key, field string) (string, error)
	HSet(ctx context.Context, key string, values ...interface{}) error
	HDel(ctx context.Context, key string, fields ...string) error
	HExists(ctx context.Context, key, field string) (bool, error)

	// List operations
	LPush(ctx context.Context, key string, values ...interface{}) error
	RPop(ctx context.Context, key string) (string, error)
	LRange(ctx context.Context, key string, start, stop int64) ([]string, error)

	// Set operations for tags
	SAdd(ctx context.Context, key string, members ...interface{}) error
	SMembers(ctx context.Context, key string) ([]string, error)
	SIsMember(ctx context.Context, key string, member interface{}) (bool, error)

	// Pub/Sub
	Publish(ctx context.Context, channel string, message interface{}) error
	Subscribe(ctx context.Context, channels ...string) <-chan *redis.Message

	// Health check
	Ping(ctx context.Context) error

	// Cache invalidation
	InvalidatePattern(ctx context.Context, pattern string) error
	InvalidateTags(ctx context.Context, tags ...string) error

	// Session revocation (for SessionRevoker interface compatibility)
	RevokeSession(ctx context.Context, sessionID string, ttl time.Duration) error
	IsSessionRevoked(ctx context.Context, sessionID string) (bool, error)

	// Lifecycle
	Close() error
}

type RedisCache struct {
	client     *redis.Client
	config     *CacheConfig
	mu         sync.RWMutex  // Protects client during reconnection
	errorCount atomic.Int64  // Track application-level cache errors
	lastPing   time.Time     // Track last successful ping
	pingMu     sync.Mutex    // Protects lastPing updates
	defaultTTL time.Duration // Store for reference

	// FailMode controls behavior when Redis is unavailable for session revocation checks.
	// "closed" = treat sessions as revoked (deny access) - SOC 2 compliant
	// "open"   = treat sessions as not revoked (allow access) - backward compatible default
	FailMode string
}

type CacheConfig struct {
	Host         string
	Port         int
	Password     string
	DB           int
	MaxRetries   int
	PoolSize     int
	IdleTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	DefaultTTL   time.Duration

	// Sentinel configuration (for HA failover)
	// When SentinelEnabled is true, SentinelAddrs and SentinelMasterName are used
	// instead of Host/Port for connecting via Redis Sentinel.
	SentinelEnabled    bool
	SentinelAddrs      []string // e.g., ["redis-0:26379", "redis-1:26379", "redis-2:26379"]
	SentinelMasterName string   // e.g., "enclii-master"

	// SessionRevocationFailMode: "closed" (deny on Redis failure) or "open" (allow on Redis failure)
	SessionRevocationFailMode string
}

type CacheItem struct {
	Data      interface{} `json:"data"`
	CreatedAt time.Time   `json:"created_at"`
	TTL       int64       `json:"ttl"`
	Tags      []string    `json:"tags,omitempty"`
}

// Cache key patterns
const (
	ProjectCacheKey         = "project:%s"
	ServiceCacheKey         = "service:%s"
	ReleaseCacheKey         = "release:%s"
	DeploymentCacheKey      = "deployment:%s"
	UserCacheKey            = "user:%s"
	ProjectServicesCacheKey = "project:%s:services"
	ServiceReleasesCacheKey = "service:%s:releases"
	SessionRevokedKey       = "session:revoked:%s" // For JWT session revocation

	// Cache tags for invalidation
	ProjectTag    = "project"
	ServiceTag    = "service"
	ReleaseTag    = "release"
	DeploymentTag = "deployment"
	UserTag       = "user"

	// Cache TTL
	ShortTTL  = 5 * time.Minute
	MediumTTL = 30 * time.Minute
	LongTTL   = 2 * time.Hour
	DayTTL    = 24 * time.Hour
)

func NewRedisCache(config *CacheConfig) (*RedisCache, error) {
	var rdb *redis.Client

	if config.SentinelEnabled && len(config.SentinelAddrs) > 0 {
		// Sentinel mode - automatic failover to master
		logrus.WithFields(logrus.Fields{
			"sentinel_addrs": config.SentinelAddrs,
			"master_name":    config.SentinelMasterName,
		}).Info("Connecting to Redis via Sentinel (HA mode)")

		rdb = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:      config.SentinelMasterName,
			SentinelAddrs:   config.SentinelAddrs,
			Password:        config.Password,
			DB:              config.DB,
			MaxRetries:      config.MaxRetries,
			PoolSize:        config.PoolSize,
			ConnMaxIdleTime: config.IdleTimeout,
			ReadTimeout:     config.ReadTimeout,
			WriteTimeout:    config.WriteTimeout,
		})
	} else {
		// Standalone mode (default)
		logrus.WithFields(logrus.Fields{
			"host": config.Host,
			"port": config.Port,
		}).Info("Connecting to Redis (standalone mode)")

		rdb = redis.NewClient(&redis.Options{
			Addr:            fmt.Sprintf("%s:%d", config.Host, config.Port),
			Password:        config.Password,
			DB:              config.DB,
			MaxRetries:      config.MaxRetries,
			PoolSize:        config.PoolSize,
			ConnMaxIdleTime: config.IdleTimeout,
			ReadTimeout:     config.ReadTimeout,
			WriteTimeout:    config.WriteTimeout,
		})
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	if config.SentinelEnabled {
		logrus.Info("Connected to Redis cache (Sentinel HA mode)")
	} else {
		logrus.Info("Connected to Redis cache (standalone mode)")
	}

	failMode := config.SessionRevocationFailMode
	if failMode == "" {
		failMode = "open" // backward compatible default
	}

	return &RedisCache{
		client:     rdb,
		config:     config,
		defaultTTL: config.DefaultTTL,
		lastPing:   time.Now(),
		FailMode:   failMode,
	}, nil
}

func (r *RedisCache) Get(ctx context.Context, key string) ([]byte, error) {
	val, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, ErrCacheMiss
		}
		return nil, fmt.Errorf("failed to get from cache: %w", err)
	}

	return []byte(val), nil
}

func (r *RedisCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	var data []byte
	var err error

	switch v := value.(type) {
	case string:
		data = []byte(v)
	case []byte:
		data = v
	default:
		data, err = json.Marshal(value)
		if err != nil {
			return fmt.Errorf("failed to marshal value: %w", err)
		}
	}

	if ttl == 0 {
		ttl = r.config.DefaultTTL
	}

	return r.client.Set(ctx, key, data, ttl).Err()
}

func (r *RedisCache) Del(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	return r.client.Del(ctx, keys...).Err()
}

func (r *RedisCache) Exists(ctx context.Context, keys ...string) (int64, error) {
	return r.client.Exists(ctx, keys...).Result()
}

func (r *RedisCache) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return r.client.Expire(ctx, key, ttl).Err()
}

// Hash operations
func (r *RedisCache) HGet(ctx context.Context, key, field string) (string, error) {
	return r.client.HGet(ctx, key, field).Result()
}

func (r *RedisCache) HSet(ctx context.Context, key string, values ...interface{}) error {
	return r.client.HSet(ctx, key, values...).Err()
}

func (r *RedisCache) HDel(ctx context.Context, key string, fields ...string) error {
	return r.client.HDel(ctx, key, fields...).Err()
}

func (r *RedisCache) HExists(ctx context.Context, key, field string) (bool, error) {
	return r.client.HExists(ctx, key, field).Result()
}

// List operations
func (r *RedisCache) LPush(ctx context.Context, key string, values ...interface{}) error {
	return r.client.LPush(ctx, key, values...).Err()
}

func (r *RedisCache) RPop(ctx context.Context, key string) (string, error) {
	return r.client.RPop(ctx, key).Result()
}

func (r *RedisCache) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return r.client.LRange(ctx, key, start, stop).Result()
}

// Set operations
func (r *RedisCache) SAdd(ctx context.Context, key string, members ...interface{}) error {
	return r.client.SAdd(ctx, key, members...).Err()
}

func (r *RedisCache) SMembers(ctx context.Context, key string) ([]string, error) {
	return r.client.SMembers(ctx, key).Result()
}

func (r *RedisCache) SIsMember(ctx context.Context, key string, member interface{}) (bool, error) {
	return r.client.SIsMember(ctx, key, member).Result()
}

// Pub/Sub operations
func (r *RedisCache) Publish(ctx context.Context, channel string, message interface{}) error {
	var data []byte
	var err error

	switch v := message.(type) {
	case string:
		data = []byte(v)
	case []byte:
		data = v
	default:
		data, err = json.Marshal(message)
		if err != nil {
			return fmt.Errorf("failed to marshal message: %w", err)
		}
	}

	return r.client.Publish(ctx, channel, data).Err()
}

func (r *RedisCache) Subscribe(ctx context.Context, channels ...string) <-chan *redis.Message {
	pubsub := r.client.Subscribe(ctx, channels...)
	return pubsub.Channel()
}

// Ping checks Redis connectivity with automatic reconnection on failure.
// If the connection is stale, it attempts to reconnect before failing.
// Records Prometheus metrics for observability.
func (r *RedisCache) Ping(ctx context.Context) error {
	if r == nil {
		redisConnectionState.Set(0)
		return fmt.Errorf("redis client not initialized")
	}

	start := time.Now()

	r.mu.RLock()
	client := r.client
	r.mu.RUnlock()

	if client == nil {
		// Try to reconnect
		if err := r.reconnect(); err != nil {
			redisConnectionState.Set(0)
			redisPingLatency.WithLabelValues("error").Observe(time.Since(start).Seconds())
			return fmt.Errorf("redis reconnection failed: %w", err)
		}
		r.mu.RLock()
		client = r.client
		r.mu.RUnlock()
	}

	err := client.Ping(ctx).Err()
	if err != nil {
		// Connection might be stale after Redis restart, try reconnection
		logrus.WithError(err).Warn("Redis ping failed, attempting reconnection")
		if reconnErr := r.reconnect(); reconnErr == nil {
			r.mu.RLock()
			client = r.client
			r.mu.RUnlock()
			err = client.Ping(ctx).Err()
		}
	}

	// Record metrics
	duration := time.Since(start).Seconds()
	if err == nil {
		r.pingMu.Lock()
		r.lastPing = time.Now()
		r.pingMu.Unlock()
		redisConnectionState.Set(1)
		redisPingLatency.WithLabelValues("success").Observe(duration)
	} else {
		redisConnectionState.Set(0)
		redisPingLatency.WithLabelValues("error").Observe(duration)
	}

	return err
}

// reconnect attempts to re-establish the Redis connection.
// This is used when the connection becomes stale (e.g., after Redis pod restart).
func (r *RedisCache) reconnect() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Close existing client if any (ignore errors on close)
	if r.client != nil {
		_ = r.client.Close()
	}

	var rdb *redis.Client
	if r.config.SentinelEnabled && len(r.config.SentinelAddrs) > 0 {
		// Sentinel mode - automatic failover to master
		rdb = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:      r.config.SentinelMasterName,
			SentinelAddrs:   r.config.SentinelAddrs,
			Password:        r.config.Password,
			DB:              r.config.DB,
			MaxRetries:      r.config.MaxRetries,
			PoolSize:        r.config.PoolSize,
			ConnMaxIdleTime: r.config.IdleTimeout,
			ReadTimeout:     r.config.ReadTimeout,
			WriteTimeout:    r.config.WriteTimeout,
		})
	} else {
		// Standalone mode
		rdb = redis.NewClient(&redis.Options{
			Addr:            fmt.Sprintf("%s:%d", r.config.Host, r.config.Port),
			Password:        r.config.Password,
			DB:              r.config.DB,
			MaxRetries:      r.config.MaxRetries,
			PoolSize:        r.config.PoolSize,
			ConnMaxIdleTime: r.config.IdleTimeout,
			ReadTimeout:     r.config.ReadTimeout,
			WriteTimeout:    r.config.WriteTimeout,
		})
	}

	// Test new connection with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		redisReconnectTotal.WithLabelValues("failure").Inc()
		return fmt.Errorf("reconnection ping failed: %w", err)
	}

	r.client = rdb
	redisReconnectTotal.WithLabelValues("success").Inc()
	logrus.Info("Redis connection re-established")
	return nil
}

// Cache invalidation
func (r *RedisCache) InvalidatePattern(ctx context.Context, pattern string) error {
	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil {
		return fmt.Errorf("failed to get keys by pattern: %w", err)
	}

	if len(keys) > 0 {
		return r.Del(ctx, keys...)
	}

	return nil
}

func (r *RedisCache) InvalidateTags(ctx context.Context, tags ...string) error {
	for _, tag := range tags {
		tagKey := fmt.Sprintf("tag:%s", tag)
		keys, err := r.SMembers(ctx, tagKey)
		if err != nil {
			logrus.Errorf("Failed to get keys for tag %s: %v", tag, err)
			continue
		}

		if len(keys) > 0 {
			if err := r.Del(ctx, keys...); err != nil {
				logrus.Errorf("Failed to delete keys for tag %s: %v", tag, err)
			}
		}

		// Clean up the tag set
		r.Del(ctx, tagKey)
	}

	return nil
}

// Helper methods for caching with tags
func (r *RedisCache) SetWithTags(ctx context.Context, key string, value interface{}, ttl time.Duration, tags ...string) error {
	// Set the main cache item
	if err := r.Set(ctx, key, value, ttl); err != nil {
		return err
	}

	// Add to tag sets for invalidation
	for _, tag := range tags {
		tagKey := fmt.Sprintf("tag:%s", tag)
		if err := r.SAdd(ctx, tagKey, key); err != nil {
			logrus.Errorf("Failed to add key %s to tag %s: %v", key, tag, err)
		}
	}

	return nil
}

// Cached operation wrapper
func (r *RedisCache) GetOrSet(ctx context.Context, key string, ttl time.Duration, fetchFunc func() (interface{}, error)) ([]byte, error) {
	// Try to get from cache first
	data, err := r.Get(ctx, key)
	if err == nil {
		return data, nil
	}

	if err != ErrCacheMiss {
		logrus.Errorf("Cache get error for key %s: %v", key, err)
	}

	// Cache miss, fetch from source
	value, err := fetchFunc()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch data: %w", err)
	}

	// Store in cache
	if err := r.Set(ctx, key, value, ttl); err != nil {
		logrus.Errorf("Failed to set cache for key %s: %v", key, err)
		// Don't fail the request if cache set fails
	}

	// Return the data
	data, err = json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal fetched data: %w", err)
	}

	return data, nil
}

// SessionRevoker implementation for JWT session revocation
// RevokeSession marks a session as revoked in Redis with the specified TTL.
// The TTL should match the longest-lived token duration (typically refresh token duration).
func (r *RedisCache) RevokeSession(ctx context.Context, sessionID string, ttl time.Duration) error {
	// Guard against nil receiver (Go interface nil gotcha)
	if r == nil || r.client == nil {
		logrus.Warn("RevokeSession called with nil cache client, skipping")
		return fmt.Errorf("cache not available")
	}

	key := fmt.Sprintf(SessionRevokedKey, sessionID)

	// Set a marker in Redis with TTL matching token expiration
	// Value doesn't matter, just the existence of the key
	err := r.client.Set(ctx, key, "revoked", ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to revoke session in Redis: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"session_id": sessionID,
		"ttl":        ttl.String(),
	}).Info("Session revoked in cache")

	return nil
}

// IsSessionRevoked checks if a session has been revoked.
// Returns true if the session is revoked, false otherwise.
// Behavior when Redis is unavailable is controlled by FailMode:
//   - "closed": treat as revoked (deny access) - SOC 2 compliant
//   - "open":   treat as not revoked (allow access) - backward compatible
func (r *RedisCache) IsSessionRevoked(ctx context.Context, sessionID string) (bool, error) {
	failClosed := r != nil && r.FailMode == "closed"

	// Guard against nil receiver (Go interface nil gotcha)
	if r == nil || r.client == nil {
		if failClosed {
			logrus.Warn("IsSessionRevoked: cache unavailable, fail-closed — denying access")
			return true, nil
		}
		logrus.Debug("IsSessionRevoked called with nil cache client, assuming not revoked (fail-open)")
		return false, nil
	}

	key := fmt.Sprintf(SessionRevokedKey, sessionID)

	// Check if the key exists in Redis
	exists, err := r.client.Exists(ctx, key).Result()
	if err != nil {
		if failClosed {
			logrus.WithError(err).Warn("IsSessionRevoked: Redis error, fail-closed — denying access")
			return true, fmt.Errorf("failed to check session revocation: %w", err)
		}
		return false, fmt.Errorf("failed to check session revocation: %w", err)
	}

	return exists > 0, nil
}

// Close connection
func (r *RedisCache) Close() error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.Close()
}

// Errors
var (
	ErrCacheMiss = fmt.Errorf("cache miss")
)

// Default configuration
func DefaultCacheConfig() *CacheConfig {
	return &CacheConfig{
		Host:         "localhost",
		Port:         6379,
		Password:     "",
		DB:           0,
		MaxRetries:   3,
		PoolSize:     10,
		IdleTimeout:  300 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		DefaultTTL:   MediumTTL,
	}
}

// Cache key builders
func ProjectKey(projectID string) string {
	return fmt.Sprintf(ProjectCacheKey, projectID)
}

func ServiceKey(serviceID string) string {
	return fmt.Sprintf(ServiceCacheKey, serviceID)
}

func ReleaseKey(releaseID string) string {
	return fmt.Sprintf(ReleaseCacheKey, releaseID)
}

func DeploymentKey(deploymentID string) string {
	return fmt.Sprintf(DeploymentCacheKey, deploymentID)
}

func UserKey(userID string) string {
	return fmt.Sprintf(UserCacheKey, userID)
}

func ProjectServicesKey(projectID string) string {
	return fmt.Sprintf(ProjectServicesCacheKey, projectID)
}

func ServiceReleasesKey(serviceID string) string {
	return fmt.Sprintf(ServiceReleasesCacheKey, serviceID)
}

// Cache metrics (for monitoring)
type CacheMetrics struct {
	Hits   int64
	Misses int64
	Errors int64
}

func (r *RedisCache) GetMetrics(ctx context.Context) (*CacheMetrics, error) {
	info, err := r.client.Info(ctx, "stats").Result()
	if err != nil {
		r.errorCount.Add(1)
		return nil, err
	}

	metrics := &CacheMetrics{
		Hits:   parseRedisInfoInt(info, "keyspace_hits"),
		Misses: parseRedisInfoInt(info, "keyspace_misses"),
		Errors: r.errorCount.Load(),
	}

	return metrics, nil
}

// parseRedisInfoInt extracts an integer value from Redis INFO output
func parseRedisInfoInt(info, key string) int64 {
	for _, line := range strings.Split(info, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, key+":") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				val, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
				if err == nil {
					return val
				}
			}
		}
	}
	return 0
}

// IncrementErrorCount increments the application error counter
func (r *RedisCache) IncrementErrorCount() {
	r.errorCount.Add(1)
}
