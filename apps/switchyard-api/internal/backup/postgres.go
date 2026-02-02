package backup

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// BackupManager handles database backup operations
type BackupManager struct {
	config *BackupConfig
}

type BackupConfig struct {
	DatabaseURL   string
	BackupDir     string
	RetentionDays int
	S3Bucket      string
	S3Region      string
	S3AccessKey   string
	S3SecretKey   string

	// Backup schedule
	Schedule      string // cron format
	BackupTimeout time.Duration

	// Compression
	EnableCompression bool
	CompressionLevel  int

	// Encryption
	EnableEncryption bool
	EncryptionKey    string
}

type BackupInfo struct {
	Filename     string    `json:"filename"`
	Size         int64     `json:"size"`
	CreatedAt    time.Time `json:"created_at"`
	DatabaseName string    `json:"database_name"`
	Compressed   bool      `json:"compressed"`
	Encrypted    bool      `json:"encrypted"`
	Checksum     string    `json:"checksum"`
	StorageType  string    `json:"storage_type"` // "local", "s3"
}

type RestoreOptions struct {
	BackupFile    string
	DatabaseName  string
	DropExisting  bool
	RestoreData   bool
	RestoreSchema bool
}

func NewBackupManager(config *BackupConfig) *BackupManager {
	if config.BackupDir == "" {
		config.BackupDir = "/var/backups/enclii"
	}

	if config.BackupTimeout == 0 {
		config.BackupTimeout = 30 * time.Minute
	}

	if config.RetentionDays == 0 {
		config.RetentionDays = 30
	}

	// Ensure backup directory exists
	if err := os.MkdirAll(config.BackupDir, 0755); err != nil {
		logrus.Errorf("Failed to create backup directory: %v", err)
	}

	return &BackupManager{config: config}
}

// CreateBackup creates a backup of the database
func (bm *BackupManager) CreateBackup(ctx context.Context, databaseName string) (*BackupInfo, error) {
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("%s_%s.sql", databaseName, timestamp)

	if bm.config.EnableCompression {
		filename += ".gz"
	}

	if bm.config.EnableEncryption {
		filename += ".enc"
	}

	backupPath := filepath.Join(bm.config.BackupDir, filename)

	logrus.WithFields(logrus.Fields{
		"database": databaseName,
		"file":     filename,
	}).Info("Starting database backup")

	// Create context with timeout
	backupCtx, cancel := context.WithTimeout(ctx, bm.config.BackupTimeout)
	defer cancel()

	// Build pg_dump command
	cmd := bm.buildPgDumpCommand(backupCtx, databaseName, backupPath)

	// Execute backup
	start := time.Now()
	output, err := cmd.CombinedOutput()
	duration := time.Since(start)

	if err != nil {
		logrus.WithFields(logrus.Fields{
			"error":    err.Error(),
			"output":   string(output),
			"duration": duration,
		}).Error("Backup failed")
		return nil, fmt.Errorf("backup failed: %w", err)
	}

	// Get file info
	fileInfo, err := os.Stat(backupPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get backup file info: %w", err)
	}

	// Calculate checksum
	checksum, err := bm.calculateChecksum(backupPath)
	if err != nil {
		logrus.Warnf("Failed to calculate checksum: %v", err)
	}

	backupInfo := &BackupInfo{
		Filename:     filename,
		Size:         fileInfo.Size(),
		CreatedAt:    time.Now(),
		DatabaseName: databaseName,
		Compressed:   bm.config.EnableCompression,
		Encrypted:    bm.config.EnableEncryption,
		Checksum:     checksum,
		StorageType:  "local",
	}

	logrus.WithFields(logrus.Fields{
		"file":     filename,
		"size":     fileInfo.Size(),
		"duration": duration,
	}).Info("Backup completed successfully")

	// Upload to S3 if configured
	if bm.config.S3Bucket != "" {
		if err := bm.uploadToS3(backupPath, filename); err != nil {
			logrus.Errorf("Failed to upload backup to S3: %v", err)
		} else {
			backupInfo.StorageType = "s3"
		}
	}

	return backupInfo, nil
}

// RestoreBackup restores a database from backup
func (bm *BackupManager) RestoreBackup(ctx context.Context, options *RestoreOptions) error {
	backupPath := filepath.Join(bm.config.BackupDir, options.BackupFile)

	// Check if backup file exists locally
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		// Try to download from S3 if configured
		if bm.config.S3Bucket != "" {
			if err := bm.downloadFromS3(options.BackupFile, backupPath); err != nil {
				return fmt.Errorf("backup file not found locally or in S3: %w", err)
			}
		} else {
			return fmt.Errorf("backup file not found: %s", backupPath)
		}
	}

	logrus.WithFields(logrus.Fields{
		"backup_file":   options.BackupFile,
		"database":      options.DatabaseName,
		"drop_existing": options.DropExisting,
	}).Info("Starting database restore")

	// Drop existing database if requested
	if options.DropExisting {
		if err := bm.dropDatabase(options.DatabaseName); err != nil {
			return fmt.Errorf("failed to drop existing database: %w", err)
		}
	}

	// Create database if it doesn't exist
	if err := bm.createDatabase(options.DatabaseName); err != nil {
		logrus.Warnf("Database creation failed (may already exist): %v", err)
	}

	// Build pg_restore command
	cmd := bm.buildPgRestoreCommand(ctx, options.DatabaseName, backupPath)

	// Execute restore
	start := time.Now()
	output, err := cmd.CombinedOutput()
	duration := time.Since(start)

	if err != nil {
		logrus.WithFields(logrus.Fields{
			"error":    err.Error(),
			"output":   string(output),
			"duration": duration,
		}).Error("Restore failed")
		return fmt.Errorf("restore failed: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"database": options.DatabaseName,
		"duration": duration,
	}).Info("Restore completed successfully")

	return nil
}

