package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListGitHubDirectory(t *testing.T) {
	tests := []struct {
		name      string
		handler   http.HandlerFunc
		wantFiles int
		wantErr   bool
	}{
		{
			"success with files",
			func(w http.ResponseWriter, r *http.Request) {
				entries := []gitHubDirEntry{
					{Name: "deployment.yaml", Type: "file"},
					{Name: "service.yaml", Type: "file"},
					{Name: "kustomization.yaml", Type: "file"},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(entries)
			},
			3,
			false,
		},
		{
			"directory not found",
			func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Not Found"}`))
			},
			0,
			true,
		},
		{
			"empty directory",
			func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`[]`))
			},
			0,
			false,
		},
		{
			"mixed files and dirs",
			func(w http.ResponseWriter, r *http.Request) {
				entries := []gitHubDirEntry{
					{Name: "deployment.yaml", Type: "file"},
					{Name: "subdir", Type: "dir"},
					{Name: "README.md", Type: "file"},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(entries)
			},
			3,
			false,
		},
		{
			"server error",
			func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"message":"Internal Server Error"}`))
			},
			0,
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			ctx := context.Background()
			files, err := listGitHubDirectoryWithBaseURL(ctx, "fake-token", "owner", "repo", "k8s/production", "main", server.URL)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(files) != tt.wantFiles {
				t.Errorf("got %d files, want %d", len(files), tt.wantFiles)
			}
		})
	}
}

func TestListGitHubDirectory_AuthHeader(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer server.Close()

	ctx := context.Background()
	_, _ = listGitHubDirectoryWithBaseURL(ctx, "test-token-123", "owner", "repo", "path", "", server.URL)

	if gotAuth != "Bearer test-token-123" {
		t.Errorf("expected Authorization header 'Bearer test-token-123', got %q", gotAuth)
	}
}

func TestListGitHubDirectory_NoToken(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer server.Close()

	ctx := context.Background()
	_, _ = listGitHubDirectoryWithBaseURL(ctx, "", "owner", "repo", "path", "", server.URL)

	if gotAuth != "" {
		t.Errorf("expected no Authorization header when token is empty, got %q", gotAuth)
	}
}

func TestListGitHubDirectory_RefParam(t *testing.T) {
	var gotURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer server.Close()

	ctx := context.Background()
	_, _ = listGitHubDirectoryWithBaseURL(ctx, "", "owner", "repo", "k8s/prod", "develop", server.URL)

	expected := "/repos/owner/repo/contents/k8s/prod?ref=develop"
	if gotURL != expected {
		t.Errorf("expected URL %q, got %q", expected, gotURL)
	}
}
