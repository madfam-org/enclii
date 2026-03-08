package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseEncliiYAML(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantErr     bool
		errContains string
		checkFn     func(t *testing.T, cfg *EncliiYAML)
	}{
		{
			name: "valid config with domains",
			input: `
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: my-api
  project: acme
spec:
  runtime:
    port: 8080
  domains:
    - name: api.example.com
      environment: production
      tlsEnabled: true
    - name: staging.example.com
      environment: staging
      tlsEnabled: false
`,
			wantErr: false,
			checkFn: func(t *testing.T, cfg *EncliiYAML) {
				if cfg.APIVersion != "enclii.dev/v1" {
					t.Errorf("APIVersion = %q, want enclii.dev/v1", cfg.APIVersion)
				}
				if cfg.Kind != "Service" {
					t.Errorf("Kind = %q, want Service", cfg.Kind)
				}
				if cfg.Metadata.Name != "my-api" {
					t.Errorf("Metadata.Name = %q, want my-api", cfg.Metadata.Name)
				}
				if cfg.Metadata.Project != "acme" {
					t.Errorf("Metadata.Project = %q, want acme", cfg.Metadata.Project)
				}
				if cfg.Spec.Runtime.Port != 8080 {
					t.Errorf("Spec.Runtime.Port = %d, want 8080", cfg.Spec.Runtime.Port)
				}
				if len(cfg.Spec.Domains) != 2 {
					t.Fatalf("len(Domains) = %d, want 2", len(cfg.Spec.Domains))
				}
				if cfg.Spec.Domains[0].Name != "api.example.com" {
					t.Errorf("Domains[0].Name = %q, want api.example.com", cfg.Spec.Domains[0].Name)
				}
				if cfg.Spec.Domains[0].Environment != "production" {
					t.Errorf("Domains[0].Environment = %q, want production", cfg.Spec.Domains[0].Environment)
				}
				if cfg.Spec.Domains[1].Name != "staging.example.com" {
					t.Errorf("Domains[1].Name = %q, want staging.example.com", cfg.Spec.Domains[1].Name)
				}
			},
		},
		{
			name: "valid config without domains",
			input: `
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: simple-svc
  project: proj
spec:
  runtime:
    port: 3000
`,
			wantErr: false,
			checkFn: func(t *testing.T, cfg *EncliiYAML) {
				if len(cfg.Spec.Domains) != 0 {
					t.Errorf("len(Domains) = %d, want 0", len(cfg.Spec.Domains))
				}
			},
		},
		{
			name: "missing apiVersion",
			input: `
kind: Service
metadata:
  name: test
  project: test
spec: {}
`,
			wantErr:     true,
			errContains: "unsupported apiVersion",
		},
		{
			name: "wrong apiVersion",
			input: `
apiVersion: enclii.dev/v2
kind: Service
metadata:
  name: test
  project: test
spec: {}
`,
			wantErr:     true,
			errContains: "unsupported apiVersion",
		},
		{
			name: "wrong kind",
			input: `
apiVersion: enclii.dev/v1
kind: Deployment
metadata:
  name: test
  project: test
spec: {}
`,
			wantErr:     true,
			errContains: "unsupported kind",
		},
		{
			name: "empty environment defaults to production",
			input: `
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: test
  project: test
spec:
  domains:
    - name: example.com
`,
			wantErr: false,
			checkFn: func(t *testing.T, cfg *EncliiYAML) {
				if len(cfg.Spec.Domains) != 1 {
					t.Fatalf("len(Domains) = %d, want 1", len(cfg.Spec.Domains))
				}
				if cfg.Spec.Domains[0].Environment != "production" {
					t.Errorf("Domains[0].Environment = %q, want production", cfg.Spec.Domains[0].Environment)
				}
			},
		},
		{
			name: "multiple domains all parsed",
			input: `
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: multi
  project: proj
spec:
  domains:
    - name: a.example.com
      environment: production
    - name: b.example.com
      environment: staging
    - name: c.example.com
      environment: development
`,
			wantErr: false,
			checkFn: func(t *testing.T, cfg *EncliiYAML) {
				if len(cfg.Spec.Domains) != 3 {
					t.Fatalf("len(Domains) = %d, want 3", len(cfg.Spec.Domains))
				}
				expected := []string{"a.example.com", "b.example.com", "c.example.com"}
				for i, exp := range expected {
					if cfg.Spec.Domains[i].Name != exp {
						t.Errorf("Domains[%d].Name = %q, want %q", i, cfg.Spec.Domains[i].Name, exp)
					}
				}
			},
		},
		{
			name: "valid config with custom headers",
			input: `
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: ws-service
  project: nuit
spec:
  runtime:
    port: 6001
  headers:
    Cross-Origin-Opener-Policy: same-origin
    Cross-Origin-Embedder-Policy: require-corp
    Access-Control-Allow-Origin: "https://nuit.one"
`,
			wantErr: false,
			checkFn: func(t *testing.T, cfg *EncliiYAML) {
				if len(cfg.Spec.Headers) != 3 {
					t.Fatalf("len(Headers) = %d, want 3", len(cfg.Spec.Headers))
				}
				if cfg.Spec.Headers["Cross-Origin-Opener-Policy"] != "same-origin" {
					t.Errorf("Headers[COOP] = %q, want same-origin", cfg.Spec.Headers["Cross-Origin-Opener-Policy"])
				}
				if cfg.Spec.Headers["Cross-Origin-Embedder-Policy"] != "require-corp" {
					t.Errorf("Headers[COEP] = %q, want require-corp", cfg.Spec.Headers["Cross-Origin-Embedder-Policy"])
				}
				if cfg.Spec.Headers["Access-Control-Allow-Origin"] != "https://nuit.one" {
					t.Errorf("Headers[ACAO] = %q, want https://nuit.one", cfg.Spec.Headers["Access-Control-Allow-Origin"])
				}
			},
		},
		{
			name: "config without headers has nil map",
			input: `
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: no-headers
  project: proj
spec:
  runtime:
    port: 3000
`,
			wantErr: false,
			checkFn: func(t *testing.T, cfg *EncliiYAML) {
				if cfg.Spec.Headers != nil && len(cfg.Spec.Headers) != 0 {
					t.Errorf("expected nil or empty Headers, got %v", cfg.Spec.Headers)
				}
			},
		},
		{
			name:        "invalid YAML",
			input:       `{{{not valid yaml`,
			wantErr:     true,
			errContains: "failed to parse",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ParseEncliiYAML([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.checkFn != nil {
				tt.checkFn(t, cfg)
			}
		})
	}
}

