package services

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// PlacementService handles propagation policies and GPU-aware scheduling
type PlacementService struct {
	repos  *db.Repositories
	logger *logrus.Logger
}

func NewPlacementService(repos *db.Repositories, logger *logrus.Logger) *PlacementService {
	return &PlacementService{repos: repos, logger: logger}
}

// CreatePolicy creates a propagation policy
func (s *PlacementService) CreatePolicy(ctx context.Context, pp *types.PropagationPolicy) error {
	if err := s.repos.PropagationPolicies.Create(ctx, pp); err != nil {
		return fmt.Errorf("failed to create propagation policy: %w", err)
	}
	s.logger.WithField("policy_id", pp.ID).Info("propagation policy created")
	return nil
}
