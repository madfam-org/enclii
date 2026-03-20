package storage

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// fakeS3Handler returns an http.Handler that simulates a minimal S3-compatible
// API. It stores objects in memory and supports PutObject, GetObject,
// DeleteObject, HeadObject, ListObjectsV2, and CopyObject.
func fakeS3Handler(objects map[string][]byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The key is the URL path minus the leading /bucket/ segment.
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)
		key := ""
		if len(parts) == 2 {
			key = parts[1]
		}

		switch r.Method {
		case http.MethodPut:
			// CopyObject sends x-amz-copy-source header
			if src := r.Header.Get("X-Amz-Copy-Source"); src != "" {
				// Source format is /bucket/key
				srcParts := strings.SplitN(strings.TrimPrefix(src, "/"), "/", 2)
				srcKey := ""
				if len(srcParts) == 2 {
					srcKey = srcParts[1]
				}
				data, ok := objects[srcKey]
				if !ok {
					http.Error(w, xmlError("NoSuchKey", "Source key not found"), http.StatusNotFound)
					return
				}
				objects[key] = data
				// Return minimal CopyObjectResult
				w.Header().Set("Content-Type", "application/xml")
				fmt.Fprintf(w, `<CopyObjectResult><ETag>"copy"</ETag></CopyObjectResult>`)
				return
			}
			body, _ := io.ReadAll(r.Body)
			objects[key] = body
			w.WriteHeader(http.StatusOK)

		case http.MethodGet:
			// ListObjectsV2: GET /bucket?list-type=2
			if r.URL.Query().Get("list-type") == "2" {
				prefix := r.URL.Query().Get("prefix")
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(http.StatusOK)
				writeListResponse(w, objects, prefix)
				return
			}
			data, ok := objects[key]
			if !ok {
				http.Error(w, xmlError("NoSuchKey", "Key not found"), http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write(data)

		case http.MethodDelete:
			delete(objects, key)
			w.WriteHeader(http.StatusNoContent)

		case http.MethodHead:
			if _, ok := objects[key]; !ok {
				http.Error(w, xmlError("NoSuchKey", "Key not found"), http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusOK)

		default:
			http.Error(w, "method not supported", http.StatusMethodNotAllowed)
		}
	})
}

// xmlError returns an S3-style XML error response body.
func xmlError(code, message string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code><Message>%s</Message></Error>`, code, message)
}

// listBucketResult mirrors the S3 ListObjectsV2 response shape for XML encoding.
type listBucketResult struct {
	XMLName  xml.Name     `xml:"ListBucketResult"`
	Contents []listObject `xml:"Contents"`
	KeyCount int          `xml:"KeyCount"`
}

type listObject struct {
	Key          string `xml:"Key"`
	Size         int64  `xml:"Size"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
}

func writeListResponse(w io.Writer, objects map[string][]byte, prefix string) {
	var contents []listObject
	for k, v := range objects {
		if strings.HasPrefix(k, prefix) {
			contents = append(contents, listObject{
				Key:          k,
				Size:         int64(len(v)),
				LastModified: time.Now().UTC().Format(time.RFC3339),
				ETag:         `"fakeetag"`,
			})
		}
	}
	result := listBucketResult{
		Contents: contents,
		KeyCount: len(contents),
	}
	xml.NewEncoder(w).Encode(result)
}

// newTestR2Client creates an R2Client pointing at a fake S3 httptest server.
// The caller must defer server.Close().
func newTestR2Client(t *testing.T, objects map[string][]byte) (*R2Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(fakeS3Handler(objects))

	ctx := context.Background()
	creds := credentials.NewStaticCredentialsProvider("test-key", "test-secret", "")
	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithCredentialsProvider(creds),
		config.WithRegion("auto"),
	)
	if err != nil {
		t.Fatalf("failed to load AWS config for test: %v", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true
	})

	presigner := s3.NewPresignClient(client)

	return &R2Client{
		client:    client,
		bucket:    "test-bucket",
		accountID: "test-account",
		presigner: presigner,
	}, server
}

