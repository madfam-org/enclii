package services

import (
	"context"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/errors"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// WebhookService handles webhook processing business logic — service discovery,
// release creation, and watch-path filtering for the push → build pipeline.
type WebhookService struct {
	repos  *db.Repositories
	logger *logrus.Logger
}

// NewWebhookService creates a new webhook service
func NewWebhookService(
	repos *db.Repositories,
	logger *logrus.Logger,
) *WebhookService {
	return &WebhookService{
		repos:  repos,
		logger: logger,
	}
}

// FindServicesForRepoRequest represents a request to find services matching a git repo.
type FindServicesForRepoRequest struct {
	CloneURL string
	HTMLURL  string
	SSHURL   string
}

// FindServicesForRepo finds ALL services matching a git repository URL.
// Tries multiple URL formats that GitHub might send (monorepo support).
func (s *WebhookService) FindServicesForRepo(ctx context.Context, req *FindServicesForRepoRequest) ([]*types.Service, error) {
	for _, repoURL := range []string{req.CloneURL, req.HTMLURL, req.SSHURL} {
		if repoURL == "" {
			continue
		}
		services, err := s.repos.Services.ListByGitRepo(repoURL)
		if err == nil && len(services) > 0 {
			s.logger.WithFields(logrus.Fields{
				"repo_url":      repoURL,
				"service_count": len(services),
			}).Info("Found services for repository")
			return services, nil
		}
	}

	return nil, errors.ErrServiceNotFound.WithDetails(map[string]any{
		"clone_url": req.CloneURL,
		"html_url":  req.HTMLURL,
		"reason":    "No services registered for this repository",
	})
}

// ShouldRebuildService checks if a service should be rebuilt based on
// its watch paths and the set of changed files in the push event.
// Returns true if any changed file matches any watch path.
// Returns true if the service has no watch paths (rebuild everything).
func (s *WebhookService) ShouldRebuildService(watchPaths []string, changedFiles []string) bool {
	if len(watchPaths) == 0 {
		return true // No watch paths = rebuild on any change
	}
	for _, changed := range changedFiles {
		for _, watchPath := range watchPaths {
			if matchesWatchPath(changed, watchPath) {
				return true
			}
		}
	}
	return false
}

// matchesWatchPath checks if a file path matches a watch path pattern.
// Supports directory prefixes ("apps/api/") and exact matches ("go.mod").
func matchesWatchPath(filePath, watchPath string) bool {
	if strings.HasSuffix(watchPath, "/") {
		return strings.HasPrefix(filePath, watchPath)
	}
	return filePath == watchPath || strings.HasPrefix(filePath, watchPath+"/")
}

// CreateReleaseRequest represents a request to create a release from a push event.
type CreateReleaseRequest struct {
	Service          *types.Service
	GitSHA           string
	GitBranch        string
	CommitMessage    string
	CommitAuthorName string
	CommitAuthorEmail string
	RepoURL          string
	Registry         string // Container registry prefix
}

// CreateReleaseForPush creates a release record for a service triggered by a push event.
func (s *WebhookService) CreateReleaseForPush(ctx context.Context, req *CreateReleaseRequest) (*types.Release, error) {
	if req.Service == nil {
		return nil, errors.ErrValidation.WithDetails(map[string]any{
			"field":  "service",
			"reason": "Service is required",
		})
	}
	if len(req.GitSHA) < 7 {
		return nil, errors.ErrValidation.WithDetails(map[string]any{
			"field":  "git_sha",
			"reason": "Git SHA must be at least 7 characters",
		})
	}

	release := &types.Release{
		ServiceID:         req.Service.ID,
		Version:           "v" + currentTimestamp() + "-" + req.GitSHA[:7],
		ImageURI:          req.Registry + "/" + req.Service.Name + ":" + req.GitSHA[:7],
		GitSHA:            req.GitSHA,
		GitBranch:         req.GitBranch,
		CommitMessage:     req.CommitMessage,
		CommitAuthorName:  req.CommitAuthorName,
		CommitAuthorEmail: req.CommitAuthorEmail,
		RepoURL:           req.RepoURL,
		Status:            types.ReleaseStatusBuilding,
	}

	if err := s.repos.Releases.Create(release); err != nil {
		s.logger.WithFields(logrus.Fields{
			"service":  req.Service.Name,
			"git_sha":  req.GitSHA,
		}).Error("Failed to create release for push event")
		return nil, errors.Wrap(err, errors.ErrDatabaseError)
	}

	s.logger.WithFields(logrus.Fields{
		"release_id": release.ID.String(),
		"service":    req.Service.Name,
		"git_sha":    req.GitSHA[:7],
	}).Info("Release created for push event")

	return release, nil
}
