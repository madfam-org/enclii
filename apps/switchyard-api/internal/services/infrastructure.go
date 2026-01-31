package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// InfrastructureService handles Crossplane-managed infrastructure resources
type InfrastructureService struct {
	repos     *db.Repositories
	k8sClient *k8s.Client
	logger    *logrus.Logger
}

func NewInfrastructureService(repos *db.Repositories, k8sClient *k8s.Client, logger *logrus.Logger) *InfrastructureService {
	return &InfrastructureService{repos: repos, k8sClient: k8sClient, logger: logger}
}

// ImportBrownfield imports existing infrastructure with ObserveOnly policy
func (s *InfrastructureService) ImportBrownfield(ctx context.Context, mr *types.ManagedResource) error {
	mr.ManagementPolicy = types.ManagementPolicyObserveOnly
	if err := s.repos.ManagedResources.Create(ctx, mr); err != nil {
		return fmt.Errorf("failed to import resource: %w", err)
	}
	s.logger.WithField("resource_id", mr.ID).Info("brownfield resource imported with ObserveOnly policy")
	return nil
}

// CreateComposition creates a new Crossplane composition
func (s *InfrastructureService) CreateComposition(ctx context.Context, mr *types.ManagedResource) error {
	if mr.ManagementPolicy == "" {
		mr.ManagementPolicy = types.ManagementPolicyFullControl
	}
	mr.SyncStatus = types.SyncStatusUnknown
	if err := s.repos.ManagedResources.Create(ctx, mr); err != nil {
		return fmt.Errorf("failed to create composition: %w", err)
	}
	s.logger.WithField("resource_id", mr.ID).Info("managed resource created")
	return nil
}

// SwitchPolicy changes the management policy of a resource
func (s *InfrastructureService) SwitchPolicy(ctx context.Context, id uuid.UUID, policy types.ManagementPolicy) error {
	if err := s.repos.ManagedResources.UpdatePolicy(ctx, id, policy); err != nil {
		return fmt.Errorf("failed to switch policy: %w", err)
	}
	s.logger.WithFields(logrus.Fields{"resource_id": id, "policy": policy}).Info("management policy updated")
	return nil
}