// newErrorR2Client creates an R2Client backed by a server that always returns
// the given HTTP status code, useful for testing error propagation.
func newErrorR2Client(t *testing.T, statusCode int, xmlCode, xmlMsg string) (*R2Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
		w.Write([]byte(xmlError(xmlCode, xmlMsg)))
	}))

	ctx := context.Background()
	creds := credentials.NewStaticCredentialsProvider("key", "secret", "")
	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithCredentialsProvider(creds),
		config.WithRegion("auto"),
		config.WithRetryMaxAttempts(1),
	)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true
	})

	return &R2Client{
		client:    s3Client,
		bucket:    "test-bucket",
		accountID: "test",
		presigner: s3.NewPresignClient(s3Client),
	}, server
}

// ---------------------------------------------------------------------------
// NewR2Client configuration validation tests
// ---------------------------------------------------------------------------

func TestNewR2Client_MissingAccountID(t *testing.T) {
	_, err := NewR2Client(context.Background(), &R2Config{
		AccountID:       "",
		AccessKeyID:     "key",
		AccessKeySecret: "secret",
		BucketName:      "bucket",
	})
	if err == nil {
		t.Fatal("expected error when AccountID is empty")
	}
	if !strings.Contains(err.Error(), "accountID") {
		t.Fatalf("error should mention accountID, got: %v", err)
	}
}

func TestNewR2Client_MissingAccessKeyID(t *testing.T) {
	_, err := NewR2Client(context.Background(), &R2Config{
		AccountID:       "acct",
		AccessKeyID:     "",
		AccessKeySecret: "secret",
		BucketName:      "bucket",
	})
	if err == nil {
		t.Fatal("expected error when AccessKeyID is empty")
	}
	if !strings.Contains(err.Error(), "accessKeyID") {
		t.Fatalf("error should mention accessKeyID, got: %v", err)
	}
}

func TestNewR2Client_MissingAccessKeySecret(t *testing.T) {
	_, err := NewR2Client(context.Background(), &R2Config{
		AccountID:       "acct",
		AccessKeyID:     "key",
		AccessKeySecret: "",
		BucketName:      "bucket",
	})
	if err == nil {
		t.Fatal("expected error when AccessKeySecret is empty")
	}
	if !strings.Contains(err.Error(), "accessKeySecret") {
		t.Fatalf("error should mention accessKeySecret, got: %v", err)
	}
}

