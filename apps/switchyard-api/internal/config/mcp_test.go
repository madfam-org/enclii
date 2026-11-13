package config

import (
	"reflect"
	"testing"
)

func TestLoad_MCP_OffByDefault(t *testing.T) {
	t.Setenv("ENCLII_DATABASE_URL", "postgres://user:pass@localhost:5432/test?sslmode=disable")
	t.Setenv("ENCLII_ENVIRONMENT", "development")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := MCPConfig{Enabled: false, RatePerMinute: 60, Burst: 10}
	if !reflect.DeepEqual(cfg.MCP, want) {
		t.Fatalf("MCP = %+v, want %+v", cfg.MCP, want)
	}
}

func TestLoad_MCP_Override(t *testing.T) {
	t.Setenv("ENCLII_DATABASE_URL", "postgres://user:pass@localhost:5432/test?sslmode=disable")
	t.Setenv("ENCLII_ENVIRONMENT", "development")
	t.Setenv("ENCLII_MCP_ENABLED", "true")
	t.Setenv("ENCLII_MCP_ORG_IDS", " org-a, ,org-b ")
	t.Setenv("ENCLII_MCP_RATE_PER_MINUTE", "30")
	t.Setenv("ENCLII_MCP_BURST", "5")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := MCPConfig{Enabled: true, OrgIDs: []string{"org-a", "org-b"}, RatePerMinute: 30, Burst: 5}
	if !reflect.DeepEqual(cfg.MCP, want) {
		t.Fatalf("MCP = %+v, want %+v", cfg.MCP, want)
	}
}
