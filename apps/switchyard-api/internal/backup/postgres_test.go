package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// parseHost
// ---------------------------------------------------------------------------
func TestParseHost(t *testing.T) {
	tests := []struct {
		name        string
		databaseURL string
		want        string
	}{
		{
			name:        "full postgres URL",
			databaseURL: "postgres://admin:secret@db.example.com:5432/mydb",
			want:        "db.example.com",
		},
		{
			name:        "URL with IP address",
			databaseURL: "postgres://user:pass@192.168.1.100:5432/app",
			want:        "192.168.1.100",
		},
		{
			name:        "URL without port",
			databaseURL: "postgres://user@myhost/db",
			want:        "myhost",
		},
		{
			name:        "URL with only scheme and host",
			databaseURL: "postgres://dbserver",
			want:        "dbserver",
		},
		{
			name:        "empty URL falls back to localhost",
			databaseURL: "",
			want:        "localhost",
		},
		{
			name:        "URL with no host component returns localhost",
			databaseURL: "postgres:///dbname",
			want:        "localhost",
		},
		{
			name:        "localhost URL",
			databaseURL: "postgres://localhost:5432/mydb",
			want:        "localhost",
		},
		{
			name:        "URL with IPv6 host",
			databaseURL: "postgres://user:pass@[::1]:5432/mydb",
			want:        "::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bm := &BackupManager{config: &BackupConfig{DatabaseURL: tt.databaseURL}}
			got := bm.parseHost()
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// parsePort
// ---------------------------------------------------------------------------
func TestParsePort(t *testing.T) {
	tests := []struct {
		name        string
		databaseURL string
		want        string
	}{
		{
			name:        "explicit port",
			databaseURL: "postgres://user:pass@host:6543/db",
			want:        "6543",
		},
		{
			name:        "standard port",
			databaseURL: "postgres://user:pass@host:5432/db",
			want:        "5432",
		},
		{
			name:        "no port defaults to 5432",
			databaseURL: "postgres://user@host/db",
			want:        "5432",
		},
		{
			name:        "empty URL defaults to 5432",
			databaseURL: "",
			want:        "5432",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bm := &BackupManager{config: &BackupConfig{DatabaseURL: tt.databaseURL}}
			got := bm.parsePort()
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// parseUsername
// ---------------------------------------------------------------------------
func TestParseUsername(t *testing.T) {
	tests := []struct {
		name        string
		databaseURL string
		want        string
	}{
		{
			name:        "explicit username",
			databaseURL: "postgres://admin:secret@host:5432/db",
			want:        "admin",
		},
		{
			name:        "username without password",
			databaseURL: "postgres://readonly@host/db",
			want:        "readonly",
		},
		{
			name:        "no user info defaults to postgres",
			databaseURL: "postgres://host:5432/db",
			want:        "postgres",
		},
		{
			name:        "empty URL defaults to postgres",
			databaseURL: "",
			want:        "postgres",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bm := &BackupManager{config: &BackupConfig{DatabaseURL: tt.databaseURL}}
			got := bm.parseUsername()
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// parsePassword
// ---------------------------------------------------------------------------
func TestParsePassword(t *testing.T) {
	tests := []struct {
		name        string
		databaseURL string
		want        string
	}{
		{
			name:        "explicit password",
			databaseURL: "postgres://admin:s3cret@host:5432/db",
			want:        "s3cret",
		},
		{
			name:        "password with special chars",
			databaseURL: "postgres://user:p%40ss%23word@host/db",
			want:        "p@ss#word",
		},
		{
			name:        "username without password returns empty",
			databaseURL: "postgres://user@host/db",
			want:        "",
		},
		{
			name:        "no user info returns empty",
			databaseURL: "postgres://host:5432/db",
			want:        "",
		},
		{
			name:        "empty URL returns empty",
			databaseURL: "",
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bm := &BackupManager{config: &BackupConfig{DatabaseURL: tt.databaseURL}}
			got := bm.parsePassword()
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// NewBackupManager
// ---------------------------------------------------------------------------
func TestNewBackupManager(t *testing.T) {
	t.Run("default BackupDir when empty", func(t *testing.T) {
		// Use a temp dir so MkdirAll succeeds without permission issues
		tmpDir := t.TempDir()
		cfg := &BackupConfig{BackupDir: "", DatabaseURL: "postgres://localhost/db"}
		// BackupDir will be set to /var/backups/enclii but MkdirAll may fail
		// depending on permissions. We only assert the config mutation.
		cfg.BackupDir = "" // ensure empty
		bm := NewBackupManager(cfg)

		assert.Equal(t, "/var/backups/enclii", bm.config.BackupDir)
		_ = tmpDir // referenced so linter stays quiet
	})

	t.Run("default BackupTimeout when zero", func(t *testing.T) {
		cfg := &BackupConfig{BackupDir: t.TempDir(), BackupTimeout: 0}
		bm := NewBackupManager(cfg)
		assert.Equal(t, 30*time.Minute, bm.config.BackupTimeout)
	})

	t.Run("default RetentionDays when zero", func(t *testing.T) {
		cfg := &BackupConfig{BackupDir: t.TempDir(), RetentionDays: 0}
		bm := NewBackupManager(cfg)
		assert.Equal(t, 30, bm.config.RetentionDays)
	})

	t.Run("custom values preserved", func(t *testing.T) {
		dir := t.TempDir()
		cfg := &BackupConfig{
			BackupDir:     dir,
			BackupTimeout: 5 * time.Minute,
			RetentionDays: 7,
		}
		bm := NewBackupManager(cfg)
		assert.Equal(t, dir, bm.config.BackupDir)
		assert.Equal(t, 5*time.Minute, bm.config.BackupTimeout)
		assert.Equal(t, 7, bm.config.RetentionDays)
	})

	t.Run("creates backup directory via TempDir", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "nested", "backups")
		cfg := &BackupConfig{BackupDir: dir}
		_ = NewBackupManager(cfg)

		info, err := os.Stat(dir)
		require.NoError(t, err, "backup directory should be created")
		assert.True(t, info.IsDir())
	})

	t.Run("returns non-nil manager even when MkdirAll fails", func(t *testing.T) {
		// A path we cannot create (file as parent) to trigger the MkdirAll error path.
		tmpFile := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(tmpFile, []byte("x"), 0o644))

		dir := filepath.Join(tmpFile, "impossible")
		cfg := &BackupConfig{BackupDir: dir}
		bm := NewBackupManager(cfg)
		assert.NotNil(t, bm, "should still return a manager on MkdirAll failure")
	})
}

// ---------------------------------------------------------------------------
// ListBackups
// ---------------------------------------------------------------------------
func TestListBackups(t *testing.T) {
	t.Run("empty directory returns empty list", func(t *testing.T) {
		bm := newTestManager(t)
		backups, err := bm.ListBackups()
		require.NoError(t, err)
		assert.Empty(t, backups)
	})

	t.Run("finds .sql files", func(t *testing.T) {
		bm := newTestManager(t)
		writeTestFile(t, bm.config.BackupDir, "mydb_20260101-120000.sql", "data")

		backups, err := bm.ListBackups()
		require.NoError(t, err)
		require.Len(t, backups, 1)
		assert.Equal(t, "mydb_20260101-120000.sql", backups[0].Filename)
		assert.Equal(t, "mydb", backups[0].DatabaseName)
		assert.False(t, backups[0].Compressed)
		assert.False(t, backups[0].Encrypted)
		assert.Equal(t, "local", backups[0].StorageType)
	})

	t.Run("finds .sql.gz files and marks compressed", func(t *testing.T) {
		bm := newTestManager(t)
		writeTestFile(t, bm.config.BackupDir, "appdb_20260201-080000.sql.gz", "gzdata")

		backups, err := bm.ListBackups()
		require.NoError(t, err)
		require.Len(t, backups, 1)
		assert.True(t, backups[0].Compressed)
		assert.False(t, backups[0].Encrypted)
	})

	t.Run("finds .sql.gz.enc files and marks encrypted", func(t *testing.T) {
		bm := newTestManager(t)
		writeTestFile(t, bm.config.BackupDir, "secdb_20260301-090000.sql.gz.enc", "encdata")

		backups, err := bm.ListBackups()
		require.NoError(t, err)
		require.Len(t, backups, 1)
		assert.True(t, backups[0].Compressed)
		assert.True(t, backups[0].Encrypted)
	})

	t.Run("skips non-backup file extensions", func(t *testing.T) {
		bm := newTestManager(t)
		writeTestFile(t, bm.config.BackupDir, "notes_20260101-120000.txt", "text")
		writeTestFile(t, bm.config.BackupDir, "readme.md", "docs")

		backups, err := bm.ListBackups()
		require.NoError(t, err)
		assert.Empty(t, backups)
	})

	t.Run("skips directories inside backup dir", func(t *testing.T) {
		bm := newTestManager(t)
		subDir := filepath.Join(bm.config.BackupDir, "subdir_20260101.sql")
		require.NoError(t, os.Mkdir(subDir, 0o755))

		backups, err := bm.ListBackups()
		require.NoError(t, err)
		assert.Empty(t, backups)
	})

	t.Run("skips files without underscore separator", func(t *testing.T) {
		bm := newTestManager(t)
		writeTestFile(t, bm.config.BackupDir, "nounderscore.sql", "data")

		backups, err := bm.ListBackups()
		require.NoError(t, err)
		assert.Empty(t, backups)
	})

	t.Run("multiple files sorted newest first", func(t *testing.T) {
		bm := newTestManager(t)

		// Create files with different modification times
		oldFile := filepath.Join(bm.config.BackupDir, "db_old.sql")
		require.NoError(t, os.WriteFile(oldFile, []byte("old"), 0o644))
		oldTime := time.Now().Add(-48 * time.Hour)
		require.NoError(t, os.Chtimes(oldFile, oldTime, oldTime))

		newFile := filepath.Join(bm.config.BackupDir, "db_new.sql")
		require.NoError(t, os.WriteFile(newFile, []byte("new"), 0o644))

		backups, err := bm.ListBackups()
		require.NoError(t, err)
		require.Len(t, backups, 2)
		assert.Equal(t, "db_new.sql", backups[0].Filename, "newest should be first")
		assert.Equal(t, "db_old.sql", backups[1].Filename, "oldest should be last")
	})

	t.Run("non-existent directory returns error", func(t *testing.T) {
		bm := &BackupManager{config: &BackupConfig{BackupDir: "/nonexistent/dir/xyz"}}
		_, err := bm.ListBackups()
		assert.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// CleanupOldBackups
// ---------------------------------------------------------------------------
func TestCleanupOldBackups(t *testing.T) {
	t.Run("no old backups nothing deleted", func(t *testing.T) {
		bm := newTestManager(t)
		writeTestFile(t, bm.config.BackupDir, "db_recent.sql", "data")

		err := bm.CleanupOldBackups()
		require.NoError(t, err)

		backups, _ := bm.ListBackups()
		assert.Len(t, backups, 1, "recent file should remain")
	})

	t.Run("old backup file deleted", func(t *testing.T) {
		bm := newTestManagerWithRetention(t, 7)
		path := filepath.Join(bm.config.BackupDir, "db_old.sql")
		require.NoError(t, os.WriteFile(path, []byte("old"), 0o644))

		// Set modification time to 30 days ago
		oldTime := time.Now().AddDate(0, 0, -30)
		require.NoError(t, os.Chtimes(path, oldTime, oldTime))

		err := bm.CleanupOldBackups()
		require.NoError(t, err)

		backups, _ := bm.ListBackups()
		assert.Empty(t, backups, "old backup should be deleted")
	})

	t.Run("recent backup preserved", func(t *testing.T) {
		bm := newTestManagerWithRetention(t, 7)
		writeTestFile(t, bm.config.BackupDir, "db_today.sql", "fresh")

		err := bm.CleanupOldBackups()
		require.NoError(t, err)

		backups, _ := bm.ListBackups()
		assert.Len(t, backups, 1, "recent backup should be kept")
	})

	t.Run("mix of old and new only old deleted", func(t *testing.T) {
		bm := newTestManagerWithRetention(t, 7)

		// Recent file
		writeTestFile(t, bm.config.BackupDir, "db_new.sql", "new")

		// Old file
		oldPath := filepath.Join(bm.config.BackupDir, "db_old.sql")
		require.NoError(t, os.WriteFile(oldPath, []byte("old"), 0o644))
		oldTime := time.Now().AddDate(0, 0, -30)
		require.NoError(t, os.Chtimes(oldPath, oldTime, oldTime))

		err := bm.CleanupOldBackups()
		require.NoError(t, err)

		backups, _ := bm.ListBackups()
		require.Len(t, backups, 1)
		assert.Equal(t, "db_new.sql", backups[0].Filename)
	})

	t.Run("empty directory no error", func(t *testing.T) {
		bm := newTestManager(t)
		err := bm.CleanupOldBackups()
		assert.NoError(t, err)
	})
}

// ---------------------------------------------------------------------------
// VerifyBackup
// ---------------------------------------------------------------------------
func TestVerifyBackup(t *testing.T) {
	t.Run("valid PostgreSQL dump passes", func(t *testing.T) {
		bm := newTestManager(t)
		content := "-- PostgreSQL database dump\n-- Dumped by pg_dump\nCREATE TABLE t (id int);\n"
		writeTestFile(t, bm.config.BackupDir, "db_valid.sql", content)

		err := bm.VerifyBackup("db_valid.sql")
		assert.NoError(t, err)
	})

	t.Run("empty file returns error", func(t *testing.T) {
		bm := newTestManager(t)
		writeTestFile(t, bm.config.BackupDir, "db_empty.sql", "")

		err := bm.VerifyBackup("db_empty.sql")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "does not appear to be a valid PostgreSQL dump")
	})

	t.Run("file with wrong content returns error", func(t *testing.T) {
		bm := newTestManager(t)
		writeTestFile(t, bm.config.BackupDir, "db_bad.sql", "this is not a dump file")

		err := bm.VerifyBackup("db_bad.sql")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "does not appear to be a valid PostgreSQL dump")
	})

	t.Run("non-existent file returns error", func(t *testing.T) {
		bm := newTestManager(t)
		err := bm.VerifyBackup("nonexistent.sql")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot open backup file")
	})

	t.Run("file with dump marker mid-content passes", func(t *testing.T) {
		bm := newTestManager(t)
		content := "-- header\n-- PostgreSQL database dump\nSELECT 1;\n"
		writeTestFile(t, bm.config.BackupDir, "db_mid.sql", content)

		err := bm.VerifyBackup("db_mid.sql")
		assert.NoError(t, err)
	})
}

// ---------------------------------------------------------------------------
// calculateChecksum
// ---------------------------------------------------------------------------
func TestCalculateChecksum(t *testing.T) {
	t.Run("known content produces known sha256", func(t *testing.T) {
		bm := newTestManager(t)
		path := filepath.Join(bm.config.BackupDir, "checksum_test.txt")
		content := []byte("hello world\n")
		require.NoError(t, os.WriteFile(path, content, 0o644))

		got, err := bm.calculateChecksum(path)
		require.NoError(t, err)

		h := sha256.Sum256(content)
		expected := "sha256:" + hex.EncodeToString(h[:])
		assert.Equal(t, expected, got)
	})

	t.Run("empty file produces valid hash", func(t *testing.T) {
		bm := newTestManager(t)
		path := filepath.Join(bm.config.BackupDir, "empty.txt")
		require.NoError(t, os.WriteFile(path, []byte{}, 0o644))

		got, err := bm.calculateChecksum(path)
		require.NoError(t, err)

		h := sha256.Sum256([]byte{})
		expected := "sha256:" + hex.EncodeToString(h[:])
		assert.Equal(t, expected, got)
	})

	t.Run("non-existent file returns error", func(t *testing.T) {
		bm := newTestManager(t)
		_, err := bm.calculateChecksum(filepath.Join(bm.config.BackupDir, "nope.bin"))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "open file for checksum")
	})
}

// ---------------------------------------------------------------------------
// DefaultBackupConfig
// ---------------------------------------------------------------------------
func TestDefaultBackupConfig(t *testing.T) {
	t.Run("default values are sane", func(t *testing.T) {
		cfg := DefaultBackupConfig()
		require.NotNil(t, cfg)

		assert.Equal(t, "/var/backups/enclii", cfg.BackupDir)
		assert.Equal(t, 30, cfg.RetentionDays)
		assert.Equal(t, 30*time.Minute, cfg.BackupTimeout)
		assert.True(t, cfg.EnableCompression)
		assert.Equal(t, 6, cfg.CompressionLevel)
		assert.False(t, cfg.EnableEncryption)
	})

	t.Run("all fields have expected defaults", func(t *testing.T) {
		cfg := DefaultBackupConfig()

		// String fields that are intentionally empty by default
		assert.Empty(t, cfg.DatabaseURL)
		assert.Empty(t, cfg.S3Bucket)
		assert.Empty(t, cfg.S3Region)
		assert.Empty(t, cfg.S3AccessKey)
		assert.Empty(t, cfg.S3SecretKey)
		assert.Empty(t, cfg.Schedule)
		assert.Empty(t, cfg.EncryptionKey)
	})
}

// ---------------------------------------------------------------------------
// DefaultDRConfig
// ---------------------------------------------------------------------------
func TestDefaultDRConfig(t *testing.T) {
	t.Run("default values are sane", func(t *testing.T) {
		cfg := DefaultDRConfig()
		require.NotNil(t, cfg)

		assert.Equal(t, "async", cfg.SyncMode)
		assert.False(t, cfg.AutoFailover)
		assert.Equal(t, 30*time.Second, cfg.HealthCheckInterval)
		assert.Equal(t, 3, cfg.FailureThreshold)
		assert.Equal(t, 10*time.Minute, cfg.RecoveryTimeout)
	})

	t.Run("string fields intentionally empty by default", func(t *testing.T) {
		cfg := DefaultDRConfig()
		assert.Empty(t, cfg.PrimaryDB)
		assert.Empty(t, cfg.StandbyDB)
	})
}

// ---------------------------------------------------------------------------
// s3Env
// ---------------------------------------------------------------------------
func TestS3Env(t *testing.T) {
	t.Run("all credentials set includes all env vars", func(t *testing.T) {
		bm := &BackupManager{config: &BackupConfig{
			S3AccessKey: "AKIAEXAMPLE",
			S3SecretKey: "wJalrXUtnFEMI/K7MDENG",
			S3Region:    "us-east-1",
		}}
		env := bm.s3Env()

		assert.Contains(t, env, "AWS_ACCESS_KEY_ID=AKIAEXAMPLE")
		assert.Contains(t, env, "AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG")
		assert.Contains(t, env, "AWS_DEFAULT_REGION=us-east-1")
	})

	t.Run("no credentials returns only os.Environ", func(t *testing.T) {
		bm := &BackupManager{config: &BackupConfig{}}
		env := bm.s3Env()

		// Should contain at least the process environment but none of the AWS vars
		for _, e := range env {
			assert.NotContains(t, e, "AWS_ACCESS_KEY_ID=")
			assert.NotContains(t, e, "AWS_SECRET_ACCESS_KEY=")
			assert.NotContains(t, e, "AWS_DEFAULT_REGION=")
		}
	})

	t.Run("partial credentials only set ones included", func(t *testing.T) {
		bm := &BackupManager{config: &BackupConfig{
			S3AccessKey: "AKIAPARTIAL",
			// S3SecretKey intentionally empty
			S3Region: "eu-west-1",
		}}
		env := bm.s3Env()

		assert.Contains(t, env, "AWS_ACCESS_KEY_ID=AKIAPARTIAL")
		assert.Contains(t, env, "AWS_DEFAULT_REGION=eu-west-1")

		// Secret key should NOT be present as an appended entry from our code
		found := false
		for _, e := range env {
			if e == "AWS_SECRET_ACCESS_KEY=" {
				found = true
			}
		}
		assert.False(t, found, "empty secret key should not be appended")
	})
}

// ---------------------------------------------------------------------------
// BackupInfo struct field coverage (sanity)
// ---------------------------------------------------------------------------
func TestBackupInfoFields(t *testing.T) {
	now := time.Now()
	info := &BackupInfo{
		Filename:     "test_20260320-100000.sql.gz",
		Size:         12345,
		CreatedAt:    now,
		DatabaseName: "test",
		Compressed:   true,
		Encrypted:    false,
		Checksum:     "sha256:abc123",
		StorageType:  "s3",
	}

	assert.Equal(t, "test_20260320-100000.sql.gz", info.Filename)
	assert.Equal(t, int64(12345), info.Size)
	assert.Equal(t, now, info.CreatedAt)
	assert.Equal(t, "test", info.DatabaseName)
	assert.True(t, info.Compressed)
	assert.False(t, info.Encrypted)
	assert.Equal(t, "sha256:abc123", info.Checksum)
	assert.Equal(t, "s3", info.StorageType)
}

// ---------------------------------------------------------------------------
// NewDisasterRecoveryManager
// ---------------------------------------------------------------------------
func TestNewDisasterRecoveryManager(t *testing.T) {
	bm := &BackupManager{config: &BackupConfig{}}
	drCfg := &DRConfig{
		PrimaryDB:    "primary",
		StandbyDB:    "standby",
		SyncMode:     "sync",
		AutoFailover: true,
	}

	dr := NewDisasterRecoveryManager(bm, drCfg)
	require.NotNil(t, dr)
	assert.Equal(t, bm, dr.backupManager)
	assert.Equal(t, "sync", dr.config.SyncMode)
	assert.True(t, dr.config.AutoFailover)
}

// ---------------------------------------------------------------------------
// Integration-style: ListBackups with mixed file types
// ---------------------------------------------------------------------------
func TestListBackups_MixedFileTypes(t *testing.T) {
	bm := newTestManager(t)
	dir := bm.config.BackupDir

	// Valid backup files
	writeTestFile(t, dir, "app_20260101-120000.sql", "data")
	writeTestFile(t, dir, "app_20260102-120000.sql.gz", "compressed")
	writeTestFile(t, dir, "app_20260103-120000.sql.gz.enc", "encrypted")

	// Files that should be skipped
	writeTestFile(t, dir, "readme.md", "docs")
	writeTestFile(t, dir, "config.yaml", "cfg")
	writeTestFile(t, dir, "nounderscore.sql", "bad name")
	require.NoError(t, os.Mkdir(filepath.Join(dir, "subdir_test.sql"), 0o755))

	backups, err := bm.ListBackups()
	require.NoError(t, err)
	assert.Len(t, backups, 3, "should find exactly 3 valid backup files")

	// Verify each type is represented
	filenames := make([]string, len(backups))
	for i, b := range backups {
		filenames[i] = b.Filename
	}
	assert.Contains(t, filenames, "app_20260101-120000.sql")
	assert.Contains(t, filenames, "app_20260102-120000.sql.gz")
	assert.Contains(t, filenames, "app_20260103-120000.sql.gz.enc")
}

// ---------------------------------------------------------------------------
// Edge case: URL parsing with unusual but valid formats
// ---------------------------------------------------------------------------
func TestURLParsingEdgeCases(t *testing.T) {
	t.Run("postgresql scheme works the same as postgres", func(t *testing.T) {
		bm := &BackupManager{config: &BackupConfig{
			DatabaseURL: "postgresql://admin:pass@dbhost:5433/mydb",
		}}
		assert.Equal(t, "dbhost", bm.parseHost())
		assert.Equal(t, "5433", bm.parsePort())
		assert.Equal(t, "admin", bm.parseUsername())
		assert.Equal(t, "pass", bm.parsePassword())
	})

	t.Run("URL with query parameters parses host correctly", func(t *testing.T) {
		bm := &BackupManager{config: &BackupConfig{
			DatabaseURL: "postgres://user:pass@host:5432/db?sslmode=require&connect_timeout=10",
		}}
		assert.Equal(t, "host", bm.parseHost())
		assert.Equal(t, "5432", bm.parsePort())
	})

	t.Run("empty password with colon separator", func(t *testing.T) {
		bm := &BackupManager{config: &BackupConfig{
			DatabaseURL: "postgres://user:@host/db",
		}}
		assert.Equal(t, "user", bm.parseUsername())
		assert.Equal(t, "", bm.parsePassword())
	})
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newTestManager creates a BackupManager with a temporary directory.
func newTestManager(t *testing.T) *BackupManager {
	t.Helper()
	return &BackupManager{
		config: &BackupConfig{
			BackupDir:     t.TempDir(),
			RetentionDays: 30,
			BackupTimeout: 30 * time.Minute,
		},
	}
}

// newTestManagerWithRetention creates a BackupManager with a custom retention period.
func newTestManagerWithRetention(t *testing.T, days int) *BackupManager {
	t.Helper()
	return &BackupManager{
		config: &BackupConfig{
			BackupDir:     t.TempDir(),
			RetentionDays: days,
			BackupTimeout: 30 * time.Minute,
		},
	}
}

// writeTestFile writes a file into the given directory.
func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}