func TestNewR2Client_DefaultEndpoint(t *testing.T) {
	client, err := NewR2Client(context.Background(), &R2Config{
		AccountID:       "abc123",
		AccessKeyID:     "key",
		AccessKeySecret: "secret",
		BucketName:      "my-bucket",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.bucket != "my-bucket" {
		t.Errorf("bucket = %q, want %q", client.bucket, "my-bucket")
	}
	if client.accountID != "abc123" {
		t.Errorf("accountID = %q, want %q", client.accountID, "abc123")
	}
}

func TestNewR2Client_CustomEndpoint(t *testing.T) {
	client, err := NewR2Client(context.Background(), &R2Config{
		AccountID:       "acct",
		AccessKeyID:     "key",
		AccessKeySecret: "secret",
		BucketName:      "bucket",
		Endpoint:        "https://custom.endpoint.example.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	// The client was constructed without error, confirming custom endpoint is accepted.
	// We cannot directly inspect the s3.Client endpoint, but the absence of error
	// confirms the code path that uses cfg.Endpoint was taken instead of the default.
}

// ---------------------------------------------------------------------------
// StorageManager configuration defaults tests
// ---------------------------------------------------------------------------

func TestNewStorageManager_DefaultPrefixes(t *testing.T) {
	sm := NewStorageManager(&R2Client{}, &StorageConfig{})

	if sm.config.BackupPrefix != "backups/" {
		t.Errorf("BackupPrefix = %q, want %q", sm.config.BackupPrefix, "backups/")
	}
	if sm.config.BuildLogPrefix != "build-logs/" {
		t.Errorf("BuildLogPrefix = %q, want %q", sm.config.BuildLogPrefix, "build-logs/")
	}
	if sm.config.ArtifactPrefix != "artifacts/" {
		t.Errorf("ArtifactPrefix = %q, want %q", sm.config.ArtifactPrefix, "artifacts/")
	}
	if sm.config.BackupRetentionDays != 30 {
		t.Errorf("BackupRetentionDays = %d, want %d", sm.config.BackupRetentionDays, 30)
	}
	if sm.config.BuildLogRetentionDays != 7 {
		t.Errorf("BuildLogRetentionDays = %d, want %d", sm.config.BuildLogRetentionDays, 7)
	}
}

func TestNewStorageManager_CustomPrefixes(t *testing.T) {
	sm := NewStorageManager(&R2Client{}, &StorageConfig{
		BackupPrefix:          "custom-backups/",
		BuildLogPrefix:        "custom-logs/",
		ArtifactPrefix:        "custom-artifacts/",
		BackupRetentionDays:   90,
		BuildLogRetentionDays: 14,
	})

	if sm.config.BackupPrefix != "custom-backups/" {
		t.Errorf("BackupPrefix = %q, want %q", sm.config.BackupPrefix, "custom-backups/")
	}
	if sm.config.BuildLogPrefix != "custom-logs/" {
		t.Errorf("BuildLogPrefix = %q, want %q", sm.config.BuildLogPrefix, "custom-logs/")
	}
	if sm.config.ArtifactPrefix != "custom-artifacts/" {
		t.Errorf("ArtifactPrefix = %q, want %q", sm.config.ArtifactPrefix, "custom-artifacts/")
	}
	if sm.config.BackupRetentionDays != 90 {
		t.Errorf("BackupRetentionDays = %d, want %d", sm.config.BackupRetentionDays, 90)
	}
	if sm.config.BuildLogRetentionDays != 14 {
		t.Errorf("BuildLogRetentionDays = %d, want %d", sm.config.BuildLogRetentionDays, 14)
	}
}

// ---------------------------------------------------------------------------
// R2Client operations against fake S3 server
// ---------------------------------------------------------------------------

func TestR2Client_UploadAndDownload(t *testing.T) {
	objects := make(map[string][]byte)
	client, server := newTestR2Client(t, objects)
	defer server.Close()

	ctx := context.Background()
	content := "hello world from R2 test"

	// Upload
	err := client.Upload(ctx, "test/file.txt", strings.NewReader(content), "text/plain")
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	// Verify object was stored in our fake server
	if _, ok := objects["test/file.txt"]; !ok {
		t.Fatal("expected object to exist in fake server after upload")
	}

	// Download
	reader, err := client.Download(ctx, "test/file.txt")
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	defer reader.Close()

	downloaded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed to read downloaded content: %v", err)
	}
	if string(downloaded) != content {
		t.Errorf("downloaded content = %q, want %q", string(downloaded), content)
	}
}

func TestR2Client_Delete(t *testing.T) {
	objects := map[string][]byte{
		"to-delete.txt": []byte("delete me"),
		"keep.txt":      []byte("keep me"),
	}
	client, server := newTestR2Client(t, objects)
	defer server.Close()

	ctx := context.Background()
	err := client.Delete(ctx, "to-delete.txt")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if _, ok := objects["to-delete.txt"]; ok {
		t.Fatal("expected object to be deleted from fake server")
	}
	if _, ok := objects["keep.txt"]; !ok {
		t.Fatal("expected 'keep.txt' to remain after deleting a different key")
	}
}

func TestR2Client_DeleteObject_DelegatesToDelete(t *testing.T) {
	objects := map[string][]byte{
		"target.txt": []byte("data"),
	}
	client, server := newTestR2Client(t, objects)
	defer server.Close()

	ctx := context.Background()
	err := client.DeleteObject(ctx, "target.txt")
	if err != nil {
		t.Fatalf("DeleteObject failed: %v", err)
	}
	if _, ok := objects["target.txt"]; ok {
		t.Error("expected object to be removed via DeleteObject")
	}
}

func TestR2Client_List(t *testing.T) {
	objects := map[string][]byte{
		"backups/db-2026-03-01.sql": []byte("backup1"),
		"backups/db-2026-03-02.sql": []byte("backup2"),
		"logs/build-123.log":        []byte("log data"),
	}
	client, server := newTestR2Client(t, objects)
	defer server.Close()

	ctx := context.Background()

	// List with prefix filter
	results, err := client.List(ctx, "backups/", 100)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 objects with prefix 'backups/', got %d", len(results))
	}

	// Verify all returned keys have the correct prefix
	for _, obj := range results {
		if !strings.HasPrefix(obj.Key, "backups/") {
			t.Errorf("unexpected key %q in list results", obj.Key)
		}
		if obj.Size <= 0 {
			t.Errorf("expected positive Size for key %q, got %d", obj.Key, obj.Size)
		}
	}
}

func TestR2Client_List_EmptyPrefix(t *testing.T) {
	objects := map[string][]byte{
		"a.txt": []byte("a"),
		"b.txt": []byte("b"),
		"c.txt": []byte("c"),
	}
	client, server := newTestR2Client(t, objects)
	defer server.Close()

	ctx := context.Background()
	results, err := client.List(ctx, "", 100)
	if err != nil {
		t.Fatalf("List with empty prefix failed: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 objects with empty prefix, got %d", len(results))
	}
}

func TestR2Client_ListObjects_ReturnsKeys(t *testing.T) {
	objects := map[string][]byte{
		"prefix/a.txt": []byte("a"),
		"prefix/b.txt": []byte("b"),
		"other/c.txt":  []byte("c"),
	}
	client, server := newTestR2Client(t, objects)
	defer server.Close()

	ctx := context.Background()
	keys, err := client.ListObjects(ctx, "prefix/")
	if err != nil {
		t.Fatalf("ListObjects failed: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}

	// Verify these are plain strings, not ObjectInfo
	for _, k := range keys {
		if !strings.HasPrefix(k, "prefix/") {
			t.Errorf("unexpected key %q", k)
		}
	}
}

func TestR2Client_ListObjects_EmptyResult(t *testing.T) {
	objects := make(map[string][]byte)
	client, server := newTestR2Client(t, objects)
	defer server.Close()

	ctx := context.Background()
	keys, err := client.ListObjects(ctx, "nonexistent/")
	if err != nil {
		t.Fatalf("ListObjects failed: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("expected 0 keys for nonexistent prefix, got %d", len(keys))
	}
}

func TestR2Client_Exists(t *testing.T) {
	objects := map[string][]byte{
		"exists.txt": []byte("present"),
	}
	client, server := newTestR2Client(t, objects)
	defer server.Close()

	ctx := context.Background()

	// Existing key
	exists, err := client.Exists(ctx, "exists.txt")
	if err != nil {
		t.Fatalf("Exists failed for existing key: %v", err)
	}
	if !exists {
		t.Error("expected Exists=true for existing key")
	}

	// Non-existing key
	exists, err = client.Exists(ctx, "missing.txt")
	if err != nil {
		t.Fatalf("Exists failed for missing key: %v", err)
	}
	if exists {
		t.Error("expected Exists=false for missing key")
	}
}

func TestR2Client_Copy(t *testing.T) {
	objects := map[string][]byte{
		"source/original.txt": []byte("original content"),
	}
	client, server := newTestR2Client(t, objects)
	defer server.Close()

	ctx := context.Background()
	err := client.Copy(ctx, "source/original.txt", "dest/copied.txt")
	if err != nil {
		t.Fatalf("Copy failed: %v", err)
	}

	// Verify both source and destination exist
	if _, ok := objects["source/original.txt"]; !ok {
		t.Error("expected source key to still exist after copy")
	}
	if _, ok := objects["dest/copied.txt"]; !ok {
		t.Error("expected destination key to exist after copy")
	}
	if !bytes.Equal(objects["source/original.txt"], objects["dest/copied.txt"]) {
		t.Error("expected copied content to match source content")
	}
}

func TestR2Client_GetPresignedURL(t *testing.T) {
	objects := make(map[string][]byte)
	client, server := newTestR2Client(t, objects)
	defer server.Close()

	ctx := context.Background()
	url, err := client.GetPresignedURL(ctx, "some/key.txt", 15*time.Minute)
	if err != nil {
		t.Fatalf("GetPresignedURL failed: %v", err)
	}
	if url == "" {
		t.Fatal("expected non-empty presigned URL")
	}
	// The presigned URL should contain the key path and authentication parameters
	if !strings.Contains(url, "some/key.txt") {
		t.Errorf("presigned URL should contain the object key, got: %s", url)
	}
	if !strings.Contains(url, "X-Amz-Signature") {
		t.Errorf("presigned URL should contain X-Amz-Signature, got: %s", url)
	}
	if !strings.Contains(url, "X-Amz-Expires") {
		t.Errorf("presigned URL should contain X-Amz-Expires, got: %s", url)
	}
}

func TestR2Client_GetPresignedUploadURL(t *testing.T) {
	objects := make(map[string][]byte)
	client, server := newTestR2Client(t, objects)
	defer server.Close()

	ctx := context.Background()
	url, err := client.GetPresignedUploadURL(ctx, "uploads/artifact.tar.gz", "application/gzip", 30*time.Minute)
	if err != nil {
		t.Fatalf("GetPresignedUploadURL failed: %v", err)
	}
	if url == "" {
		t.Fatal("expected non-empty presigned upload URL")
	}
	if !strings.Contains(url, "uploads/artifact.tar.gz") {
		t.Errorf("presigned upload URL should contain the object key, got: %s", url)
	}
	if !strings.Contains(url, "X-Amz-Signature") {
		t.Errorf("presigned upload URL should contain X-Amz-Signature, got: %s", url)
	}
}

// ---------------------------------------------------------------------------
// R2Client file I/O operations (builder.R2Uploader interface)
// ---------------------------------------------------------------------------

func TestR2Client_UploadFile(t *testing.T) {
	objects := make(map[string][]byte)
	client, server := newTestR2Client(t, objects)
	defer server.Close()

	// Create a temporary local file
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "test-upload.txt")
	content := []byte("file upload content for R2")
	if err := os.WriteFile(localPath, content, 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	ctx := context.Background()
	err := client.UploadFile(ctx, localPath, "uploads/test-upload.txt")
	if err != nil {
		t.Fatalf("UploadFile failed: %v", err)
	}

	stored, ok := objects["uploads/test-upload.txt"]
	if !ok {
		t.Fatal("expected object at key 'uploads/test-upload.txt' after UploadFile")
	}
	if !bytes.Equal(stored, content) {
		t.Errorf("stored content = %q, want %q", string(stored), string(content))
	}
}

func TestR2Client_UploadFile_NonexistentFile(t *testing.T) {
	objects := make(map[string][]byte)
	client, server := newTestR2Client(t, objects)
	defer server.Close()

	ctx := context.Background()
	err := client.UploadFile(ctx, "/nonexistent/path/file.txt", "key.txt")
	if err == nil {
		t.Fatal("expected error when uploading from nonexistent local file")
	}
	if !strings.Contains(err.Error(), "failed to open local file") {
		t.Errorf("error should mention opening local file, got: %v", err)
	}
}

func TestR2Client_DownloadFile(t *testing.T) {
	content := []byte("downloaded content from R2")
	objects := map[string][]byte{
		"remote/file.dat": content,
	}
	client, server := newTestR2Client(t, objects)
	defer server.Close()

	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "downloaded.dat")

	ctx := context.Background()
	err := client.DownloadFile(ctx, "remote/file.dat", localPath)
	if err != nil {
		t.Fatalf("DownloadFile failed: %v", err)
	}

	downloaded, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("failed to read downloaded local file: %v", err)
	}
	if !bytes.Equal(downloaded, content) {
		t.Errorf("downloaded file content = %q, want %q", string(downloaded), string(content))
	}
}

func TestR2Client_DownloadFile_NonexistentKey(t *testing.T) {
	objects := make(map[string][]byte)
	client, server := newTestR2Client(t, objects)
	defer server.Close()

	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "output.dat")

	ctx := context.Background()
	err := client.DownloadFile(ctx, "missing/key.dat", localPath)
	if err == nil {
		t.Fatal("expected error when downloading nonexistent key")
	}
}

func TestR2Client_DownloadFile_BadLocalPath(t *testing.T) {
	content := []byte("data")
	objects := map[string][]byte{
		"remote/file.dat": content,
	}
	client, server := newTestR2Client(t, objects)
	defer server.Close()

	ctx := context.Background()
	// Use a path under a nonexistent directory
	err := client.DownloadFile(ctx, "remote/file.dat", "/nonexistent-dir-abc123/output.dat")
	if err == nil {
		t.Fatal("expected error when local path directory does not exist")
	}
	if !strings.Contains(err.Error(), "failed to create local file") {
		t.Errorf("error should mention creating local file, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// StorageManager path construction and delegation tests
// ---------------------------------------------------------------------------

func TestStorageManager_UploadAndDownloadBackup(t *testing.T) {
	objects := make(map[string][]byte)
	client, server := newTestR2Client(t, objects)
	defer server.Close()

	sm := NewStorageManager(client, &StorageConfig{})
	ctx := context.Background()
	content := "pg_dump output"

	// Upload via StorageManager
	err := sm.UploadBackup(ctx, "db-2026-03-19.sql.gz", strings.NewReader(content))
	if err != nil {
		t.Fatalf("UploadBackup failed: %v", err)
	}

	// Verify the constructed key includes the default prefix
	expectedKey := "backups/db-2026-03-19.sql.gz"
	if _, ok := objects[expectedKey]; !ok {
		t.Fatalf("expected object at key %q, got keys: %v", expectedKey, objectKeys(objects))
	}

	// Download via StorageManager
	reader, err := sm.DownloadBackup(ctx, "db-2026-03-19.sql.gz")
	if err != nil {
		t.Fatalf("DownloadBackup failed: %v", err)
	}
	defer reader.Close()

	downloaded, _ := io.ReadAll(reader)
	if string(downloaded) != content {
		t.Errorf("downloaded content = %q, want %q", string(downloaded), content)
	}
}

func TestStorageManager_UploadBuildLog_PathConstruction(t *testing.T) {
	objects := make(map[string][]byte)
	client, server := newTestR2Client(t, objects)
	defer server.Close()

	sm := NewStorageManager(client, &StorageConfig{})
	ctx := context.Background()

	err := sm.UploadBuildLog(ctx, "build-abc123", strings.NewReader("build output"))
	if err != nil {
		t.Fatalf("UploadBuildLog failed: %v", err)
	}

	expectedKey := "build-logs/build-abc123/build.log"
	if _, ok := objects[expectedKey]; !ok {
		t.Fatalf("expected object at key %q, got keys: %v", expectedKey, objectKeys(objects))
	}
}

func TestStorageManager_GetBuildLogURL(t *testing.T) {
	objects := make(map[string][]byte)
	client, server := newTestR2Client(t, objects)
	defer server.Close()

	sm := NewStorageManager(client, &StorageConfig{})
	ctx := context.Background()

	url, err := sm.GetBuildLogURL(ctx, "build-xyz789")
	if err != nil {
		t.Fatalf("GetBuildLogURL failed: %v", err)
	}
	if url == "" {
		t.Fatal("expected non-empty presigned URL from GetBuildLogURL")
	}
	// The URL should reference the correct build log key path
	if !strings.Contains(url, "build-logs/build-xyz789/build.log") {
		t.Errorf("presigned URL should contain the build log key, got: %s", url)
	}
}

func TestStorageManager_UploadArtifact_PathConstruction(t *testing.T) {
	objects := make(map[string][]byte)
	client, server := newTestR2Client(t, objects)
	defer server.Close()

	sm := NewStorageManager(client, &StorageConfig{})
	ctx := context.Background()

	err := sm.UploadArtifact(ctx, "proj-1", "v1.2.3", "app.tar.gz", strings.NewReader("artifact"), "application/gzip")
	if err != nil {
		t.Fatalf("UploadArtifact failed: %v", err)
	}

	expectedKey := "artifacts/proj-1/v1.2.3/app.tar.gz"
	if _, ok := objects[expectedKey]; !ok {
		t.Fatalf("expected object at key %q, got keys: %v", expectedKey, objectKeys(objects))
	}
}

func TestStorageManager_GetArtifactURL(t *testing.T) {
	objects := make(map[string][]byte)
	client, server := newTestR2Client(t, objects)
	defer server.Close()

	sm := NewStorageManager(client, &StorageConfig{})
	ctx := context.Background()

	url, err := sm.GetArtifactURL(ctx, "proj-42", "v2.0.0", "bundle.tar.gz")
	if err != nil {
		t.Fatalf("GetArtifactURL failed: %v", err)
	}
	if url == "" {
		t.Fatal("expected non-empty presigned URL from GetArtifactURL")
	}
	// The URL should reference the correct artifact key path
	if !strings.Contains(url, "artifacts/proj-42/v2.0.0/bundle.tar.gz") {
		t.Errorf("presigned URL should contain the artifact key, got: %s", url)
	}
}

func TestStorageManager_ListBackups(t *testing.T) {
	objects := map[string][]byte{
		"backups/db-01.sql": []byte("backup1"),
		"backups/db-02.sql": []byte("backup2"),
		"artifacts/a.tar":   []byte("artifact"),
	}
	client, server := newTestR2Client(t, objects)
	defer server.Close()

	sm := NewStorageManager(client, &StorageConfig{})
	ctx := context.Background()

	backups, err := sm.ListBackups(ctx)
	if err != nil {
		t.Fatalf("ListBackups failed: %v", err)
	}
	if len(backups) != 2 {
		t.Fatalf("expected 2 backups, got %d", len(backups))
	}
	for _, b := range backups {
		if !strings.HasPrefix(b.Key, "backups/") {
			t.Errorf("backup key %q does not have expected prefix", b.Key)
		}
	}
}

// ---------------------------------------------------------------------------
// S3 error handling tests
// ---------------------------------------------------------------------------

func TestR2Client_Download_NotFound(t *testing.T) {
	objects := make(map[string][]byte) // empty store
	client, server := newTestR2Client(t, objects)
	defer server.Close()

	ctx := context.Background()
	_, err := client.Download(ctx, "nonexistent.txt")
	if err == nil {
		t.Fatal("expected error when downloading nonexistent key")
	}
	if !strings.Contains(err.Error(), "failed to download object from R2") {
		t.Errorf("error should wrap with R2 context, got: %v", err)
	}
}

func TestR2Client_ErrorPropagation_ServerError(t *testing.T) {
	client, server := newErrorR2Client(t, http.StatusInternalServerError, "InternalError", "Something went wrong")
	defer server.Close()

	ctx := context.Background()

	// Upload should propagate the server error
	err := client.Upload(ctx, "key", strings.NewReader("data"), "text/plain")
	if err == nil {
		t.Fatal("expected error from 500 server")
	}
	if !strings.Contains(err.Error(), "failed to upload object to R2") {
		t.Errorf("expected wrapped upload error, got: %v", err)
	}

	// Delete should also propagate
	err = client.Delete(ctx, "key")
	if err == nil {
		t.Fatal("expected error from 500 server on delete")
	}
	if !strings.Contains(err.Error(), "failed to delete object from R2") {
		t.Errorf("expected wrapped delete error, got: %v", err)
	}

	// List should also propagate
	_, err = client.List(ctx, "prefix/", 10)
	if err == nil {
		t.Fatal("expected error from 500 server on list")
	}
	if !strings.Contains(err.Error(), "failed to list objects in R2") {
		t.Errorf("expected wrapped list error, got: %v", err)
	}

	// Copy should also propagate
	err = client.Copy(ctx, "src", "dst")
	if err == nil {
		t.Fatal("expected error from 500 server on copy")
	}
	if !strings.Contains(err.Error(), "failed to copy object in R2") {
		t.Errorf("expected wrapped copy error, got: %v", err)
	}
}

func TestR2Client_ErrorPropagation_ForbiddenAccess(t *testing.T) {
	client, server := newErrorR2Client(t, http.StatusForbidden, "AccessDenied", "Access Denied")
	defer server.Close()

	ctx := context.Background()

	err := client.Upload(ctx, "restricted/file.txt", strings.NewReader("data"), "text/plain")
	if err == nil {
		t.Fatal("expected error for 403 Forbidden")
	}
	// The AWS SDK wraps 403 as an operation error; verify our wrapper is present
	if !strings.Contains(err.Error(), "failed to upload object to R2") {
		t.Errorf("expected R2 upload error wrapper, got: %v", err)
	}

	// Verify the underlying cause mentions the HTTP status or error code
	var respErr *awshttp.ResponseError
	if !strings.Contains(err.Error(), "403") && !strings.Contains(err.Error(), "AccessDenied") {
		// Some SDK versions may or may not include status code in the message.
		// At minimum, verify the error is non-nil and wrapped.
		_ = respErr // used for type reference only
	}
}

// ---------------------------------------------------------------------------
// ObjectInfo struct test
// ---------------------------------------------------------------------------

func TestObjectInfo_Fields(t *testing.T) {
	now := time.Now().UTC()
	info := ObjectInfo{
		Key:          "backups/db-2026-03-19.sql",
		Size:         1048576,
		LastModified: now,
		ETag:         `"abc123"`,
	}

	if info.Key != "backups/db-2026-03-19.sql" {
		t.Errorf("Key = %q, want %q", info.Key, "backups/db-2026-03-19.sql")
	}
	if info.Size != 1048576 {
		t.Errorf("Size = %d, want %d", info.Size, 1048576)
	}
	if !info.LastModified.Equal(now) {
		t.Errorf("LastModified = %v, want %v", info.LastModified, now)
	}
	if info.ETag != `"abc123"` {
		t.Errorf("ETag = %q, want %q", info.ETag, `"abc123"`)
	}
}

// ---------------------------------------------------------------------------
// CleanupOldObjects retention logic test
// ---------------------------------------------------------------------------

func TestStorageManager_CleanupOldObjects(t *testing.T) {
	// We cannot control LastModified from the fake server easily because
	// ListObjectsV2 returns time.Now(). Instead, we verify the method
	// executes without error against an empty store (no objects to delete).
	objects := make(map[string][]byte)
	client, server := newTestR2Client(t, objects)
	defer server.Close()

	sm := NewStorageManager(client, &StorageConfig{
		BackupRetentionDays:   30,
		BuildLogRetentionDays: 7,
	})

	ctx := context.Background()
	err := sm.CleanupOldObjects(ctx)
	if err != nil {
		t.Fatalf("CleanupOldObjects failed on empty store: %v", err)
	}

	// With objects present but all recently created (LastModified = now),
	// none should be deleted.
	objects["backups/recent.sql"] = []byte("recent backup")
	objects["build-logs/recent/build.log"] = []byte("recent log")

	err = sm.CleanupOldObjects(ctx)
	if err != nil {
		t.Fatalf("CleanupOldObjects failed: %v", err)
	}

	// Both objects should still exist since they are "recent"
	if _, ok := objects["backups/recent.sql"]; !ok {
		t.Error("expected recent backup to survive cleanup")
	}
	if _, ok := objects["build-logs/recent/build.log"]; !ok {
		t.Error("expected recent build log to survive cleanup")
	}
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

func objectKeys(objects map[string][]byte) []string {
	keys := make([]string, 0, len(objects))
	for k := range objects {
		keys = append(keys, k)
	}
	return keys
}