func TestIsTLSEnabled(t *testing.T) {
	tests := []struct {
		name       string
		tlsEnabled *bool
		want       bool
	}{
		{
			name:       "nil defaults to true",
			tlsEnabled: nil,
			want:       true,
		},
		{
			name:       "explicit true",
			tlsEnabled: boolPtr(true),
			want:       true,
		},
		{
			name:       "explicit false",
			tlsEnabled: boolPtr(false),
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &EncliiYAMLDomain{
				Name:       "example.com",
				TLSEnabled: tt.tlsEnabled,
			}
			if got := d.IsTLSEnabled(); got != tt.want {
				t.Errorf("IsTLSEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFetchGitHubRawFile(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		body         string
		token        string
		ref          string
		wantContent  bool
		wantErr      bool
		checkHeaders func(t *testing.T, r *http.Request)
	}{
		{
			name:        "200 OK returns content",
			statusCode:  http.StatusOK,
			body:        "apiVersion: enclii.dev/v1",
			wantContent: true,
			wantErr:     false,
		},
		{
			name:        "404 returns nil content and nil error",
			statusCode:  http.StatusNotFound,
			body:        `{"message":"Not Found"}`,
			wantContent: false,
			wantErr:     false,
		},
		{
			name:        "401 returns error",
			statusCode:  http.StatusUnauthorized,
			body:        `{"message":"Bad credentials"}`,
			wantContent: false,
			wantErr:     true,
		},
		{
			name:        "500 returns error",
			statusCode:  http.StatusInternalServerError,
			body:        `{"message":"Internal Server Error"}`,
			wantContent: false,
			wantErr:     true,
		},
		{
			name:       "Accept header is set correctly",
			statusCode: http.StatusOK,
			body:       "content",
			checkHeaders: func(t *testing.T, r *http.Request) {
				if got := r.Header.Get("Accept"); got != "application/vnd.github.raw+json" {
					t.Errorf("Accept header = %q, want application/vnd.github.raw+json", got)
				}
			},
			wantContent: true,
		},
		{
			name:       "Authorization header set when token provided",
			statusCode: http.StatusOK,
			body:       "content",
			token:      "ghp_test_token_123",
			checkHeaders: func(t *testing.T, r *http.Request) {
				if got := r.Header.Get("Authorization"); got != "Bearer ghp_test_token_123" {
					t.Errorf("Authorization header = %q, want 'Bearer ghp_test_token_123'", got)
				}
			},
			wantContent: true,
		},
		{
			name:       "no Authorization header when token empty",
			statusCode: http.StatusOK,
			body:       "content",
			token:      "",
			checkHeaders: func(t *testing.T, r *http.Request) {
				if got := r.Header.Get("Authorization"); got != "" {
					t.Errorf("Authorization header = %q, want empty", got)
				}
			},
			wantContent: true,
		},
		{
			name:       "ref query parameter when provided",
			statusCode: http.StatusOK,
			body:       "content",
			ref:        "abc123def",
			checkHeaders: func(t *testing.T, r *http.Request) {
				if got := r.URL.Query().Get("ref"); got != "abc123def" {
					t.Errorf("ref query param = %q, want abc123def", got)
				}
			},
			wantContent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.checkHeaders != nil {
					tt.checkHeaders(t, r)
				}
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			// Build URL with optional ref param to mirror fetchGitHubRawFile behavior
			apiURL := server.URL + "/repos/owner/repo/contents/enclii.yaml"
			if tt.ref != "" {
				apiURL += "?ref=" + tt.ref
			}

			content, err := testFetchGitHubRawFileHTTP(context.Background(), tt.token, apiURL)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantContent && content == nil {
				t.Fatal("expected content, got nil")
			}
			if !tt.wantContent && content != nil {
				t.Fatalf("expected nil content, got %q", string(content))
			}
		})
	}
}

// testFetchGitHubRawFileHTTP mirrors the HTTP logic in fetchGitHubRawFile
// against an arbitrary URL for test purposes.
func testFetchGitHubRawFileHTTP(ctx context.Context, token, apiURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/vnd.github.raw+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API error: %d", resp.StatusCode)
	}

	content, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("failed to read file content: %w", err)
	}

	return content, nil
}

// helpers

func boolPtr(v bool) *bool { return &v }
