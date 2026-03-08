package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/sirupsen/logrus"
)

// R2Client provides Cloudflare R2 object storage operations
// R2 is S3-compatible, so we use the AWS SDK with custom endpoint
type R2Client struct {
	client    *s3.Client
	bucket    string
	accountID string
	presigner *s3.PresignClient
}

// R2Config holds configuration for Cloudflare R2
type R2Config struct {
	AccountID       string
	AccessKeyID     string
	AccessKeySecret string
	BucketName      string
	// Optional: custom endpoint for testing
	Endpoint string
}

// NewR2Client creates a new Cloudflare R2 storage client
func NewR2Client(ctx context.Context, cfg *R2Config) (*R2Client, error) {
	if cfg.AccountID == "" || cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" {
		return nil, fmt.Errorf("R2 configuration incomplete: accountID, accessKeyID, and accessKeySecret are required")
	}

	// R2 endpoint format: https://<account_id>.r2.cloudflarestorage.com
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = fmt.Sprintf("https://%s.r2.cloudflarestorage.com", cfg.AccountID)
	}

	// Create custom credentials provider
	creds := credentials.NewStaticCredentialsProvider(
		cfg.AccessKeyID,
		cfg.AccessKeySecret,
		"", // session token not used for R2
	)

	// Load AWS config with custom settings for R2
	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithCredentialsProvider(creds),
		config.WithRegion("auto"), // R2 uses "auto" region
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create S3 client with R2 endpoint
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true // R2 requires path-style addressing
	})

	presigner := s3.NewPresignClient(client)

	logrus.WithFields(logrus.Fields{
		"endpoint": endpoint,
		"bucket":   cfg.BucketName,
	}).Info("R2 storage client initialized")

	return &R2Client{
		client:    client,
		bucket:    cfg.BucketName,
		accountID: cfg.AccountID,
		presigner: presigner,
	}, nil
}

// Upload uploads an object to R2
func (r *R2Client) Upload(ctx context.Context, key string, body io.Reader, contentType string) error {
	input := &s3.PutObjectInput{
		Bucket:      aws.String(r.bucket),
		Key:         aws.String(key),
		Body:        body,
		ContentType: aws.String(contentType),
	}

	_, err := r.client.PutObject(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to upload object to R2: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"bucket": r.bucket,
		"key":    key,
	}).Debug("Object uploaded to R2")

	return nil
}

// Download downloads an object from R2
func (r *R2Client) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	input := &s3.GetObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(key),
	}

	output, err := r.client.GetObject(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to download object from R2: %w", err)
	}

	return output.Body, nil
}

// Delete deletes an object from R2
func (r *R2Client) Delete(ctx context.Context, key string) error {
	input := &s3.DeleteObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(key),
	}

	_, err := r.client.DeleteObject(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to delete object from R2: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"bucket": r.bucket,
		"key":    key,
	}).Debug("Object deleted from R2")

	return nil
}

// List lists objects in R2 with optional prefix
func (r *R2Client) List(ctx context.Context, prefix string, maxKeys int32) ([]ObjectInfo, error) {
	input := &s3.ListObjectsV2Input{
		Bucket:  aws.String(r.bucket),
		Prefix:  aws.String(prefix),
		MaxKeys: aws.Int32(maxKeys),
	}

	output, err := r.client.ListObjectsV2(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to list objects in R2: %w", err)
	}

	var objects []ObjectInfo
	for _, obj := range output.Contents {
		objects = append(objects, ObjectInfo{
			Key:          aws.ToString(obj.Key),
			Size:         aws.ToInt64(obj.Size),
			LastModified: aws.ToTime(obj.LastModified),
			ETag:         aws.ToString(obj.ETag),
		})
	}

	return objects, nil
}

// GetPresignedURL generates a presigned URL for temporary access
func (r *R2Client) GetPresignedURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	input := &s3.GetObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(key),
	}

	presignedReq, err := r.presigner.PresignGetObject(ctx, input, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}

	return presignedReq.URL, nil
}

// GetPresignedUploadURL generates a presigned URL for uploads
func (r *R2Client) GetPresignedUploadURL(ctx context.Context, key string, contentType string, expiry time.Duration) (string, error) {
	input := &s3.PutObjectInput{
		Bucket:      aws.String(r.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}

	presignedReq, err := r.presigner.PresignPutObject(ctx, input, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned upload URL: %w", err)
	}

	return presignedReq.URL, nil
}

// Exists checks if an object exists in R2
func (r *R2Client) Exists(ctx context.Context, key string) (bool, error) {
	input := &s3.HeadObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(key),
	}

	_, err := r.client.HeadObject(ctx, input)
	if err != nil {
		// Check if it's a "not found" error
		return false, nil
	}

	return true, nil
}

// Copy copies an object within R2
func (r *R2Client) Copy(ctx context.Context, sourceKey, destKey string) error {
	input := &s3.CopyObjectInput{
		Bucket:     aws.String(r.bucket),
		CopySource: aws.String(path.Join(r.bucket, sourceKey)),
		Key:        aws.String(destKey),
	}

	_, err := r.client.CopyObject(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to copy object in R2: %w", err)
	}

	return nil
}

// ObjectInfo contains metadata about a stored object
type ObjectInfo struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified"`
	ETag         string    `json:"etag"`
}

// StorageManager provides high-level storage operations for Enclii
type StorageManager struct {
	r2     *R2Client
	config *StorageConfig
}

// StorageConfig holds storage configuration
type StorageConfig struct {
	// Bucket prefixes for different content types
	BackupPrefix   string
	BuildLogPrefix string
	ArtifactPrefix string

	// Retention policies
	BackupRetentionDays   int
	BuildLogRetentionDays int
}

