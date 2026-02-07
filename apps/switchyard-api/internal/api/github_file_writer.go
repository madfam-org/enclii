package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// gitHubFileResponse represents the response from GitHub Contents API
type gitHubFileResponse struct {
	SHA     string `json:"sha"`
	Content string `json:"content"`
}

// gitHubCommitResponse represents a successful commit via the Contents API
type gitHubCommitResponse struct {
	Content struct {
		SHA string `json:"sha"`
	} `json:"content"`
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

// getGitHubFileSHA retrieves the current SHA of a file in a GitHub repo.
// Returns empty string (not error) if the file doesn't exist (404).
func getGitHubFileSHA(ctx context.Context, token, owner, repo, path, ref string) (string, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", owner, repo, path)
	if ref != "" {
		apiURL += "?ref=" + ref
	}

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", nil // File doesn't exist
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("GitHub API error %d: %s", resp.StatusCode, string(body))
	}

	var fileResp gitHubFileResponse
	if err := json.NewDecoder(resp.Body).Decode(&fileResp); err != nil {
		return "", fmt.Errorf("failed to decode GitHub response: %w", err)
	}

	return fileResp.SHA, nil
}

// createOrUpdateGitHubFile commits a file to a GitHub repository via the Contents API.
// If the file already exists, it updates it (requires the current SHA).
// Returns the commit SHA on success.
func createOrUpdateGitHubFile(ctx context.Context, token, owner, repo, path string, content []byte, message, branch string) (string, error) {
	if token == "" {
		return "", fmt.Errorf("GitHub token is required for file commits")
	}

	if branch == "" {
		branch = "main"
	}

	// Check if file already exists to get its SHA (required for updates)
	existingSHA, err := getGitHubFileSHA(ctx, token, owner, repo, path, branch)
	if err != nil {
		return "", fmt.Errorf("failed to check existing file: %w", err)
	}

	// Build request payload
	payload := map[string]string{
		"message": message,
		"content": base64.StdEncoding.EncodeToString(content),
		"branch":  branch,
	}
	if existingSHA != "" {
		payload["sha"] = existingSHA
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", owner, repo, path)

	req, err := http.NewRequestWithContext(ctx, "PUT", apiURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return "", err
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("GitHub API error %d: %s", resp.StatusCode, string(body))
	}

	var commitResp gitHubCommitResponse
	if err := json.NewDecoder(resp.Body).Decode(&commitResp); err != nil {
		return "", fmt.Errorf("failed to decode commit response: %w", err)
	}

	return commitResp.Commit.SHA, nil
}
