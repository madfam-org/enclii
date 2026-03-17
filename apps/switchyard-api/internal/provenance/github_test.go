package provenance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePRURL(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		wantOwner  string
		wantRepo   string
		wantNumber int
		wantErr    bool
	}{
		{
			name:       "standard pull URL",
			url:        "https://github.com/madfam-org/enclii/pull/42",
			wantOwner:  "madfam-org",
			wantRepo:   "enclii",
			wantNumber: 42,
		},
		{
			name:       "pulls plural URL",
			url:        "https://github.com/madfam-org/enclii/pulls/42",
			wantOwner:  "madfam-org",
			wantRepo:   "enclii",
			wantNumber: 42,
		},
		{
			name:       "large PR number",
			url:        "https://github.com/owner/repo/pull/99999",
			wantOwner:  "owner",
			wantRepo:   "repo",
			wantNumber: 99999,
		},
		{
			name:       "PR number 1",
			url:        "https://github.com/o/r/pull/1",
			wantOwner:  "o",
			wantRepo:   "r",
			wantNumber: 1,
		},
		{
			name:       "owner and repo with hyphens",
			url:        "https://github.com/my-org/my-repo/pull/7",
			wantOwner:  "my-org",
			wantRepo:   "my-repo",
			wantNumber: 7,
		},
		{
			name:       "owner and repo with dots and underscores",
			url:        "https://github.com/my.org/my_repo/pull/10",
			wantOwner:  "my.org",
			wantRepo:   "my_repo",
			wantNumber: 10,
		},
		{
			name:    "missing PR number",
			url:     "https://github.com/owner/repo/pull/",
			wantErr: true,
		},
		{
			name:    "completely wrong URL",
			url:     "https://gitlab.com/owner/repo/merge_requests/1",
			wantErr: true,
		},
		{
			name:    "empty string",
			url:     "",
			wantErr: true,
		},
		{
			name:    "no path after repo",
			url:     "https://github.com/owner/repo",
			wantErr: true,
		},
		{
			name:    "issues URL not a PR URL",
			url:     "https://github.com/owner/repo/issues/5",
			wantErr: true,
		},
		{
			name:    "PR number is non-numeric",
			url:     "https://github.com/owner/repo/pull/abc",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, number, err := ParsePRURL(tt.url)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantOwner, owner)
			assert.Equal(t, tt.wantRepo, repo)
			assert.Equal(t, tt.wantNumber, number)
		})
	}
}

func TestNewGitHubClient(t *testing.T) {
	client := NewGitHubClient("ghp_test_token")
	assert.NotNil(t, client)
	assert.Equal(t, "ghp_test_token", client.token)
	assert.Equal(t, "https://api.github.com", client.baseURL)
	assert.NotNil(t, client.httpClient)
}

// newTestServer creates an httptest server and a GitHubClient pointing to it.
func newTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *GitHubClient) {
	t.Helper()
	server := httptest.NewServer(handler)
	client := &GitHubClient{
		token:      "test-token",
		httpClient: server.Client(),
		baseURL:    server.URL,
	}
	return server, client
}

func TestGetPullRequest(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       interface{}
		wantErr    bool
		check      func(t *testing.T, pr *PullRequest)
	}{
		{
			name:       "successful fetch",
			statusCode: http.StatusOK,
			body: PullRequest{
				Number:      42,
				Title:       "feat: add feature",
				State:       "closed",
				MergedAt:    time.Date(2026, 3, 10, 14, 0, 0, 0, time.UTC),
				HTMLURL:     "https://github.com/org/repo/pull/42",
				MergeCommit: "abc123",
			},
			check: func(t *testing.T, pr *PullRequest) {
				assert.Equal(t, 42, pr.Number)
				assert.Equal(t, "feat: add feature", pr.Title)
				assert.Equal(t, "closed", pr.State)
				assert.Equal(t, "abc123", pr.MergeCommit)
			},
		},
		{
			name:       "not found returns error",
			statusCode: http.StatusNotFound,
			body:       map[string]string{"message": "Not Found"},
			wantErr:    true,
		},
		{
			name:       "unauthorized returns error",
			statusCode: http.StatusUnauthorized,
			body:       map[string]string{"message": "Bad credentials"},
			wantErr:    true,
		},
		{
			name:       "server error returns error",
			statusCode: http.StatusInternalServerError,
			body:       map[string]string{"message": "Internal Server Error"},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				// Verify auth header is set
				assert.Contains(t, r.Header.Get("Authorization"), "token test-token")
				assert.Equal(t, "application/vnd.github.v3+json", r.Header.Get("Accept"))

				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.body)
			})
			defer server.Close()

			pr, err := client.GetPullRequest(context.Background(), "org", "repo", 42)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, pr)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, pr)
			tt.check(t, pr)
		})
	}
}

func TestGetPullRequest_VerifiesRequestPath(t *testing.T) {
	server, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/my-org/my-repo/pulls/99", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(PullRequest{Number: 99})
	})
	defer server.Close()

	pr, err := client.GetPullRequest(context.Background(), "my-org", "my-repo", 99)
	require.NoError(t, err)
	assert.Equal(t, 99, pr.Number)
}

