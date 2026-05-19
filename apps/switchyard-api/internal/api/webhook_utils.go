package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// verifyGitHubSignature verifies the HMAC SHA-256 signature from GitHub
func verifyGitHubSignature(payload []byte, signature string, secret string) bool {
	// GitHub sends signature in format: sha256=<hex digest>
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	signatureHex := strings.TrimPrefix(signature, "sha256=")

	// Compute expected signature
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	// Use constant-time comparison to prevent timing attacks
	return hmac.Equal([]byte(expectedSig), []byte(signatureHex))
}

// extractBranchName extracts the branch name from a git ref
// e.g., "refs/heads/main" -> "main"
func extractBranchName(ref string) string {
	if strings.HasPrefix(ref, "refs/heads/") {
		return strings.TrimPrefix(ref, "refs/heads/")
	}
	return ref
}

// truncateString truncates a string to maxLen characters, adding "..." if truncated
func truncateString(s string, maxLen int) string {
	// Replace newlines with spaces for log readability
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// extractChangedFiles extracts all changed file paths from a push event
// Combines added, modified, and removed files from all commits
func extractChangedFiles(event *GitHubPushEvent) []string {
	seen := make(map[string]bool)
	var files []string

	// Add files from head commit
	for _, f := range event.HeadCommit.Added {
		if !seen[f] {
			seen[f] = true
			files = append(files, f)
		}
	}
	for _, f := range event.HeadCommit.Modified {
		if !seen[f] {
			seen[f] = true
			files = append(files, f)
		}
	}
	for _, f := range event.HeadCommit.Removed {
		if !seen[f] {
			seen[f] = true
			files = append(files, f)
		}
	}

	// Add files from all commits (for push with multiple commits)
	for _, commit := range event.Commits {
		for _, f := range commit.Added {
			if !seen[f] {
				seen[f] = true
				files = append(files, f)
			}
		}
		for _, f := range commit.Modified {
			if !seen[f] {
				seen[f] = true
				files = append(files, f)
			}
		}
		for _, f := range commit.Removed {
			if !seen[f] {
				seen[f] = true
				files = append(files, f)
			}
		}
	}

	return files
}

// shouldRebuildService checks if any changed file matches the service's watch paths
// Uses glob patterns and prefix matching for flexible path filtering
func shouldRebuildService(watchPaths []string, changedFiles []string) bool {
	for _, changed := range changedFiles {
		for _, watchPath := range watchPaths {
			if matchWatchPath(changed, watchPath) {
				return true
			}
		}
	}
	return false
}

func shouldTriggerWebhookBuild(service *types.Service, branch string, changedFiles []string) (bool, string) {
	if service == nil {
		return false, "service missing"
	}
	if !service.AutoDeploy {
		return false, "auto-deploy disabled"
	}
	autoDeployBranch := strings.TrimSpace(service.AutoDeployBranch)
	if autoDeployBranch == "" {
		autoDeployBranch = "main"
	}
	if branch != autoDeployBranch {
		return false, fmt.Sprintf("branch %q does not match auto-deploy branch %q", branch, autoDeployBranch)
	}
	if len(service.WatchPaths) > 0 && !shouldRebuildService(service.WatchPaths, changedFiles) {
		return false, "no files changed in watched paths"
	}
	if !hasWebhookBuildConfig(service) {
		return false, "build config incomplete for webhook auto-build"
	}
	return true, ""
}

func hasWebhookBuildConfig(service *types.Service) bool {
	buildType := strings.TrimSpace(string(service.BuildConfig.Type))
	switch buildType {
	case string(types.BuildTypeAuto):
		return true
	case string(types.BuildTypeBuildpack):
		return strings.TrimSpace(service.BuildConfig.Buildpack) != ""
	case string(types.BuildTypeDockerfile):
		return strings.TrimSpace(service.BuildConfig.Dockerfile) != "" ||
			strings.TrimSpace(service.AppPath) != ""
	default:
		return false
	}
}

// matchWatchPath checks if a file path matches a watch path pattern
// Supports:
// - Exact file matches: "package.json"
// - Directory prefixes: "apps/api/" (matches any file in that directory)
// - Glob patterns: "*.go", "apps/*/src/**"
func matchWatchPath(filePath, watchPath string) bool {
	// Handle glob patterns
	if strings.Contains(watchPath, "*") {
		matched, _ := filepath.Match(watchPath, filePath)
		if matched {
			return true
		}
		// Try matching just the filename for patterns like "*.go"
		matched, _ = filepath.Match(watchPath, filepath.Base(filePath))
		if matched {
			return true
		}
		// Try recursive glob matching for ** patterns
		if strings.Contains(watchPath, "**") {
			// Convert ** to single * for basic matching and check prefix
			prefix := strings.Split(watchPath, "**")[0]
			if strings.HasPrefix(filePath, prefix) {
				return true
			}
		}
		return false
	}

	// Handle directory prefix matching (e.g., "apps/api/")
	if strings.HasSuffix(watchPath, "/") {
		return strings.HasPrefix(filePath, watchPath)
	}

	// Exact match or directory prefix without trailing slash
	return filePath == watchPath || strings.HasPrefix(filePath, watchPath+"/")
}

// findServiceByRepo attempts to find a service by trying different repo URL formats
func (h *Handler) findServiceByRepo(ctx context.Context, cloneURL, htmlURL, sshURL string) (*types.Service, error) {
	service, err := h.repos.Services.GetByGitRepo(cloneURL)
	if err == nil {
		return service, nil
	}

	service, err = h.repos.Services.GetByGitRepo(htmlURL)
	if err == nil {
		return service, nil
	}

	service, err = h.repos.Services.GetByGitRepo(sshURL)
	if err == nil {
		return service, nil
	}

	// Try owner/repo format
	parts := strings.Split(htmlURL, "/")
	if len(parts) >= 2 {
		ownerRepo := parts[len(parts)-2] + "/" + parts[len(parts)-1]
		service, err = h.repos.Services.GetByGitRepo(ownerRepo)
		if err == nil {
			return service, nil
		}
	}

	return nil, err
}

// parseGitHubRepo extracts owner and repo from a GitHub URL
func parseGitHubRepo(gitRepo string) (string, string) {
	// Handle various formats:
	// https://github.com/owner/repo.git
	// https://github.com/owner/repo
	// git@github.com:owner/repo.git
	// owner/repo
	gitRepo = strings.TrimSuffix(gitRepo, ".git")
	gitRepo = strings.TrimPrefix(gitRepo, "https://github.com/")
	gitRepo = strings.TrimPrefix(gitRepo, "git@github.com:")

	parts := strings.Split(gitRepo, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2], parts[len(parts)-1]
	}
	return "", ""
}

// itoa converts an int to string
func itoa(i int) string {
	return fmt.Sprintf("%d", i)
}
