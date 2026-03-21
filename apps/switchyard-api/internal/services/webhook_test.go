package services

import (
	"testing"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func TestWebhookService_ShouldRebuildService(t *testing.T) {
	svc := &WebhookService{}

	tests := []struct {
		name         string
		watchPaths   []string
		changedFiles []string
		want         bool
	}{
		{
			name:         "no watch paths — always rebuild",
			watchPaths:   nil,
			changedFiles: []string{"anything.go"},
			want:         true,
		},
		{
			name:         "empty watch paths — always rebuild",
			watchPaths:   []string{},
			changedFiles: []string{"anything.go"},
			want:         true,
		},
		{
			name:         "matching directory prefix",
			watchPaths:   []string{"apps/api/"},
			changedFiles: []string{"apps/api/main.go"},
			want:         true,
		},
		{
			name:         "no match — different directory",
			watchPaths:   []string{"apps/api/"},
			changedFiles: []string{"apps/web/index.ts"},
			want:         false,
		},
		{
			name:         "exact file match",
			watchPaths:   []string{"go.mod"},
			changedFiles: []string{"go.mod"},
			want:         true,
		},
		{
			name:         "directory prefix without trailing slash",
			watchPaths:   []string{"apps/api"},
			changedFiles: []string{"apps/api/handler.go"},
			want:         true,
		},
		{
			name:         "multiple watch paths — one matches",
			watchPaths:   []string{"apps/api/", "packages/shared/"},
			changedFiles: []string{"packages/shared/utils.go"},
			want:         true,
		},
		{
			name:         "multiple changed files — one matches",
			watchPaths:   []string{"apps/api/"},
			changedFiles: []string{"README.md", "apps/api/main.go", "docs/plan.md"},
			want:         true,
		},
		{
			name:         "no changed files",
			watchPaths:   []string{"apps/api/"},
			changedFiles: nil,
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := svc.ShouldRebuildService(tt.watchPaths, tt.changedFiles)
			if got != tt.want {
				t.Errorf("ShouldRebuildService(%v, %v) = %v, want %v",
					tt.watchPaths, tt.changedFiles, got, tt.want)
			}
		})
	}
}

func TestMatchesWatchPath(t *testing.T) {
	tests := []struct {
		name      string
		filePath  string
		watchPath string
		want      bool
	}{
		{"exact match", "go.mod", "go.mod", true},
		{"directory with slash", "apps/api/main.go", "apps/api/", true},
		{"directory without slash", "apps/api/main.go", "apps/api", true},
		{"no match", "apps/web/index.ts", "apps/api/", false},
		{"nested exact", "a/b/c/d.go", "a/b/c/d.go", true},
		{"partial name mismatch", "apps/api-v2/main.go", "apps/api/", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesWatchPath(tt.filePath, tt.watchPath)
			if got != tt.want {
				t.Errorf("matchesWatchPath(%q, %q) = %v, want %v",
					tt.filePath, tt.watchPath, got, tt.want)
			}
		})
	}
}

func TestWebhookService_ShouldRebuildMonorepo(t *testing.T) {
	svc := &WebhookService{}

	// Simulate a monorepo with 3 services watching different paths
	apiPaths := []string{"apps/api/", "packages/shared/"}
	webPaths := []string{"apps/web/", "packages/shared/"}
	workerPaths := []string{"apps/worker/"}

	changedFiles := []string{
		"apps/api/main.go",
		"apps/api/handlers.go",
		"packages/shared/utils.go",
	}

	if !svc.ShouldRebuildService(apiPaths, changedFiles) {
		t.Error("API should rebuild — matches apps/api/ and packages/shared/")
	}
	if !svc.ShouldRebuildService(webPaths, changedFiles) {
		t.Error("Web should rebuild — matches packages/shared/")
	}
	if svc.ShouldRebuildService(workerPaths, changedFiles) {
		t.Error("Worker should NOT rebuild — no matching files")
	}
}

func TestWebhookService_CreateReleaseForPush_Validation(t *testing.T) {
	svc := &WebhookService{} // repos is nil — we only test validation

	t.Run("nil service", func(t *testing.T) {
		_, err := svc.CreateReleaseForPush(nil, &CreateReleaseRequest{
			Service: nil,
			GitSHA:  "abc1234567890",
		})
		if err == nil {
			t.Error("expected validation error for nil service")
		}
	})

	t.Run("short git SHA", func(t *testing.T) {
		_, err := svc.CreateReleaseForPush(nil, &CreateReleaseRequest{
			Service: &types.Service{ID: uuid.New(), Name: "test"},
			GitSHA:  "abc",
		})
		if err == nil {
			t.Error("expected validation error for short SHA")
		}
	})
}
