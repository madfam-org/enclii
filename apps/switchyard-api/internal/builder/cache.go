package builder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// BuildCache manages build layer caching for faster rebuilds
type BuildCache struct {
	registry      string // Container registry for cache images
	cachePrefix   string // Prefix for cache image tags
	r2Bucket      string // R2 bucket for cache metadata
	r2Client      R2Uploader
	localCacheDir string
}

// R2Uploader interface for R2 storage operations
type R2Uploader interface {
	UploadFile(ctx context.Context, localPath, r2Key string) error
	DownloadFile(ctx context.Context, r2Key, localPath string) error
	DeleteObject(ctx context.Context, key string) error
	ListObjects(ctx context.Context, prefix string) ([]string, error)
}

// CacheKey represents a unique build cache identifier
type CacheKey struct {
	ProjectID   string    `json:"project_id"`
	ServiceName string    `json:"service_name"`
	DepsHash    string    `json:"deps_hash"`    // Hash of dependency files
	BuilderHash string    `json:"builder_hash"` // Hash of builder configuration
	GeneratedAt time.Time `json:"generated_at"`
}

// CacheMetadata stores cache hit information
type CacheMetadata struct {
	Key        CacheKey  `json:"key"`
	CacheImage string    `json:"cache_image"`
	HitCount   int       `json:"hit_count"`
	LastHit    time.Time `json:"last_hit"`
	SizeBytes  int64     `json:"size_bytes"`
	CreatedAt  time.Time `json:"created_at"`
}

// CacheStats tracks cache performance
type CacheStats struct {
	Hits       int           `json:"hits"`
	Misses     int           `json:"misses"`
	HitRate    float64       `json:"hit_rate"`
	TotalSaved time.Duration `json:"total_time_saved"`
}

// NewBuildCache creates a new build cache manager
func NewBuildCache(registry, cachePrefix string, r2Client R2Uploader, localCacheDir string) *BuildCache {
	return &BuildCache{
		registry:      registry,
		cachePrefix:   cachePrefix,
		r2Client:      r2Client,
		localCacheDir: localCacheDir,
	}
}

// GenerateCacheKey creates a unique cache key based on project dependencies
func (c *BuildCache) GenerateCacheKey(ctx context.Context, projectID, serviceName, sourcePath string) (*CacheKey, error) {
	// Calculate hash of dependency files
	depsHash, err := c.hashDependencyFiles(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("failed to hash dependencies: %w", err)
	}

	// Calculate hash of builder config (if exists)
	builderHash, err := c.hashBuilderConfig(sourcePath)
	if err != nil {
		// Non-fatal - use empty hash if no builder config
		builderHash = "default"
	}

	return &CacheKey{
		ProjectID:   projectID,
		ServiceName: serviceName,
		DepsHash:    depsHash,
		BuilderHash: builderHash,
		GeneratedAt: time.Now(),
	}, nil
}

// hashDependencyFiles creates a hash of all dependency definition files
func (c *BuildCache) hashDependencyFiles(sourcePath string) (string, error) {
	// Dependency files to consider for cache key
	depFiles := []string{
		"package.json",
		"package-lock.json",
		"yarn.lock",
		"pnpm-lock.yaml",
		"go.mod",
		"go.sum",
		"requirements.txt",
		"Pipfile.lock",
		"poetry.lock",
		"Gemfile.lock",
		"pom.xml",
		"build.gradle",
		"Cargo.lock",
	}

	hasher := sha256.New()
	foundFiles := []string{}

	for _, depFile := range depFiles {
		filePath := filepath.Clean(filepath.Join(sourcePath, depFile))
		if _, err := os.Stat(filePath); err == nil {
			content, err := os.ReadFile(filePath)
			if err != nil {
				continue
			}
			hasher.Write([]byte(depFile))
			hasher.Write(content)
			foundFiles = append(foundFiles, depFile)
		}
	}

	if len(foundFiles) == 0 {
		// No dependency files found, use timestamp to force rebuild
		hasher.Write([]byte(time.Now().String()))
	}

	logrus.Debugf("Cache key based on files: %v", foundFiles)
	return hex.EncodeToString(hasher.Sum(nil))[:16], nil
}

// hashBuilderConfig creates a hash of builder configuration
func (c *BuildCache) hashBuilderConfig(sourcePath string) (string, error) {
	configFiles := []string{
		"enclii.yaml",
		"enclii.yml",
		".enclii.yaml",
		"Dockerfile",
		"project.toml", // Buildpacks config
	}

	hasher := sha256.New()
	found := false

	for _, configFile := range configFiles {
		filePath := filepath.Clean(filepath.Join(sourcePath, configFile))
		if _, err := os.Stat(filePath); err == nil {
			content, err := os.ReadFile(filePath)
			if err != nil {
				continue
			}
			hasher.Write([]byte(configFile))
			hasher.Write(content)
			found = true
		}
	}

	if !found {
		return "default", nil
	}

	return hex.EncodeToString(hasher.Sum(nil))[:8], nil
}