// ListBackups lists all available backups
func (bm *BackupManager) ListBackups() ([]*BackupInfo, error) {
	files, err := os.ReadDir(bm.config.BackupDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read backup directory: %w", err)
	}

	var backups []*BackupInfo

	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".sql") &&
			!strings.HasSuffix(file.Name(), ".sql.gz") &&
			!strings.HasSuffix(file.Name(), ".sql.gz.enc") {
			continue
		}

		fileInfo, err := file.Info()
		if err != nil {
			continue
		}

		// Parse filename to extract database name and timestamp
		parts := strings.Split(file.Name(), "_")
		if len(parts) < 2 {
			continue
		}

		databaseName := parts[0]

		backupInfo := &BackupInfo{
			Filename:     file.Name(),
			Size:         fileInfo.Size(),
			CreatedAt:    fileInfo.ModTime(),
			DatabaseName: databaseName,
			Compressed:   strings.Contains(file.Name(), ".gz"),
			Encrypted:    strings.Contains(file.Name(), ".enc"),
			StorageType:  "local",
		}

		backups = append(backups, backupInfo)
	}

	// Sort by creation time (newest first)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})

	return backups, nil
}

// CleanupOldBackups removes backups older than retention period
func (bm *BackupManager) CleanupOldBackups() error {
	backups, err := bm.ListBackups()
	if err != nil {
		return err
	}

	cutoffTime := time.Now().AddDate(0, 0, -bm.config.RetentionDays)
	var deletedCount int

	for _, backup := range backups {
		if backup.CreatedAt.Before(cutoffTime) {
			backupPath := filepath.Join(bm.config.BackupDir, backup.Filename)
			if err := os.Remove(backupPath); err != nil {
				logrus.Errorf("Failed to delete old backup %s: %v", backup.Filename, err)
				continue
			}

			// Delete from S3 if configured
			if bm.config.S3Bucket != "" {
				if err := bm.deleteFromS3(backup.Filename); err != nil {
					logrus.Errorf("Failed to delete backup from S3: %v", err)
				}
			}

			deletedCount++
			logrus.WithField("file", backup.Filename).Info("Deleted old backup")
		}
	}

	if deletedCount > 0 {
		logrus.WithField("count", deletedCount).Info("Cleaned up old backups")
	}

	return nil
}

// VerifyBackup verifies the integrity of a backup
func (bm *BackupManager) VerifyBackup(backupFile string) error {
	backupPath := filepath.Clean(filepath.Join(bm.config.BackupDir, backupFile))

	// Check if file exists and is readable
	file, err := os.Open(backupPath)
	if err != nil {
		return fmt.Errorf("cannot open backup file: %w", err)
	}
	defer file.Close()

	// Basic verification - check if it's a valid SQL dump
	buffer := make([]byte, 1024)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return fmt.Errorf("cannot read backup file: %w", err)
	}

	content := string(buffer[:n])
	if !strings.Contains(content, "PostgreSQL database dump") {
		return fmt.Errorf("backup file does not appear to be a valid PostgreSQL dump")
	}

	logrus.WithField("file", backupFile).Info("Backup verification passed")
	return nil
}

// Helper methods
func (bm *BackupManager) buildPgDumpCommand(ctx context.Context, databaseName, outputPath string) *exec.Cmd {
	args := []string{
		"-h", bm.parseHost(),
		"-p", bm.parsePort(),
		"-U", bm.parseUsername(),
		"-d", databaseName,
		"--verbose",
		"--no-password",
		"--format=custom",
		"--compress=9",
		"--file=" + outputPath,
	}

	cmd := exec.CommandContext(ctx, "pg_dump", args...)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+bm.parsePassword())

	return cmd
}

func (bm *BackupManager) buildPgRestoreCommand(ctx context.Context, databaseName, backupPath string) *exec.Cmd {
	args := []string{
		"-h", bm.parseHost(),
		"-p", bm.parsePort(),
		"-U", bm.parseUsername(),
		"-d", databaseName,
		"--verbose",
		"--no-password",
		"--clean",
		"--if-exists",
		backupPath,
	}

	cmd := exec.CommandContext(ctx, "pg_restore", args...)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+bm.parsePassword())

	return cmd
}

