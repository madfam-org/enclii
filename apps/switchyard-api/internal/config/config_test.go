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

func TestLoad_ArgocdRegistrationMode_Default(t *testing.T) {
	t.Setenv("ENCLII_DATABASE_URL", "postgres://user:pass@localhost:5432/test?sslmode=disable")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ArgocdRegistrationMode != "runtime" {
		t.Fatalf("expected default runtime mode, got %q", cfg.ArgocdRegistrationMode)
	}
	if cfg.AllowLegacyGitOpsRegistration {
		t.Fatal("expected legacy gitops registration to be disabled by default")
	}
	if cfg.ArgocdNamespace != "argocd" {
		t.Fatalf("expected default argocd namespace, got %q", cfg.ArgocdNamespace)
	}
}

func TestLoad_ArgocdRegistrationMode_Override(t *testing.T) {
	t.Setenv("ENCLII_DATABASE_URL", "postgres://user:pass@localhost:5432/test?sslmode=disable")
	t.Setenv("ENCLII_ARGOCD_REGISTRATION_MODE", "runtime")
	t.Setenv("ENCLII_ARGOCD_NAMESPACE", "custom-argocd")
	t.Setenv("ENCLII_ALLOW_LEGACY_GITOPS_REGISTRATION", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ArgocdRegistrationMode != "runtime" {
		t.Fatalf("expected runtime mode, got %q", cfg.ArgocdRegistrationMode)
	}
	if !cfg.AllowLegacyGitOpsRegistration {
		t.Fatal("expected legacy gitops registration override to load")
	}
	if cfg.ArgocdNamespace != "custom-argocd" {
		t.Fatalf("expected custom namespace, got %q", cfg.ArgocdNamespace)
	}
}

func TestLoad_StatusProjectionMode_Default(t *testing.T) {
	t.Setenv("ENCLII_DATABASE_URL", "postgres://user:pass@localhost:5432/test?sslmode=disable")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.StatusProjectionMode != "runtime" {
		t.Fatalf("expected default runtime mode, got %q", cfg.StatusProjectionMode)
	}
	if cfg.AllowLegacyGitOpsStatusProjection {
		t.Fatal("expected legacy gitops status projection to be disabled by default")
	}
	if cfg.StatusConfigNamespace != "enclii" {
		t.Fatalf("expected default status namespace, got %q", cfg.StatusConfigNamespace)
	}
}

func TestLoad_StatusProjectionMode_Override(t *testing.T) {
	t.Setenv("ENCLII_DATABASE_URL", "postgres://user:pass@localhost:5432/test?sslmode=disable")
	t.Setenv("ENCLII_STATUS_PROJECTION_MODE", "runtime")
	t.Setenv("ENCLII_STATUS_CONFIG_NAMESPACE", "status")
	t.Setenv("ENCLII_ALLOW_LEGACY_GITOPS_STATUS_PROJECTION", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.StatusProjectionMode != "runtime" {
		t.Fatalf("expected runtime mode, got %q", cfg.StatusProjectionMode)
	}
	if !cfg.AllowLegacyGitOpsStatusProjection {
		t.Fatal("expected legacy gitops status projection override to load")
	}
	if cfg.StatusConfigNamespace != "status" {
		t.Fatalf("expected custom status namespace, got %q", cfg.StatusConfigNamespace)
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

// TestLoad_SignupEnabledRequiresJanuaAdminToken covers the fail-fast added
// alongside the other SEC-00x checks: ENCLII_SIGNUP_ENABLED=true without
// ENCLII_JANUA_ADMIN_TOKEN must refuse to start rather than let the signup
// wizard's GitHub-link step dead-end on an unauthenticated Janua call.
func TestLoad_SignupEnabledRequiresJanuaAdminToken(t *testing.T) {
	t.Setenv("ENCLII_DATABASE_URL", "postgres://user:pass@localhost:5432/test?sslmode=disable")
	t.Setenv("ENCLII_SIGNUP_ENABLED", "true")
	t.Setenv("ENCLII_JANUA_ADMIN_TOKEN", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when ENCLII_SIGNUP_ENABLED=true and ENCLII_JANUA_ADMIN_TOKEN is empty")
	}
	if !strings.Contains(err.Error(), "ENCLII_JANUA_ADMIN_TOKEN") {
		t.Fatalf("error should mention ENCLII_JANUA_ADMIN_TOKEN, got: %v", err)
	}
	if !strings.Contains(err.Error(), "ENCLII_SIGNUP_ENABLED") {
		t.Fatalf("error should mention ENCLII_SIGNUP_ENABLED for context, got: %v", err)
	}
}

func TestLoad_SignupEnabledWithJanuaAdminTokenSucceeds(t *testing.T) {
	t.Setenv("ENCLII_DATABASE_URL", "postgres://user:pass@localhost:5432/test?sslmode=disable")
	t.Setenv("ENCLII_SIGNUP_ENABLED", "true")
	t.Setenv("ENCLII_JANUA_ADMIN_TOKEN", "test-admin-token")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.SignupEnabled {
		t.Fatal("expected SignupEnabled to be true")
	}
	if cfg.JanuaAdminToken != "test-admin-token" {
		t.Fatalf("expected JanuaAdminToken to load through, got %q", cfg.JanuaAdminToken)
	}
}

func TestLoad_SignupDisabledAllowsEmptyJanuaAdminToken(t *testing.T) {
	t.Setenv("ENCLII_DATABASE_URL", "postgres://user:pass@localhost:5432/test?sslmode=disable")
	t.Setenv("ENCLII_SIGNUP_ENABLED", "false")
	t.Setenv("ENCLII_JANUA_ADMIN_TOKEN", "")

	_, err := Load()
	if err != nil {
		t.Fatalf("unexpected error when signup is disabled: %v", err)
	}
}
