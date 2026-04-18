package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

// TestInit_TemplateCatalogValidation verifies unknown templates are
// rejected with a helpful error pointing at the known-slug list.
func TestInit_TemplateCatalogValidation(t *testing.T) {
	cmd := NewInitCommand(&config.Config{})
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--template", "nonexistent-framework", "svc"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown template, got nil")
	}
	if !strings.Contains(err.Error(), "unknown template") {
		t.Errorf("error missing 'unknown template' prefix: %v", err)
	}
	// The error should list known templates so users can pick one.
	if !strings.Contains(err.Error(), "nextjs") {
		t.Errorf("error missing catalog listing: %v", err)
	}
}

// TestInit_AcceptsCatalogSlug verifies a known catalog slug produces a
// service.yaml. We run in a temp dir so we don't pollute the repo.
func TestInit_AcceptsCatalogSlug(t *testing.T) {
	tmp := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	cmd := NewInitCommand(&config.Config{})
	cmd.SetArgs([]string{"--template", "go-fiber", "my-service"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("init with valid template failed: %v", err)
	}

	yaml, err := os.ReadFile(filepath.Join(tmp, "service.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(yaml), "name: my-service") {
		t.Errorf("service.yaml missing service name: %s", yaml)
	}
}

// TestDetectPort covers catalog-aware port selection.
func TestDetectPort(t *testing.T) {
	cases := map[string]int{
		"nextjs":    3000,
		"remix":     3000,
		"nuxtjs":    3000,
		"vite":      4173,
		"angular":   4200,
		"fastapi":   8000,
		"django":    8000,
		"flask":     5000,
		"go-fiber":  8080,
		"go-stdlib": 8080,
		"rust-axum": 8080,
		"phoenix":   8080,
		// Legacy aliases keep working.
		"node":   3000,
		"go":     8080,
		"python": 8000,
		"ruby":   3000,
		"gin":    8080,
		// Unknown → default.
		"unknown-thing": 8080,
	}
	for template, want := range cases {
		if got := detectPort(template); got != want {
			t.Errorf("detectPort(%q) = %d, want %d", template, got, want)
		}
	}
}

// TestStarterTemplateRepo verifies the starter repo naming convention.
func TestStarterTemplateRepo(t *testing.T) {
	if got := starterTemplateRepo("nextjs"); got != "madfam-org/nextjs-starter" {
		t.Errorf("starterTemplateRepo(nextjs) = %q, want madfam-org/nextjs-starter", got)
	}
	if got := starterTemplateRepo("unknown"); got != "" {
		t.Errorf("starterTemplateRepo(unknown) should be empty, got %q", got)
	}
	if got := starterTemplateRepo("auto"); got != "" {
		t.Errorf("starterTemplateRepo(auto) should be empty, got %q", got)
	}
}

// TestKnownTemplates sanity-checks the listing contains both sentinel
// and catalog slugs.
func TestKnownTemplates(t *testing.T) {
	tmpls := knownTemplates()
	want := []string{"auto", "nextjs", "fastapi", "go-fiber", "rust-axum"}
	for _, w := range want {
		found := false
		for _, slug := range tmpls {
			if slug == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("knownTemplates missing %q; got %v", w, tmpls)
		}
	}
	// Must NOT include "unknown" sentinel.
	for _, slug := range tmpls {
		if slug == "unknown" {
			t.Error("knownTemplates should exclude 'unknown' sentinel")
		}
	}
}