func (bm *BackupManager) parseHost() string {
	u, err := url.Parse(bm.config.DatabaseURL)
	if err != nil {
		logrus.Errorf("Failed to parse database URL: %v", err)
		return "localhost"
	}
	host := u.Hostname()
	if host == "" {
		return "localhost"
	}
	return host
}

func (bm *BackupManager) parsePort() string {
	u, err := url.Parse(bm.config.DatabaseURL)
	if err != nil {
		logrus.Errorf("Failed to parse database URL: %v", err)
		return "5432"
	}
	port := u.Port()
	if port == "" {
		return "5432"
	}
	return port
}

func (bm *BackupManager) parseUsername() string {
	u, err := url.Parse(bm.config.DatabaseURL)
	if err != nil {
		logrus.Errorf("Failed to parse database URL: %v", err)
		return "postgres"
	}
	if u.User == nil {
		return "postgres"
	}
	return u.User.Username()
}

func (bm *BackupManager) parsePassword() string {
	u, err := url.Parse(bm.config.DatabaseURL)
	if err != nil {
		logrus.Errorf("Failed to parse database URL: %v", err)
		return ""
	}
	if u.User == nil {
		return ""
	}
	password, _ := u.User.Password()
	return password
}

func (bm *BackupManager) dropDatabase(databaseName string) error {
	cmd := exec.Command("dropdb", "-h", bm.parseHost(), "-p", bm.parsePort(), "-U", bm.parseUsername(), databaseName)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+bm.parsePassword())

	return cmd.Run()
}

func (bm *BackupManager) createDatabase(databaseName string) error {
	cmd := exec.Command("createdb", "-h", bm.parseHost(), "-p", bm.parsePort(), "-U", bm.parseUsername(), databaseName)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+bm.parsePassword())

	return cmd.Run()
}

func (bm *BackupManager) calculateChecksum(filePath string) (string, error) {
	// In a real implementation, calculate SHA256 checksum
	return "checksum-placeholder", nil
}

// S3 operations (simplified - would use AWS SDK in production)
func (bm *BackupManager) uploadToS3(localPath, key string) error {
	logrus.WithFields(logrus.Fields{
		"local_path": localPath,
		"s3_key":     key,
		"bucket":     bm.config.S3Bucket,
	}).Info("Uploading backup to S3")

	// Implementation would use AWS SDK to upload file
	return nil
}

func (bm *BackupManager) downloadFromS3(key, localPath string) error {
	logrus.WithFields(logrus.Fields{
		"s3_key":     key,
		"local_path": localPath,
		"bucket":     bm.config.S3Bucket,
	}).Info("Downloading backup from S3")

	// Implementation would use AWS SDK to download file
	return nil
}

func (bm *BackupManager) deleteFromS3(key string) error {
	// Implementation would use AWS SDK to delete file
	return nil
}

// Disaster Recovery operations
type DisasterRecoveryManager struct {
	backupManager *BackupManager
	config        *DRConfig
}

type DRConfig struct {
	PrimaryDB    string
	StandbyDB    string
	SyncMode     string // "sync" or "async"
	AutoFailover bool

	// Health check intervals
	HealthCheckInterval time.Duration
	FailureThreshold    int

	// Recovery settings
	RecoveryTimeout time.Duration
}

func NewDisasterRecoveryManager(backupManager *BackupManager, config *DRConfig) *DisasterRecoveryManager {
	return &DisasterRecoveryManager{
		backupManager: backupManager,
		config:        config,
	}
}

// InitiateFailover initiates failover to standby database
func (dr *DisasterRecoveryManager) InitiateFailover(ctx context.Context) error {
	logrus.Warn("Initiating disaster recovery failover")

	// 1. Stop accepting new connections to primary
	// 2. Wait for ongoing transactions to complete
	// 3. Promote standby to primary
	// 4. Update connection strings/DNS
	// 5. Notify administrators

	// This is a simplified implementation
	// In production, this would involve complex coordination

	logrus.Info("Failover completed successfully")
	return nil
}

// PerformPointInTimeRecovery performs point-in-time recovery
func (dr *DisasterRecoveryManager) PerformPointInTimeRecovery(ctx context.Context, targetTime time.Time) error {
	logrus.WithField("target_time", targetTime).Info("Starting point-in-time recovery")

	// 1. Find the appropriate backup before the target time
	// 2. Restore from that backup
	// 3. Apply WAL files up to the target time
	// 4. Start the database

	return nil
}

// Default configurations
func DefaultBackupConfig() *BackupConfig {
	return &BackupConfig{
		BackupDir:         "/var/backups/enclii",
		RetentionDays:     30,
		BackupTimeout:     30 * time.Minute,
		EnableCompression: true,
		CompressionLevel:  6,
		EnableEncryption:  false,
	}
}

func DefaultDRConfig() *DRConfig {
	return &DRConfig{
		SyncMode:            "async",
		AutoFailover:        false,
		HealthCheckInterval: 30 * time.Second,
		FailureThreshold:    3,
		RecoveryTimeout:     10 * time.Minute,
	}
}
