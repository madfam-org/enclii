package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// postGitHubPRComment posts a comment on the GitHub PR with the preview URL
func (h *Handler) postGitHubPRComment(service *types.Service, preview *types.PreviewEnvironment) {
	ctx := context.Background()

	// Only post comment if GitHub token is configured
	if h.config.GitHubToken == "" {
		h.logger.Debug(ctx, "Skipping PR comment - no GitHub token configured")
		return
	}

	// Parse owner/repo from git_repo URL
	owner, repo := parseGitHubRepo(service.GitRepo)
	if owner == "" || repo == "" {
		h.logger.Warn(ctx, "Could not parse GitHub owner/repo from service git_repo",
			logging.String("git_repo", service.GitRepo))
		return
	}

	h.logger.Info(ctx, "Posting preview URL to GitHub PR",
		logging.String("owner", owner),
		logging.String("repo", repo),
		logging.Int("pr_number", preview.PRNumber),
		logging.String("preview_url", preview.PreviewURL))

	// Build the comment body with useful information
	commentMarker := "<!-- enclii-preview-comment -->"
	commentBody := fmt.Sprintf(`%s
## 🚀 Preview Deployment

| Environment | Status |
|------------|--------|
| **Preview URL** | [%s](%s) |
| **Branch** | %s |
| **Commit** | %s |

---

<details>
<summary>📋 Preview Details</summary>

- **Service**: %s
- **Created**: %s
- **Auto-sleep**: %d minutes after inactivity

</details>

*Deployed with [Enclii](https://enclii.dev)*`,
		commentMarker,
		preview.PreviewURL,
		preview.PreviewURL,
		preview.PRBranch,
		preview.CommitSHA[:7],
		service.Name,
		preview.CreatedAt.Format("2006-01-02 15:04 UTC"),
		preview.AutoSleepAfter,
	)

	// Check if we already have a comment on this PR (update it instead of creating new)
	existingComment, err := h.findExistingPreviewComment(ctx, owner, repo, preview.PRNumber, commentMarker)
	if err != nil {
		h.logger.Warn(ctx, "Failed to check for existing comment",
			logging.Error("error", err))
	}

	if existingComment != nil {
		// Update existing comment
		if err := h.updateGitHubComment(ctx, owner, repo, existingComment.ID, commentBody); err != nil {
			h.logger.Error(ctx, "Failed to update GitHub PR comment",
				logging.Int("pr_number", preview.PRNumber),
				logging.Error("error", err))
			return
		}
		h.logger.Info(ctx, "Updated existing GitHub PR comment",
			logging.Int("pr_number", preview.PRNumber),
			logging.String("comment_id", fmt.Sprintf("%d", existingComment.ID)))
	} else {
		// Create new comment
		commentID, err := h.createGitHubComment(ctx, owner, repo, preview.PRNumber, commentBody)
		if err != nil {
			h.logger.Error(ctx, "Failed to create GitHub PR comment",
				logging.Int("pr_number", preview.PRNumber),
				logging.Error("error", err))
			return
		}
		h.logger.Info(ctx, "Created GitHub PR comment",
			logging.Int("pr_number", preview.PRNumber),
			logging.String("comment_id", fmt.Sprintf("%d", commentID)))
	}
}

// existingGitHubComment represents a GitHub comment for lookup
type existingGitHubComment struct {
	ID int64
}

// findExistingPreviewComment checks if we already posted a preview comment
func (h *Handler) findExistingPreviewComment(ctx context.Context, owner, repo string, prNumber int, marker string) (*existingGitHubComment, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d/comments?per_page=100", owner, repo, prNumber)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+h.config.GitHubToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error: %d - %s", resp.StatusCode, string(body))
	}

	var comments []struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&comments); err != nil {
		return nil, err
	}

	for _, c := range comments {
		if strings.Contains(c.Body, marker) {
			return &existingGitHubComment{ID: c.ID}, nil
		}
	}

	return nil, nil
}

// createGitHubComment creates a new comment on a GitHub PR
func (h *Handler) createGitHubComment(ctx context.Context, owner, repo string, prNumber int, body string) (int64, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d/comments", owner, repo, prNumber)

	payload := struct {
		Body string `json:"body"`
	}{Body: body}

	payloadBytes, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return 0, err
	}

	req.Header.Set("Authorization", "Bearer "+h.config.GitHubToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("GitHub API error: %d - %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	return result.ID, nil
}

// updateGitHubComment updates an existing comment on a GitHub PR
func (h *Handler) updateGitHubComment(ctx context.Context, owner, repo string, commentID int64, body string) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/comments/%d", owner, repo, commentID)

	payload := struct {
		Body string `json:"body"`
	}{Body: body}

	payloadBytes, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "PATCH", url, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+h.config.GitHubToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GitHub API error: %d - %s", resp.StatusCode, string(respBody))
	}

	return nil
}
