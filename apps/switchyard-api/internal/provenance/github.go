package provenance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

// GitHubClient handles interactions with GitHub API for PR verification
type GitHubClient struct {
	token      string
	httpClient *http.Client
	baseURL    string
}

// NewGitHubClient creates a new GitHub API client
func NewGitHubClient(token string) *GitHubClient {
	return &GitHubClient{
		token:      token,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    "https://api.github.com",
	}
}

// PullRequest represents a GitHub pull request
type PullRequest struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	State       string    `json:"state"`
	MergedAt    time.Time `json:"merged_at"`
	HTMLURL     string    `json:"html_url"`
	MergeCommit string    `json:"merge_commit_sha"`
	Head        struct {
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref  string `json:"ref"`
		Repo struct {
			Name  string `json:"name"`
			Owner struct {
				Login string `json:"login"`
			} `json:"owner"`
		} `json:"repo"`
	} `json:"base"`
}

// Review represents a GitHub PR review
type Review struct {
	ID          int64     `json:"id"`
	User        User      `json:"user"`
	State       string    `json:"state"`
	SubmittedAt time.Time `json:"submitted_at"`
}

// User represents a GitHub user
type User struct {
	Login string `json:"login"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

// CheckStatus represents CI check status
type CheckStatus struct {
	State      string `json:"state"` // success, failure, pending
	TotalCount int    `json:"total_count"`
	Statuses   []struct {
		State   string `json:"state"`
		Context string `json:"context"`
	} `json:"statuses"`
}

// ParsePRURL extracts owner, repo, and PR number from a GitHub PR URL
// Supports formats:
//   - https://github.com/owner/repo/pull/123
//   - https://github.com/owner/repo/pulls/123
func ParsePRURL(url string) (owner, repo string, prNumber int, err error) {
	re := regexp.MustCompile(`github\.com/([^/]+)/([^/]+)/pulls?/(\d+)`)
	matches := re.FindStringSubmatch(url)
	if len(matches) != 4 {
		return "", "", 0, fmt.Errorf("invalid GitHub PR URL format: %s", url)
	}

	prNumber, err = strconv.Atoi(matches[3])
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid PR number: %s", matches[3])
	}

	return matches[1], matches[2], prNumber, nil
}

// GetPullRequest fetches PR details from GitHub
func (g *GitHubClient) GetPullRequest(ctx context.Context, owner, repo string, prNumber int) (*PullRequest, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", g.baseURL, owner, repo, prNumber)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("token %s", g.token))
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch PR: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API error: %s", resp.Status)
	}

	var pr PullRequest
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, fmt.Errorf("failed to decode PR response: %w", err)
	}

	return &pr, nil
}

// GetPRReviews fetches all reviews for a pull request
func (g *GitHubClient) GetPRReviews(ctx context.Context, owner, repo string, prNumber int) ([]Review, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/reviews", g.baseURL, owner, repo, prNumber)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("token %s", g.token))
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch reviews: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API error: %s", resp.Status)
	}

	var reviews []Review
	if err := json.NewDecoder(resp.Body).Decode(&reviews); err != nil {
		return nil, fmt.Errorf("failed to decode reviews response: %w", err)
	}

	return reviews, nil
}

// GetCheckStatus fetches CI check status for a commit
func (g *GitHubClient) GetCheckStatus(ctx context.Context, owner, repo, sha string) (*CheckStatus, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/commits/%s/status", g.baseURL, owner, repo, sha)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("token %s", g.token))
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch check status: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API error: %s", resp.Status)
	}

	var status CheckStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to decode status response: %w", err)
	}

	return &status, nil
}

// FindPRByCommit searches for a PR that contains the given commit SHA
// This is useful when you have a commit SHA but not the PR URL
func (g *GitHubClient) FindPRByCommit(ctx context.Context, owner, repo, commitSHA string) (*PullRequest, error) {
	// GitHub search API: find PRs that contain this commit
	url := fmt.Sprintf("%s/repos/%s/%s/commits/%s/pulls", g.baseURL, owner, repo, commitSHA)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("token %s", g.token))
	req.Header.Set("Accept", "application/vnd.github.groot-preview+json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to search for PR: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API error: %s", resp.Status)
	}

	var prs []PullRequest
	if err := json.NewDecoder(resp.Body).Decode(&prs); err != nil {
		return nil, fmt.Errorf("failed to decode PR search response: %w", err)
	}

	if len(prs) == 0 {
		return nil, fmt.Errorf("no PR found for commit %s", commitSHA)
	}

	// Return the first merged PR (most recent)
	for _, pr := range prs {
		if pr.State == "closed" && !pr.MergedAt.IsZero() {
			return &pr, nil
		}
	}

	return nil, fmt.Errorf("no merged PR found for commit %s", commitSHA)
}
