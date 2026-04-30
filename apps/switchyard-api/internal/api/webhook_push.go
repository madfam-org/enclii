package api

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/manifest"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// GitHubPushEvent represents a GitHub push webhook payload
type GitHubPushEvent struct {
	Ref        string `json:"ref"`
	Before     string `json:"before"`
	After      string `json:"after"`
	Created    bool   `json:"created"`
	Deleted    bool   `json:"deleted"`
	Forced     bool   `json:"forced"`
	Repository struct {
		ID       int64  `json:"id"`
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		CloneURL string `json:"clone_url"`
		SSHURL   string `json:"ssh_url"`
		HTMLURL  string `json:"html_url"`
	} `json:"repository"`
	Pusher struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"pusher"`
	HeadCommit struct {
		ID        string   `json:"id"`
		Message   string   `json:"message"`
		Timestamp string   `json:"timestamp"`
		Added     []string `json:"added"`
		Modified  []string `json:"modified"`
		Removed   []string `json:"removed"`
		Author    struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"author"`
	} `json:"head_commit"`
	Commits []struct {
		ID       string   `json:"id"`
		Added    []string `json:"added"`
		Modified []string `json:"modified"`
		Removed  []string `json:"removed"`
	} `json:"commits"`
}

// handleGitHubPush processes push events and triggers builds for ALL matching services
// Supports monorepos where multiple services share the same git repository
func (h *Handler) handleGitHubPush(c *gin.Context, ctx context.Context, body []byte) {
	var event GitHubPushEvent
	if err := json.Unmarshal(body, &event); err != nil {
		h.logger.Error(ctx, "Failed to parse push event", logging.Error("error", err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid push event payload"})
		return
	}

	// Only trigger builds for pushes to main/master branch
	branch := extractBranchName(event.Ref)
	if branch != "main" && branch != "master" {
		h.logger.Info(ctx, "Ignoring push to non-main branch",
			logging.String("branch", branch),
			logging.String("repo", event.Repository.FullName))
		c.JSON(http.StatusOK, gin.H{
			"message": "Push to non-main branch ignored",
			"branch":  branch,
		})
		return
	}

	// Skip if this is a branch deletion
	if event.Deleted {
		h.logger.Info(ctx, "Ignoring branch deletion event",
			logging.String("branch", branch))
		c.JSON(http.StatusOK, gin.H{"message": "Branch deletion ignored"})
		return
	}

	gitSHA := event.After
	if len(gitSHA) < 7 {
		h.logger.Error(ctx, "Invalid git SHA in push event",
			logging.String("sha", gitSHA))
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid git SHA"})
		return
	}

	// Find ALL services matching this git repo (monorepo support)
	// Try multiple URL formats that GitHub might send
	var services []*types.Service
	var err error

	for _, repoURL := range []string{
		event.Repository.CloneURL,
		event.Repository.HTMLURL,
		event.Repository.SSHURL,
	} {
		services, err = h.repos.Services.ListByGitRepo(repoURL)
		if err == nil && len(services) > 0 {
			break
		}
	}

	if len(services) == 0 {
		h.logger.Info(ctx, "No services found for repository",
			logging.String("repo", event.Repository.FullName),
			logging.String("clone_url", event.Repository.CloneURL))
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "No services registered for this repository",
			"repo":    event.Repository.FullName,
			"message": "Register a service with this git_repo URL to enable auto-deploy",
		})
		return
	}

	// Fetch and parse enclii.yaml from the repo for domain auto-provisioning
	// This is non-blocking — if the file doesn't exist or can't be parsed, we continue
	encliiConfig := manifest.FetchAndParse(ctx, h.logger, h.config.GitHubToken, event.Repository.FullName, gitSHA)

	// Extract all changed files from the push event for monorepo path filtering
	changedFiles := extractChangedFiles(&event)

	h.logger.Info(ctx, "Triggering builds from GitHub webhook (monorepo)",
		logging.Int("service_count", len(services)),
		logging.Int("changed_files", len(changedFiles)),
		logging.String("repo", event.Repository.FullName),
		logging.String("git_sha", gitSHA),
		logging.String("branch", branch),
		logging.String("pusher", event.Pusher.Name),
		logging.String("commit_message", truncateString(event.HeadCommit.Message, 100)))

	// Emit push_received lifecycle event
	pushMsg := fmt.Sprintf("Push to %s by %s: %s", branch, event.Pusher.Name, truncateString(event.HeadCommit.Message, 80))
	var firstProjectID, firstServiceID *uuid.UUID
	if len(services) > 0 {
		firstProjectID = &services[0].ProjectID
		firstServiceID = &services[0].ID
	}
	h.emitLifecycleEvent(&types.DeploymentLifecycleEvent{
		ProjectID:    firstProjectID,
		ServiceID:    firstServiceID,
		RepoFullName: event.Repository.FullName,
		CommitSHA:    gitSHA,
		Branch:       branch,
		Ref:          event.Ref,
		EventType:    types.LifecyclePushReceived,
		Source:       types.SourceGitHubWebhook,
		Message:      &pushMsg,
		Metadata: map[string]interface{}{
			"pusher":        event.Pusher.Name,
			"service_count": len(services),
			"changed_files": len(changedFiles),
		},
	})

	// Trigger builds for matching services (filtered by watch paths if configured)
	type buildResult struct {
		Service   string `json:"service"`
		ReleaseID string `json:"release_id"`
		Status    string `json:"status"`
		Skipped   bool   `json:"skipped,omitempty"`
		Reason    string `json:"reason,omitempty"`
	}
	var results []buildResult
	var skippedCount int

	// Auto-provision domains from enclii.yaml (non-blocking, best-effort)
	// This runs once per push, not per service — domains apply to the first matching service
	if encliiConfig != nil && len(encliiConfig.Spec.Domains) > 0 && len(services) > 0 {
		go h.provisionDomainsFromYAML(context.Background(), services[0], encliiConfig)
	}

	// Sync custom response headers from enclii.yaml to service record
	if encliiConfig != nil && len(encliiConfig.Spec.Headers) > 0 {
		for i := range services {
			services[i].Headers = encliiConfig.Spec.Headers
		}
	}

	for _, service := range services {
		// Check if service should be rebuilt based on changed files and WatchPaths
		if len(service.WatchPaths) > 0 && !shouldRebuildService(service.WatchPaths, changedFiles) {
			h.logger.Info(ctx, "Skipping build for service - no relevant file changes",
				logging.String("service", service.Name),
				logging.String("watch_paths", strings.Join(service.WatchPaths, ", ")))
			results = append(results, buildResult{
				Service: service.Name,
				Status:  "skipped",
				Skipped: true,
				Reason:  "No files changed in watched paths",
			})
			skippedCount++
			continue
		}
		// Create release record for this service
		release := &types.Release{
			ID:                uuid.New(),
			ServiceID:         service.ID,
			Version:           "v" + time.Now().Format("20060102-150405") + "-" + gitSHA[:7],
			ImageURI:          h.config.Registry + "/" + service.Name + ":" + gitSHA[:7],
			GitSHA:            gitSHA,
			GitBranch:         branch,
			CommitMessage:     event.HeadCommit.Message,
			CommitAuthorName:  event.HeadCommit.Author.Name,
			CommitAuthorEmail: event.HeadCommit.Author.Email,
			RepoURL:           event.Repository.HTMLURL,
			Status:            types.ReleaseStatusBuilding,
			CreatedAt:         time.Now(),
			UpdatedAt:         time.Now(),
		}

		if err := h.repos.Releases.Create(release); err != nil {
			h.logger.Error(ctx, "Failed to create release for service",
				logging.String("service", service.Name),
				logging.Error("db_error", err))
			results = append(results, buildResult{
				Service: service.Name,
				Status:  "failed: " + err.Error(),
			})
			continue
		}

		// Trigger async build (routes to Roundhouse or in-process based on config)
		h.triggerBuildAsync(service, release, gitSHA, branch)

		h.logger.Info(ctx, "Build triggered for service",
			logging.String("service_id", service.ID.String()),
			logging.String("service_name", service.Name),
			logging.String("release_id", release.ID.String()))

		// Log webhook event to Activity feed for dashboard visibility (async to not block response)
		go func() {
			_ = h.repos.AuditLogs.Log(context.Background(), &types.AuditLog{
				ActorID:      nil, // System action (webhook)
				ActorEmail:   "github-webhook@system.enclii.dev",
				ActorRole:    types.RoleSystem,
				Action:       "webhook.build_triggered",
				ResourceType: "service",
				ResourceID:   service.ID.String(),
				ResourceName: service.Name,
				ProjectID:    &service.ProjectID,
				Outcome:      "success",
				Context: map[string]interface{}{
					"event_type": "push",
					"commit_sha": gitSHA,
					"branch":     branch,
					"repository": event.Repository.FullName,
					"release_id": release.ID.String(),
					"pusher":     event.Pusher.Name,
					"trigger":    "github_push",
				},
			})
		}()

		results = append(results, buildResult{
			Service:   service.Name,
			ReleaseID: release.ID.String(),
			Status:    "building",
		})
	}

	triggeredCount := len(results) - skippedCount
	c.JSON(http.StatusOK, gin.H{
		"message":         fmt.Sprintf("Builds triggered for %d services (%d skipped)", triggeredCount, skippedCount),
		"repo":            event.Repository.FullName,
		"git_sha":         gitSHA,
		"branch":          branch,
		"builds":          results,
		"service_count":   len(results),
		"triggered_count": triggeredCount,
		"skipped_count":   skippedCount,
		"changed_files":   len(changedFiles),
	})
}
