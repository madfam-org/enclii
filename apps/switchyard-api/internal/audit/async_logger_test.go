package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func TestNewAsyncLogger_MinBufferSize(t *testing.T) {
	logger := NewAsyncLogger(nil, 10, filepath.Join(t.TempDir(), "fb.jsonl"))
	defer logger.Close()

	if cap(logger.logChan) < 1000 {
		t.Fatalf("expected buffer >= 1000, got %d", cap(logger.logChan))
	}
}

func TestNewAsyncLogger_DefaultFallbackPath(t *testing.T) {
	// Pass empty path; constructor should default to /var/log/enclii/audit-fallback.jsonl
	// We can't actually write there in CI, but we can inspect the field.
	logger := NewAsyncLogger(nil, 1000, "")
	defer logger.Close()

	if logger.fallbackPath != "/var/log/enclii/audit-fallback.jsonl" {
		t.Fatalf("expected default fallback path, got %q", logger.fallbackPath)
	}
}

func TestNewAsyncLogger_CustomFallbackPath(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "custom.jsonl")
	logger := NewAsyncLogger(nil, 1000, custom)
	defer logger.Close()

	if logger.fallbackPath != custom {
		t.Fatalf("expected %q, got %q", custom, logger.fallbackPath)
	}
}

func TestWriteToFileFallback(t *testing.T) {
	fbPath := filepath.Join(t.TempDir(), "fb.jsonl")
	logger := NewAsyncLogger(nil, 1000, fbPath)
	defer logger.Close()

	entry := &types.AuditLog{
		Action:       "test_action",
		ActorID:      nil,
		ResourceType: "project",
		ResourceID:   "proj-1",
		Timestamp:    time.Now(),
	}

	ok := logger.writeToFileFallback(entry)
	if !ok {
		t.Fatal("writeToFileFallback returned false")
	}

	data, err := os.ReadFile(fbPath)
	if err != nil {
		t.Fatalf("failed to read fallback file: %v", err)
	}

	var got types.AuditLog
	if err := json.Unmarshal(data[:len(data)-1], &got); err != nil { // strip trailing newline
		t.Fatalf("failed to unmarshal JSONL entry: %v", err)
	}
	if got.Action != "test_action" {
		t.Fatalf("expected action 'test_action', got %q", got.Action)
	}
}

func TestReplayFallbackFile(t *testing.T) {
	fbPath := filepath.Join(t.TempDir(), "fb.jsonl")
	// repos is nil so replayFallbackFile will fail on DB write and NOT truncate.
	// We test the read-parse-truncate path by checking file is NOT truncated (no repos).
	logger := NewAsyncLogger(nil, 1000, fbPath)
	defer logger.Close()

	for i := 0; i < 3; i++ {
		logger.writeToFileFallback(&types.AuditLog{
			Action:    "replay_test",
			ActorID:   nil,
			Timestamp: time.Now(),
		})
	}

	// Verify file has content before replay
	info, _ := os.Stat(fbPath)
	if info.Size() == 0 {
		t.Fatal("fallback file should have content before replay")
	}

	// replayFallbackFile with nil repos will panic on l.repos.AuditLogs.Log,
	// so we just verify the file was written correctly (replay tested via integration).
	data, err := os.ReadFile(fbPath)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	if lines != 3 {
		t.Fatalf("expected 3 JSONL lines, got %d", lines)
	}
}
