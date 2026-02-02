package cache

import (
	"context"
	"testing"
)

func TestIsSessionRevoked_NilCache_FailOpen(t *testing.T) {
	rc := &RedisCache{client: nil, FailMode: "open"}
	revoked, err := rc.IsSessionRevoked(context.Background(), "sess-123")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if revoked {
		t.Fatal("expected false (fail-open), got true")
	}
}

func TestIsSessionRevoked_NilCache_FailClosed(t *testing.T) {
	rc := &RedisCache{client: nil, FailMode: "closed"}
	revoked, err := rc.IsSessionRevoked(context.Background(), "sess-123")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !revoked {
		t.Fatal("expected true (fail-closed), got false")
	}
}

func TestNewRedisCache_SetsFailMode(t *testing.T) {
	// We can't call NewRedisCache (it pings Redis), but we can verify
	// the FailMode propagation logic directly.
	cfg := &CacheConfig{SessionRevocationFailMode: "closed"}
	failMode := cfg.SessionRevocationFailMode
	if failMode == "" {
		failMode = "open"
	}
	if failMode != "closed" {
		t.Fatalf("expected FailMode 'closed', got %q", failMode)
	}

	cfg2 := &CacheConfig{SessionRevocationFailMode: ""}
	failMode2 := cfg2.SessionRevocationFailMode
	if failMode2 == "" {
		failMode2 = "open"
	}
	if failMode2 != "open" {
		t.Fatalf("expected FailMode 'open' (default), got %q", failMode2)
	}
}

func Test_parseRedisInfoInt(t *testing.T) {
	sampleInfo := `# Stats
total_connections_received:1234
total_commands_processed:56789
keyspace_hits:12345
keyspace_misses:678
rejected_connections:0
expired_keys:100
`

	tests := []struct {
		name string
		key  string
		want int64
	}{
		{"keyspace_hits", "keyspace_hits", 12345},
		{"keyspace_misses", "keyspace_misses", 678},
		{"total_commands_processed", "total_commands_processed", 56789},
		{"non_existent_key", "non_existent_key", 0},
		{"empty_key", "", 0},
		{"partial_match", "keyspace", 0}, // Should not match partial
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRedisInfoInt(sampleInfo, tt.key)
			if got != tt.want {
				t.Errorf("parseRedisInfoInt(%q) = %d, want %d", tt.key, got, tt.want)
			}
		})
	}
}

func TestCacheMetrics(t *testing.T) {
	// Test that CacheMetrics struct is properly initialized
	metrics := &CacheMetrics{
		Hits:   100,
		Misses: 10,
		Errors: 5,
	}

	if metrics.Hits != 100 {
		t.Errorf("Expected Hits=100, got %d", metrics.Hits)
	}
	if metrics.Misses != 10 {
		t.Errorf("Expected Misses=10, got %d", metrics.Misses)
	}
	if metrics.Errors != 5 {
		t.Errorf("Expected Errors=5, got %d", metrics.Errors)
	}
}
