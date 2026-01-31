package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
)

// DriftService handles drift detection and resolution
type DriftService struct {
	repos  *db.Repositories
	logger *logrus.Logger
}

func NewDriftService(repos *db.Repositories, logger *logrus.Logger) *DriftService {
	return &DriftService{repos: repos, logger: logger}
}

// ResolveDrift triggers a sync/reconcile to fix detected drift
func (s *DriftService) ResolveDrift(ctx context.Context, id uuid.UUID) error {
	event, err := s.repos.DriftEvents.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get drift event: %w", err)
	}
	if event == nil {
		return fmt.Errorf("drift event not found")
	}
	if err := s.repos.DriftEvents.Resolve(ctx, id); err != nil {
		return fmt.Errorf("failed to resolve drift: %w", err)
	}
	s.logger.WithField("drift_id", id).Info("drift event resolved")
	return nil
}
