// Repo metadata batch endpoint for the dashboard.
//
// Backs the at-a-glance public/private indicator and key stats on each project
// card. The dashboard renders ~25 project cards on first load — we cache repo
// metadata in-process for 5 minutes so a refresh-storm doesn't fan out to
// GitHub. Per-user user-token (X-IDP-Token → Janua → GH access token) auth
// matches the rest of /v1/integrations/github/*.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
)

const (
	repoMetadataCacheTTL = 5 * time.Minute
	repoMetadataMaxBatch = 64
)

// RepoMetadata is the at-a-glance subset of GitHub repo data the dashboard
// renders on each project card. Fields are explicit (vs. embedding the full
// GitHub payload) so the contract stays small and the JSON shape doesn't drift
// when GitHub adds fields.
type RepoMetadata struct {
	FullName      string    `json:"full_name"`
	Private       bool      `json:"private"`
	Visibility    string    `json:"visibility"` // public|private|internal
	HTMLURL       string    `json:"html_url"`
	Description   string    `json:"description,omitempty"`
	Language      string    `json:"language,omitempty"`
	License       string    `json:"license,omitempty"` // SPDX id when present
	DefaultBranch string    `json:"default_branch,omitempty"`
	Stars         int       `json:"stars"`
	Forks         int       `json:"forks"`
	OpenIssues    int       `json:"open_issues"`
	Archived      bool      `json:"archived"`
	Disabled      bool      `json:"disabled"`
	Fork          bool      `json:"fork"`
	IsTemplate    bool      `json:"is_template"`
	Topics        []string  `json:"topics,omitempty"`
	PushedAt      time.Time `json:"pushed_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// repoMetadataResp is the GitHub /repos/{owner}/{repo} subset we decode.
// Kept separate from RepoMetadata so we never accidentally leak GH-internal
// fields (clone URLs, owner sub-objects, etc.) to the browser.
type repoMetadataResp struct {
	FullName      string   `json:"full_name"`
	Private       bool     `json:"private"`
	Visibility    string   `json:"visibility"`
	HTMLURL       string   `json:"html_url"`
	Description   string   `json:"description"`
	Language      string   `json:"language"`
	DefaultBranch string   `json:"default_branch"`
	Stargazers    int      `json:"stargazers_count"`
	Forks         int      `json:"forks_count"`
	OpenIssues    int      `json:"open_issues_count"`
	Archived      bool     `json:"archived"`
	Disabled      bool     `json:"disabled"`
	Fork          bool     `json:"fork"`
	IsTemplate    bool     `json:"is_template"`
	Topics        []string `json:"topics"`
	PushedAt      string   `json:"pushed_at"`
	UpdatedAt     string   `json:"updated_at"`
	License       *struct {
		SPDXID string `json:"spdx_id"`
		Key    string `json:"key"`
	} `json:"license"`
}

type repoMetadataCacheEntry struct {
	meta      RepoMetadata
	expiresAt time.Time
}

// repoMetadataCache is process-local. Sufficient for the dashboard's read
// pattern: 25 keys × 5-min TTL × per-replica cache = ~hundreds of GH calls/hr
// in steady state, well under the 5000/hr token budget. If we ever scale to
// many replicas behind a load balancer, swap for Redis without changing the
// public contract.
var (
	repoMetadataCache   = map[string]repoMetadataCacheEntry{}
	repoMetadataCacheMu sync.Mutex
)

func repoMetadataCacheGet(key string) (RepoMetadata, bool) {
	repoMetadataCacheMu.Lock()
	defer repoMetadataCacheMu.Unlock()
	entry, ok := repoMetadataCache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return RepoMetadata{}, false
	}
	return entry.meta, true
}

func repoMetadataCacheSet(key string, meta RepoMetadata) {
	repoMetadataCacheMu.Lock()
	defer repoMetadataCacheMu.Unlock()
	repoMetadataCache[key] = repoMetadataCacheEntry{
		meta:      meta,
		expiresAt: time.Now().Add(repoMetadataCacheTTL),
	}
}

// fetchRepoMetadata calls GitHub for one repo and converts the response into
// the shape the dashboard needs. Caller is responsible for caching.
func (h *Handler) fetchRepoMetadata(ctx context.Context, accessToken, owner, repo string) (RepoMetadata, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return RepoMetadata{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return RepoMetadata{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		// Treat 404 as "no access" rather than a hard error — the user's
		// token may not have access to a repo that another user onboarded.
		return RepoMetadata{}, errRepoNotAccessible
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return RepoMetadata{}, fmt.Errorf("GitHub API error: %d - %s", resp.StatusCode, string(body))
	}

	var raw repoMetadataResp
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return RepoMetadata{}, fmt.Errorf("failed to decode GitHub response: %w", err)
	}

	pushed, _ := time.Parse(time.RFC3339, raw.PushedAt)
	updated, _ := time.Parse(time.RFC3339, raw.UpdatedAt)
	license := ""
	if raw.License != nil {
		if raw.License.SPDXID != "" && raw.License.SPDXID != "NOASSERTION" {
			license = raw.License.SPDXID
		} else {
			license = raw.License.Key
		}
	}
	visibility := raw.Visibility
	if visibility == "" {
		if raw.Private {
			visibility = "private"
		} else {
			visibility = "public"
		}
	}

	return RepoMetadata{
		FullName:      raw.FullName,
		Private:       raw.Private,
		Visibility:    visibility,
		HTMLURL:       raw.HTMLURL,
		Description:   raw.Description,
		Language:      raw.Language,
		License:       license,
		DefaultBranch: raw.DefaultBranch,
		Stars:         raw.Stargazers,
		Forks:         raw.Forks,
		OpenIssues:    raw.OpenIssues,
		Archived:      raw.Archived,
		Disabled:      raw.Disabled,
		Fork:          raw.Fork,
		IsTemplate:    raw.IsTemplate,
		Topics:        raw.Topics,
		PushedAt:      pushed,
		UpdatedAt:     updated,
	}, nil
}

// errRepoNotAccessible signals a 404/403 from GitHub — the user's token
// can't see this repo. Surfaced to the client as `error: "not_accessible"`
// so the UI can fall back to "private (no access)" without spamming alerts.
var errRepoNotAccessible = fmt.Errorf("repo not accessible")

// RepoMetadataRequest is the dashboard's batch ask — up to 64 repos in one go.
// Pre-existing /v1/integrations/github/repos lists all the user's repos but
// doesn't include the platform-tracked subset; we want exact one-to-one
// alignment with the project cards.
type RepoMetadataRequest struct {
	Repos []string `json:"repos"`
}

// RepoMetadataResponse maps owner/name → metadata. Repos that errored
// (403, 404, network failure) appear in `errors` with the failure reason
// rather than being silently dropped — the UI can render a degraded "?"
// indicator and operators can audit.
type RepoMetadataResponse struct {
	Repos  map[string]RepoMetadata `json:"repos"`
	Errors map[string]string       `json:"errors,omitempty"`
}

// GetRepoMetadataBatch handles POST /v1/integrations/github/repos/metadata.
// Body: {"repos": ["madfam-org/enclii", "madfam-org/karafiel", ...]}.
func (h *Handler) GetRepoMetadataBatch(c *gin.Context) {
	ctx := c.Request.Context()

	var req RepoMetadataRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if len(req.Repos) == 0 {
		c.JSON(http.StatusOK, RepoMetadataResponse{Repos: map[string]RepoMetadata{}})
		return
	}
	if len(req.Repos) > repoMetadataMaxBatch {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("too many repos in batch (max %d)", repoMetadataMaxBatch),
		})
		return
	}

	resp := RepoMetadataResponse{
		Repos:  make(map[string]RepoMetadata, len(req.Repos)),
		Errors: map[string]string{},
	}

	// First pass: serve everything we can from the cache. Only when we have
	// at least one miss do we bother hitting Janua for the GH token.
	uncached := make([]string, 0, len(req.Repos))
	for _, full := range req.Repos {
		key := strings.TrimSuffix(strings.TrimSpace(full), ".git")
		key = strings.TrimPrefix(key, "https://github.com/")
		if key == "" || !strings.Contains(key, "/") {
			resp.Errors[full] = "invalid_repo_format"
			continue
		}
		if meta, ok := repoMetadataCacheGet(key); ok {
			resp.Repos[key] = meta
			continue
		}
		uncached = append(uncached, key)
	}

	if len(uncached) == 0 {
		c.JSON(http.StatusOK, resp)
		return
	}

	// Get GH access token via the same X-IDP-Token → Janua flow used by the
	// rest of /v1/integrations/github/*.
	idpToken := c.GetHeader("X-IDP-Token")
	if idpToken == "" {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
			return
		}
		idpToken = strings.TrimPrefix(authHeader, "Bearer ")
	}
	tokenResp, err := h.getJanuaToken(ctx, "github", idpToken)
	if err != nil {
		h.logger.Error(ctx, "Failed to get GitHub token from Janua",
			logging.Error("error", err))
		// Don't fail the whole request — return what we cached and surface
		// the auth issue per-repo. The UI degrades gracefully.
		for _, key := range uncached {
			resp.Errors[key] = "github_token_unavailable"
		}
		c.JSON(http.StatusOK, resp)
		return
	}

	for _, key := range uncached {
		parts := strings.SplitN(key, "/", 2)
		if len(parts) != 2 {
			resp.Errors[key] = "invalid_repo_format"
			continue
		}
		meta, err := h.fetchRepoMetadata(ctx, tokenResp.AccessToken, parts[0], parts[1])
		if err != nil {
			if err == errRepoNotAccessible {
				resp.Errors[key] = "not_accessible"
			} else {
				h.logger.Warn(ctx, "Failed to fetch repo metadata",
					logging.String("repo", key),
					logging.Error("error", err))
				resp.Errors[key] = "fetch_failed"
			}
			continue
		}
		repoMetadataCacheSet(key, meta)
		resp.Repos[key] = meta
	}

	c.JSON(http.StatusOK, resp)
}
