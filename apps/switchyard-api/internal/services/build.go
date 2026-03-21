package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/errors"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// BuildService handles build callback processing — updating releases after
// Roundhouse completes a build, and triggering the GitOps digest commit flow.
type BuildService struct {
	repos  *db.Repositories
	logger *logrus.Logger
}

// NewBuildService creates a new build service
func NewBuildService(
	repos *db.Repositories,
	logger *logrus.Logger,
) *BuildService {
	return &BuildService{
		repos:  repos,
		logger: logger,
	}
}

// ProcessBuildCallbackRequest represents a build completion callback from Roundhouse.
type ProcessBuildCallbackRequest struct {
	JobID          uuid.UUID
	ReleaseID      uuid.UUID
	Success        bool
	ImageURI       string
	ImageDigest    string
	ImageSizeMB    float64
	SBOM           string
	SBOMFormat     string
	ImageSignature string
	DurationSecs   float64
	ErrorMessage   string
	LogsURL        string
}

// ProcessBuildCallbackResponse contains the result of processing a build callback.
type ProcessBuildCallbackResponse struct {
	Release *types.Release
	Service *types.Service // The service for post-build actions (auto-deploy, digest commit)
}

// ProcessBuildCallback updates the release with build results and returns the
// service for downstream actions (GitOps digest commit, auto-deploy).
func (s *BuildService) ProcessBuildCallback(ctx context.Context, req *ProcessBuildCallbackRequest) (*ProcessBuildCallbackResponse, error) {
	// Get the release
	release, err := s.repos.Releases.GetByID(req.ReleaseID)
	if err != nil {
		s.logger.WithFields(logrus.Fields{
			"release_id": req.ReleaseID.String(),
		}).Error("Failed to get release for build callback")
		return nil, errors.Wrap(err, errors.ErrReleaseNotFound)
	}

	if req.Success {
		if err := s.handleSuccessfulBuild(ctx, req, release); err != nil {
			return nil, err
		}
	} else {
		if err := s.handleFailedBuild(ctx, req, release); err != nil {
			return nil, err
		}
	}

	// Look up service for post-build actions
	service, err := s.repos.Services.GetByID(release.ServiceID)
	if err != nil {
		s.logger.WithFields(logrus.Fields{
			"service_id": release.ServiceID.String(),
		}).Warn("Failed to get service for post-build actions (non-fatal)")
		// Non-fatal — build succeeded, just can't return service for downstream
	}

	return &ProcessBuildCallbackResponse{
		Release: release,
		Service: service,
	}, nil
}

// handleSuccessfulBuild updates the release with build artifacts and marks it ready.
func (s *BuildService) handleSuccessfulBuild(ctx context.Context, req *ProcessBuildCallbackRequest, release *types.Release) error {
	// Update image URI
	if req.ImageURI != "" {
		if err := s.repos.Releases.UpdateImageURI(req.ReleaseID, req.ImageURI); err != nil {
			s.logger.WithFields(logrus.Fields{
				"release_id": req.ReleaseID.String(),
			}).Error("Failed to update release image URI")
			return errors.Wrap(err, errors.ErrDatabaseError)
		}
	}

	// Store SBOM (non-fatal on failure)
	if req.SBOM != "" {
		if err := s.repos.Releases.UpdateSBOM(ctx, req.ReleaseID, req.SBOM, req.SBOMFormat); err != nil {
			s.logger.WithFields(logrus.Fields{
				"release_id": req.ReleaseID.String(),
			}).Warn("Failed to store SBOM (non-fatal)")
		}
	}

	// Store signature (non-fatal on failure)
	if req.ImageSignature != "" {
		if err := s.repos.Releases.UpdateSignature(ctx, req.ReleaseID, req.ImageSignature); err != nil {
			s.logger.WithFields(logrus.Fields{
				"release_id": req.ReleaseID.String(),
			}).Warn("Failed to store image signature (non-fatal)")
		}
	}

	// Mark release as ready
	if err := s.repos.Releases.UpdateStatus(req.ReleaseID, types.ReleaseStatusReady); err != nil {
		s.logger.WithFields(logrus.Fields{
			"release_id": req.ReleaseID.String(),
		}).Error("Failed to update release status to ready")
		return errors.Wrap(err, errors.ErrDatabaseError)
	}

	s.logger.WithFields(logrus.Fields{
		"release_id":    req.ReleaseID.String(),
		"job_id":        req.JobID.String(),
		"duration_secs": req.DurationSecs,
		"image_uri":     req.ImageURI,
	}).Info("Build completed successfully")

	return nil
}

// handleFailedBuild marks the release as failed with the error message.
func (s *BuildService) handleFailedBuild(_ context.Context, req *ProcessBuildCallbackRequest, _ *types.Release) error {
	var errorMsg *string
	if req.ErrorMessage != "" {
		errorMsg = &req.ErrorMessage
	}
	if err := s.repos.Releases.UpdateStatusWithError(req.ReleaseID, types.ReleaseStatusFailed, errorMsg); err != nil {
		s.logger.WithFields(logrus.Fields{
			"release_id": req.ReleaseID.String(),
		}).Error("Failed to update release status to failed")
		return errors.Wrap(err, errors.ErrDatabaseError)
	}

	s.logger.WithFields(logrus.Fields{
		"release_id": req.ReleaseID.String(),
		"job_id":     req.JobID.String(),
		"error":      req.ErrorMessage,
		"logs_url":   req.LogsURL,
	}).Error("Build failed")

	return nil
}

// CreateReleaseForBuildRequest represents a request to create a release for a manual build.
type CreateReleaseForBuildRequest struct {
	ServiceID uuid.UUID
	GitSHA    string
	GitBranch string
	Registry  string
}

// CreateReleaseForBuild creates a release record for a build trigger (manual or webhook).
func (s *BuildService) CreateReleaseForBuild(ctx context.Context, req *CreateReleaseForBuildRequest) (*types.Release, error) {
	service, err := s.repos.Services.GetByID(req.ServiceID)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrServiceNotFound)
	}

	if len(req.GitSHA) < 7 {
		return nil, errors.ErrValidation.WithDetails(map[string]any{
			"field":  "git_sha",
			"reason": "Git SHA must be at least 7 characters",
		})
	}

	release := &types.Release{
		ID:        uuid.New(),
		ServiceID: req.ServiceID,
		Version:   fmt.Sprintf("v%s-%s", time.Now().Format("20060102-150405"), req.GitSHA[:7]),
		ImageURI:  req.Registry + "/" + service.Name + ":" + req.GitSHA[:7],
		GitSHA:    req.GitSHA,
		GitBranch: req.GitBranch,
		Status:    types.ReleaseStatusBuilding,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := s.repos.Releases.Create(release); err != nil {
		s.logger.WithFields(logrus.Fields{
			"service_id": req.ServiceID.String(),
			"git_sha":    req.GitSHA[:7],
		}).Error("Failed to create release for build")
		return nil, errors.Wrap(err, errors.ErrDatabaseError)
	}

	return release, nil
}

// currentTimestamp returns a formatted timestamp for version strings.
func currentTimestamp() string {
	return time.Now().Format("20060102-150405")
}
