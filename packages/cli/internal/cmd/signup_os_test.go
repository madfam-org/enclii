package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func TestSignupCommand_PrintsDefaultURL(t *testing.T) {
	cmd := NewSignupCommand(&config.Config{})
	cmd.SetArgs([]string{"--no-browser"})

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	t.Setenv("ENCLII_APP_BASE_URL", "")

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "https://app.enclii.dev/signup") {
		t.Errorf("output missing default signup URL:\n%s", got)
	}
	if !strings.Contains(got, "enclii login") {
		t.Errorf("output missing login follow-up instruction:\n%s", got)
	}
}

func TestSignupCommand_RespectsEnvOverride(t *testing.T) {
	cmd := NewSignupCommand(&config.Config{})
	cmd.SetArgs([]string{"--no-browser"})

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	t.Setenv("ENCLII_APP_BASE_URL", "https://staging.enclii.dev")

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "https://staging.enclii.dev/signup") {
		t.Errorf("output missing staging URL:\n%s", got)
	}
	if strings.Contains(got, "https://app.enclii.dev/signup") {
		t.Errorf("output leaked production URL when env override set:\n%s", got)
	}
}

func TestSignupCommand_NoBrowserSuppressesLaunchBanner(t *testing.T) {
	cmd := NewSignupCommand(&config.Config{})
	cmd.SetArgs([]string{"--no-browser"})

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := out.String()
	if strings.Contains(got, "Opening your default browser") {
		t.Errorf("--no-browser should not print the 'opening…' banner:\n%s", got)
	}
}
