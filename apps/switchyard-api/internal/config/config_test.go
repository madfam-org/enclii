package config

import (
	"strings"
	"testing"
)

func TestLoad_RequiresDatabaseURL(t *testing.T) {
	// Clear any existing env that might set the DB URL
	t.Setenv("ENCLII_DATABASE_URL", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when DATABASE_URL is empty")
	}
	if !strings.Contains(err.Error(), "ENCLII_DATABASE_URL") {
		t.Fatalf("error should mention ENCLII_DATABASE_URL, got: %v", err)
	}
}

func TestLoad_SessionRevocationFailMode_Default(t *testing.T) {
	t.Setenv("ENCLII_DATABASE_URL", "postgres://user:pass@localhost:5432/test?sslmode=disable")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SessionRevocationFailMode != "closed" {
		t.Fatalf("expected default 'closed', got %q", cfg.SessionRevocationFailMode)
	}
}

func TestLoad_SessionRevocationFailMode_Override(t *testing.T) {
	t.Setenv("ENCLII_DATABASE_URL", "postgres://user:pass@localhost:5432/test?sslmode=disable")
	t.Setenv("ENCLII_SESSION_REVOCATION_FAIL_MODE", "open")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SessionRevocationFailMode != "open" {
		t.Fatalf("expected 'open', got %q", cfg.SessionRevocationFailMode)
	}
}

func TestLoad_SEC003_ProductionRequiresAllowedOrigins(t *testing.T) {
	t.Setenv("ENCLII_DATABASE_URL", "postgres://user:pass@localhost:5432/test?sslmode=disable")
	t.Setenv("ENCLII_ENVIRONMENT", "production")
	t.Setenv("ENCLII_ALLOWED_ORIGINS", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when ENCLII_ALLOWED_ORIGINS is empty in production")
	}
	if !strings.Contains(err.Error(), "SEC-003") {
		t.Fatalf("error should mention SEC-003, got: %v", err)
	}
}

func TestLoad_SEC003_NonProductionAllowsEmptyOrigins(t *testing.T) {
	t.Setenv("ENCLII_DATABASE_URL", "postgres://user:pass@localhost:5432/test?sslmode=disable")
	t.Setenv("ENCLII_ENVIRONMENT", "development")
	t.Setenv("ENCLII_ALLOWED_ORIGINS", "")

	_, err := Load()
	if err != nil {
		t.Fatalf("unexpected error in development mode: %v", err)
	}
}

func TestLoad_SEC003_ProductionWithOriginsSucceeds(t *testing.T) {
	t.Setenv("ENCLII_DATABASE_URL", "postgres://user:pass@localhost:5432/test?sslmode=disable")
	t.Setenv("ENCLII_ENVIRONMENT", "production")
	t.Setenv("ENCLII_ALLOWED_ORIGINS", "https://app.enclii.dev")

	_, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
