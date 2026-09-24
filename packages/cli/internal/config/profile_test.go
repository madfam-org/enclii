package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeProfileCreds stores credentials for profile under home, the way login does.
func writeProfileCreds(t *testing.T, home, profile, token string) {
	t.Helper()
	dir := filepath.Join(home, ".enclii")
	if profile != DefaultProfile {
		dir = filepath.Join(home, ".enclii", "profiles", profile)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	data, err := json.Marshal(Credentials{AccessToken: token, TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour), Issuer: "https://auth.example.com"})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "credentials.json"), data, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func TestNormalizeProfile(t *testing.T) {
	cases := map[string]string{"": DefaultProfile, "default": DefaultProfile, " admin ": "admin", "ops-2": "ops-2", "client_acme": "client_acme"}
	for in, want := range cases {
		got, err := NormalizeProfile(in)
		if err != nil || got != want {
			t.Errorf("NormalizeProfile(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"Admin", "../admin", "a/b", "-admin", "admin@madfam.io", "has space"} {
		if _, err := NormalizeProfile(bad); err == nil {
			t.Errorf("NormalizeProfile(%q) should fail", bad)
		}
	}
}

func TestGetCredentialsPath_PerProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Cleanup(func() { _ = SetProfile("") })

	if err := SetProfile(""); err != nil {
		t.Fatal(err)
	}
	if got, want := GetCredentialsPath(), filepath.Join(home, ".enclii", "credentials.json"); got != want {
		t.Errorf("default profile path = %q, want %q", got, want)
	}
	if err := SetProfile("admin"); err != nil {
		t.Fatal(err)
	}
	if got, want := GetCredentialsPath(), filepath.Join(home, ".enclii", "profiles", "admin", "credentials.json"); got != want {
		t.Errorf("admin profile path = %q, want %q", got, want)
	}
}

func TestLoad_ProfileFromEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ENCLII_API_TOKEN", "")
	t.Setenv("ENCLII_TOKEN", "")
	t.Setenv("ENCLII_LOG_LEVEL", "")
	t.Cleanup(func() { _ = SetProfile("") })
	writeProfileCreds(t, home, DefaultProfile, "everyday-token")
	writeProfileCreds(t, home, "admin", "admin-token")

	t.Setenv("ENCLII_PROFILE", "admin")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if ActiveProfile() != "admin" || cfg.APIToken != "admin-token" {
		t.Errorf("profile %q token %q, want admin/admin-token", ActiveProfile(), cfg.APIToken)
	}

	t.Setenv("ENCLII_PROFILE", "")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if ActiveProfile() != DefaultProfile || cfg.APIToken != "everyday-token" {
		t.Errorf("profile %q token %q, want default/everyday-token", ActiveProfile(), cfg.APIToken)
	}
}

func TestLoad_InvalidProfileEnv(t *testing.T) {
	t.Setenv("ENCLII_PROFILE", "../escape")
	t.Setenv("ENCLII_LOG_LEVEL", "")
	t.Cleanup(func() { _ = SetProfile("") })
	if _, err := Load(); err == nil {
		t.Fatal("Load() should reject an invalid ENCLII_PROFILE")
	}
}

func TestReloadCredentials_SwitchesProfileButExplicitTokenWins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ENCLII_API_TOKEN", "")
	t.Setenv("ENCLII_TOKEN", "")
	t.Setenv("ENCLII_PROFILE", "")
	t.Setenv("ENCLII_LOG_LEVEL", "")
	t.Cleanup(func() { _ = SetProfile("") })
	writeProfileCreds(t, home, DefaultProfile, "everyday-token")
	writeProfileCreds(t, home, "admin", "admin-token")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.APIToken != "everyday-token" {
		t.Fatalf("APIToken = %q, want everyday-token", cfg.APIToken)
	}

	// --profile admin: the root command switches and reloads.
	if err := SetProfile("admin"); err != nil {
		t.Fatal(err)
	}
	cfg.ReloadCredentials()
	if cfg.APIToken != "admin-token" || cfg.Credentials == nil || cfg.Credentials.AccessToken != "admin-token" {
		t.Errorf("after switching to admin, APIToken = %q", cfg.APIToken)
	}

	// A profile with no login leaves no stale token behind.
	if err := SetProfile("nobody"); err != nil {
		t.Fatal(err)
	}
	cfg.ReloadCredentials()
	if cfg.APIToken != "" || cfg.Credentials != nil {
		t.Errorf("profile without credentials kept token %q", cfg.APIToken)
	}

	// --api-token is explicit and survives a reload.
	cfg.SetAPIToken("explicit-token")
	if err := SetProfile("admin"); err != nil {
		t.Fatal(err)
	}
	cfg.ReloadCredentials()
	if cfg.APIToken != "explicit-token" {
		t.Errorf("explicit token overridden: %q", cfg.APIToken)
	}
}