// GetCacheImage returns the cache image URI for buildpacks
func (c *BuildCache) GetCacheImage(key *CacheKey) string {
	// Format: registry/cache-prefix:project-service-depshash
	cacheTag := fmt.Sprintf("%s-%s-%s", key.ProjectID[:8], key.ServiceName, key.DepsHash[:8])
	return fmt.Sprintf("%s/%s:%s", c.registry, c.cachePrefix, cacheTag)
}

// LookupCache checks if a valid cache exists for the given key
func (c *BuildCache) LookupCache(ctx context.Context, key *CacheKey) (*CacheMetadata, error) {
	metadataKey := c.getMetadataKey(key)

	// Check R2 for cache metadata
	localPath := filepath.Clean(filepath.Join(c.localCacheDir, "metadata", metadataKey+".json"))
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return nil, err
	}

	err := c.r2Client.DownloadFile(ctx, "build-cache/"+metadataKey+".json", localPath)
	if err != nil {
		// Cache miss
		logrus.Debugf("Cache miss for key %s: %v", metadataKey, err)
		return nil, nil
	}

	// Read and parse metadata
	data, err := os.ReadFile(localPath)
	if err != nil {
		return nil, err
	}

	var metadata CacheMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, err
	}

	// Update hit count
	metadata.HitCount++
	metadata.LastHit = time.Now()

	logrus.Infof("Cache hit for %s (hits: %d)", key.ServiceName, metadata.HitCount)
	return &metadata, nil
}

// SaveCacheMetadata stores cache metadata to R2
func (c *BuildCache) SaveCacheMetadata(ctx context.Context, metadata *CacheMetadata) error {
	metadataKey := c.getMetadataKey(&metadata.Key)

	// Serialize metadata
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}

	// Write to local file
	localPath := filepath.Join(c.localCacheDir, "metadata", metadataKey+".json")
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(localPath, data, 0644); err != nil {
		return err
	}

	// Upload to R2
	err = c.r2Client.UploadFile(ctx, localPath, "build-cache/"+metadataKey+".json")
	if err != nil {
		return fmt.Errorf("failed to upload cache metadata: %w", err)
	}

	logrus.Infof("Saved cache metadata for %s", metadata.Key.ServiceName)
	return nil
}

// getMetadataKey generates a unique key for cache metadata storage
func (c *BuildCache) getMetadataKey(key *CacheKey) string {
	return fmt.Sprintf("%s-%s-%s", key.ProjectID, key.ServiceName, key.DepsHash)
}

// CleanupOldCaches removes caches older than maxAge
func (c *BuildCache) CleanupOldCaches(ctx context.Context, projectID string, maxAge time.Duration) (int, error) {
	// List all cache metadata for this project
	prefix := fmt.Sprintf("build-cache/%s-", projectID)
	objects, err := c.r2Client.ListObjects(ctx, prefix)
	if err != nil {
		return 0, err
	}

	deleted := 0
	cutoff := time.Now().Add(-maxAge)

	for _, obj := range objects {
		// Download metadata to check age
		localPath := filepath.Clean(filepath.Join(c.localCacheDir, "cleanup", filepath.Base(obj)))
		if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
			continue
		}

		if err := c.r2Client.DownloadFile(ctx, obj, localPath); err != nil {
			continue
		}

		data, err := os.ReadFile(localPath)
		if err != nil {
			continue
		}

		var metadata CacheMetadata
		if err := json.Unmarshal(data, &metadata); err != nil {
			continue
		}

		// Check if cache is old and unused
		if metadata.LastHit.Before(cutoff) || metadata.CreatedAt.Before(cutoff) {
			if err := c.r2Client.DeleteObject(ctx, obj); err != nil {
				logrus.Warnf("Failed to delete old cache %s: %v", obj, err)
				continue
			}
			deleted++
			logrus.Infof("Deleted old cache: %s (last hit: %v)", obj, metadata.LastHit)
		}
	}

	return deleted, nil
}

// GetCacheStats returns cache performance statistics for a project
func (c *BuildCache) GetCacheStats(ctx context.Context, projectID string) (*CacheStats, error) {
	prefix := fmt.Sprintf("build-cache/%s-", projectID)
	objects, err := c.r2Client.ListObjects(ctx, prefix)
	if err != nil {
		return nil, err
	}

	stats := &CacheStats{}
	for _, obj := range objects {
		localPath := filepath.Clean(filepath.Join(c.localCacheDir, "stats", filepath.Base(obj)))
		if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
			continue
		}

		if err := c.r2Client.DownloadFile(ctx, obj, localPath); err != nil {
			continue
		}

		data, err := os.ReadFile(localPath)
		if err != nil {
			continue
		}

		var metadata CacheMetadata
		if err := json.Unmarshal(data, &metadata); err != nil {
			continue
		}

		stats.Hits += metadata.HitCount
	}

	// Calculate hit rate (assuming each miss creates a new cache entry)
	if stats.Hits > 0 || len(objects) > 0 {
		stats.Misses = len(objects) // Each cache entry represents one initial miss
		total := stats.Hits + stats.Misses
		if total > 0 {
			stats.HitRate = float64(stats.Hits) / float64(total)
		}
	}

	// Estimate time saved (average 2 minutes per cache hit)
	stats.TotalSaved = time.Duration(stats.Hits) * 2 * time.Minute

	return stats, nil
}