// NewStorageManager creates a new storage manager
func NewStorageManager(r2 *R2Client, cfg *StorageConfig) *StorageManager {
	if cfg.BackupPrefix == "" {
		cfg.BackupPrefix = "backups/"
	}
	if cfg.BuildLogPrefix == "" {
		cfg.BuildLogPrefix = "build-logs/"
	}
	if cfg.ArtifactPrefix == "" {
		cfg.ArtifactPrefix = "artifacts/"
	}
	if cfg.BackupRetentionDays == 0 {
		cfg.BackupRetentionDays = 30
	}
	if cfg.BuildLogRetentionDays == 0 {
		cfg.BuildLogRetentionDays = 7
	}

	return &StorageManager{
		r2:     r2,
		config: cfg,
	}
}

// UploadBackup uploads a database backup to R2
func (sm *StorageManager) UploadBackup(ctx context.Context, filename string, data io.Reader) error {
	key := sm.config.BackupPrefix + filename
	return sm.r2.Upload(ctx, key, data, "application/octet-stream")
}

// DownloadBackup downloads a database backup from R2
func (sm *StorageManager) DownloadBackup(ctx context.Context, filename string) (io.ReadCloser, error) {
	key := sm.config.BackupPrefix + filename
	return sm.r2.Download(ctx, key)
}

// ListBackups lists all backups in R2
func (sm *StorageManager) ListBackups(ctx context.Context) ([]ObjectInfo, error) {
	return sm.r2.List(ctx, sm.config.BackupPrefix, 1000)
}

// UploadBuildLog uploads build logs to R2
func (sm *StorageManager) UploadBuildLog(ctx context.Context, buildID string, data io.Reader) error {
	key := fmt.Sprintf("%s%s/build.log", sm.config.BuildLogPrefix, buildID)
	return sm.r2.Upload(ctx, key, data, "text/plain")
}

// GetBuildLogURL returns a presigned URL for accessing build logs
func (sm *StorageManager) GetBuildLogURL(ctx context.Context, buildID string) (string, error) {
	key := fmt.Sprintf("%s%s/build.log", sm.config.BuildLogPrefix, buildID)
	return sm.r2.GetPresignedURL(ctx, key, 1*time.Hour)
}

// UploadArtifact uploads a build artifact to R2
func (sm *StorageManager) UploadArtifact(ctx context.Context, projectID, version, filename string, data io.Reader, contentType string) error {
	key := fmt.Sprintf("%s%s/%s/%s", sm.config.ArtifactPrefix, projectID, version, filename)
	return sm.r2.Upload(ctx, key, data, contentType)
}

// GetArtifactURL returns a presigned URL for downloading an artifact
func (sm *StorageManager) GetArtifactURL(ctx context.Context, projectID, version, filename string) (string, error) {
	key := fmt.Sprintf("%s%s/%s/%s", sm.config.ArtifactPrefix, projectID, version, filename)
	return sm.r2.GetPresignedURL(ctx, key, 24*time.Hour)
}

// UploadFile uploads a local file to R2 (implements builder.R2Uploader interface)
func (r *R2Client) UploadFile(ctx context.Context, localPath, r2Key string) error {
	localPath = filepath.Clean(localPath)
	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to open local file: %w", err)
	}
	defer func() { _ = file.Close() }()

	return r.Upload(ctx, r2Key, file, "application/octet-stream")
}

// DownloadFile downloads from R2 to a local file (implements builder.R2Uploader interface)
func (r *R2Client) DownloadFile(ctx context.Context, r2Key, localPath string) error {
	reader, err := r.Download(ctx, r2Key)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()

	localPath = filepath.Clean(localPath)
	file, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create local file: %w", err)
	}
	defer func() { _ = file.Close() }()

	_, err = io.Copy(file, reader)
	if err != nil {
		return fmt.Errorf("failed to write to local file: %w", err)
	}

	return nil
}

// DeleteObject deletes an object from R2 (implements builder.R2Uploader interface)
func (r *R2Client) DeleteObject(ctx context.Context, key string) error {
	return r.Delete(ctx, key)
}

// ListObjects lists object keys with a prefix (implements builder.R2Uploader interface)
func (r *R2Client) ListObjects(ctx context.Context, prefix string) ([]string, error) {
	objects, err := r.List(ctx, prefix, 1000)
	if err != nil {
		return nil, err
	}

	keys := make([]string, len(objects))
	for i, obj := range objects {
		keys[i] = obj.Key
	}
	return keys, nil
}

// CleanupOldObjects removes objects older than retention period
func (sm *StorageManager) CleanupOldObjects(ctx context.Context) error {
	// Cleanup old backups
	backups, err := sm.r2.List(ctx, sm.config.BackupPrefix, 1000)
	if err != nil {
		return fmt.Errorf("failed to list backups: %w", err)
	}

	cutoff := time.Now().AddDate(0, 0, -sm.config.BackupRetentionDays)
	for _, obj := range backups {
		if obj.LastModified.Before(cutoff) {
			if err := sm.r2.Delete(ctx, obj.Key); err != nil {
				logrus.WithError(err).WithField("key", obj.Key).Warn("Failed to delete old backup")
			}
		}
	}

	// Cleanup old build logs
	logs, err := sm.r2.List(ctx, sm.config.BuildLogPrefix, 1000)
	if err != nil {
		return fmt.Errorf("failed to list build logs: %w", err)
	}

	logCutoff := time.Now().AddDate(0, 0, -sm.config.BuildLogRetentionDays)
	for _, obj := range logs {
		if obj.LastModified.Before(logCutoff) {
			if err := sm.r2.Delete(ctx, obj.Key); err != nil {
				logrus.WithError(err).WithField("key", obj.Key).Warn("Failed to delete old build log")
			}
		}
	}

	return nil
}