func TestGetPRReviews(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       interface{}
		wantErr    bool
		wantCount  int
	}{
		{
			name:       "successful fetch with reviews",
			statusCode: http.StatusOK,
			body: []Review{
				{ID: 1, User: User{Login: "alice"}, State: "APPROVED"},
				{ID: 2, User: User{Login: "bob"}, State: "CHANGES_REQUESTED"},
			},
			wantCount: 2,
		},
		{
			name:       "empty reviews list",
			statusCode: http.StatusOK,
			body:       []Review{},
			wantCount:  0,
		},
		{
			name:       "API error",
			statusCode: http.StatusForbidden,
			body:       map[string]string{"message": "Forbidden"},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.body)
			})
			defer server.Close()

			reviews, err := client.GetPRReviews(context.Background(), "org", "repo", 10)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, reviews, tt.wantCount)
		})
	}
}

func TestGetPRReviews_VerifiesRequestPath(t *testing.T) {
	server, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/org/repo/pulls/55/reviews", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]Review{})
	})
	defer server.Close()

	_, err := client.GetPRReviews(context.Background(), "org", "repo", 55)
	require.NoError(t, err)
}

func TestGetCheckStatus(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       interface{}
		wantErr    bool
		wantState  string
	}{
		{
			name:       "all checks passing",
			statusCode: http.StatusOK,
			body: CheckStatus{
				State:      "success",
				TotalCount: 3,
			},
			wantState: "success",
		},
		{
			name:       "checks failing",
			statusCode: http.StatusOK,
			body: CheckStatus{
				State:      "failure",
				TotalCount: 2,
			},
			wantState: "failure",
		},
		{
			name:       "API error",
			statusCode: http.StatusNotFound,
			body:       map[string]string{"message": "Not Found"},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.body)
			})
			defer server.Close()

			status, err := client.GetCheckStatus(context.Background(), "org", "repo", "abc123")
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantState, status.State)
		})
	}
}

func TestGetCheckStatus_VerifiesRequestPath(t *testing.T) {
	server, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/org/repo/commits/deadbeef/status", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(CheckStatus{State: "success"})
	})
	defer server.Close()

	_, err := client.GetCheckStatus(context.Background(), "org", "repo", "deadbeef")
	require.NoError(t, err)
}

func TestFindPRByCommit(t *testing.T) {
	mergedTime := time.Date(2026, 3, 10, 14, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		statusCode int
		body       interface{}
		wantErr    bool
		wantNumber int
		errMsg     string
	}{
		{
			name:       "finds merged PR",
			statusCode: http.StatusOK,
			body: []PullRequest{
				{Number: 10, State: "open"},
				{Number: 20, State: "closed", MergedAt: mergedTime},
				{Number: 30, State: "closed", MergedAt: mergedTime},
			},
			wantNumber: 20, // first merged PR
		},
		{
			name:       "no PRs found",
			statusCode: http.StatusOK,
			body:       []PullRequest{},
			wantErr:    true,
			errMsg:     "no PR found for commit",
		},
		{
			name:       "PRs found but none merged",
			statusCode: http.StatusOK,
			body: []PullRequest{
				{Number: 10, State: "open"},
				{Number: 20, State: "closed"}, // closed but MergedAt is zero
			},
			wantErr: true,
			errMsg:  "no merged PR found for commit",
		},
		{
			name:       "API error",
			statusCode: http.StatusNotFound,
			body:       map[string]string{"message": "Not Found"},
			wantErr:    true,
			errMsg:     "GitHub API error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				// Verify the groot-preview accept header for commit-to-PR lookup
				assert.Equal(t, "application/vnd.github.groot-preview+json", r.Header.Get("Accept"))
				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.body)
			})
			defer server.Close()

			pr, err := client.FindPRByCommit(context.Background(), "org", "repo", "abc123")
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}
			require.NoError(t, err)
			require.NotNil(t, pr)
			assert.Equal(t, tt.wantNumber, pr.Number)
		})
	}
}

func TestFindPRByCommit_VerifiesRequestPath(t *testing.T) {
	server, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/org/repo/commits/sha256abc/pulls", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		mergedTime := time.Date(2026, 3, 10, 14, 0, 0, 0, time.UTC)
		_ = json.NewEncoder(w).Encode([]PullRequest{
			{Number: 1, State: "closed", MergedAt: mergedTime},
		})
	})
	defer server.Close()

	pr, err := client.FindPRByCommit(context.Background(), "org", "repo", "sha256abc")
	require.NoError(t, err)
	assert.Equal(t, 1, pr.Number)
}

func TestGetPullRequest_CancelledContext(t *testing.T) {
	server, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(PullRequest{Number: 1})
	})
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := client.GetPullRequest(ctx, "org", "repo", 1)
	assert.Error(t, err)
}

func TestGetPRReviews_InvalidResponseBody(t *testing.T) {
	server, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Write invalid JSON for an array endpoint
		_, _ = w.Write([]byte(`{"not": "an array"}`))
	})
	defer server.Close()

	_, err := client.GetPRReviews(context.Background(), "org", "repo", 1)
	assert.Error(t, err)
}