// BuildCacheConfig configures cache behavior
type BuildCacheConfig struct {
	Enabled       bool          `json:"enabled"`
	MaxCacheAge   time.Duration `json:"max_cache_age"`
	CacheImageTTL time.Duration `json:"cache_image_ttl"`
}

// DefaultBuildCacheConfig returns default cache configuration
func DefaultBuildCacheConfig() BuildCacheConfig {
	return BuildCacheConfig{
		Enabled:       true,
		MaxCacheAge:   7 * 24 * time.Hour,  // 7 days
		CacheImageTTL: 30 * 24 * time.Hour, // 30 days
	}
}

// MonorepoCache handles caching for monorepo projects
type MonorepoCache struct {
	*BuildCache
	pathFilters map[string][]string // service -> watched paths
}

// NewMonorepoCache creates a cache manager for monorepo projects
func NewMonorepoCache(cache *BuildCache) *MonorepoCache {
	return &MonorepoCache{
		BuildCache:  cache,
		pathFilters: make(map[string][]string),
	}
}

// SetPathFilter configures which paths affect a service's cache
func (m *MonorepoCache) SetPathFilter(serviceName string, paths []string) {
	m.pathFilters[serviceName] = paths
}

// ShouldRebuild checks if a service needs rebuilding based on changed files
func (m *MonorepoCache) ShouldRebuild(serviceName string, changedFiles []string) bool {
	filters, ok := m.pathFilters[serviceName]
	if !ok {
		// No filters configured, always rebuild
		return true
	}

	for _, changed := range changedFiles {
		for _, filter := range filters {
			if matchesPathFilter(changed, filter) {
				logrus.Infof("Service %s needs rebuild due to change in %s (matches %s)",
					serviceName, changed, filter)
				return true
			}
		}
	}

	logrus.Infof("Service %s can skip rebuild - no relevant file changes", serviceName)
	return false
}

// matchesPathFilter checks if a file path matches a filter pattern
func matchesPathFilter(filePath, filter string) bool {
	// Handle glob patterns
	if strings.Contains(filter, "*") {
		matched, _ := filepath.Match(filter, filePath)
		if matched {
			return true
		}
		// Try matching just the filename
		matched, _ = filepath.Match(filter, filepath.Base(filePath))
		return matched
	}

	// Handle prefix/directory matching
	if strings.HasSuffix(filter, "/") {
		return strings.HasPrefix(filePath, filter)
	}

	// Exact match or prefix
	return filePath == filter || strings.HasPrefix(filePath, filter+"/")
}

// GenerateMonorepoCacheKey creates a cache key considering only relevant paths
func (m *MonorepoCache) GenerateMonorepoCacheKey(ctx context.Context, projectID, serviceName, sourcePath string, watchPaths []string) (*CacheKey, error) {
	// Calculate hash only from watched paths
	depsHash, err := m.hashWatchedPaths(sourcePath, watchPaths)
	if err != nil {
		return nil, err
	}

	builderHash, _ := m.hashBuilderConfig(sourcePath)

	return &CacheKey{
		ProjectID:   projectID,
		ServiceName: serviceName,
		DepsHash:    depsHash,
		BuilderHash: builderHash,
		GeneratedAt: time.Now(),
	}, nil
}

// hashWatchedPaths creates a hash of files within watched paths
func (m *MonorepoCache) hashWatchedPaths(sourcePath string, watchPaths []string) (string, error) {
	hasher := sha256.New()

	// Sort paths for consistent hashing
	sort.Strings(watchPaths)

	for _, watchPath := range watchPaths {
		fullPath := filepath.Join(sourcePath, watchPath)

		// Walk directory and hash all files
		err := filepath.Walk(fullPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // Skip inaccessible files
			}
			if info.IsDir() {
				return nil
			}

			// Skip common non-source files
			if shouldSkipFile(path) {
				return nil
			}

			path = filepath.Clean(path)
			content, err := os.ReadFile(path)
			if err != nil {
				return nil
			}

			relPath, _ := filepath.Rel(sourcePath, path)
			hasher.Write([]byte(relPath))
			hasher.Write(content)
			return nil
		})

		if err != nil && !os.IsNotExist(err) {
			logrus.Warnf("Error walking path %s: %v", watchPath, err)
		}
	}

	return hex.EncodeToString(hasher.Sum(nil))[:16], nil
}

// shouldSkipFile returns true for files that shouldn't affect the cache
func shouldSkipFile(path string) bool {
	skipPatterns := []string{
		"node_modules/",
		".git/",
		"dist/",
		"build/",
		".next/",
		"__pycache__/",
		".pytest_cache/",
		"target/",
		"vendor/",
		".DS_Store",
		"*.log",
		"*.tmp",
	}

	for _, pattern := range skipPatterns {
		if strings.Contains(path, pattern) {
			return true
		}
	}
	return false
}

// CopyFile is a utility to copy files (for cache operations)
func CopyFile(src, dst string) error {
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}
