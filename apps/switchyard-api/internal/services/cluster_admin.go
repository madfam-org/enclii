package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ClusterAdminService handles cluster registration and management
type ClusterAdminService struct {
	repos  *db.Repositories
	logger *logrus.Logger
}

func NewClusterAdminService(repos *db.Repositories, logger *logrus.Logger) *ClusterAdminService {
	return &ClusterAdminService{repos: repos, logger: logger}
}

func (s *ClusterAdminService) Register(ctx context.Context, cluster *types.Cluster) error {
	cluster.Status = types.ClusterStatusPending
	if err := s.repos.Clusters.Create(ctx, cluster); err != nil {
		return fmt.Errorf("failed to register cluster: %w", err)
	}
	s.logger.WithField("cluster_id", cluster.ID).Info("cluster registered")
	return nil
}

func (s *ClusterAdminService) Update(ctx context.Context, cluster *types.Cluster) error {
	if err := s.repos.Clusters.Update(ctx, cluster); err != nil {
		return fmt.Errorf("failed to update cluster: %w", err)
	}
	return nil
}

func (s *ClusterAdminService) Deregister(ctx context.Context, id uuid.UUID) error {
	if err := s.repos.Clusters.Delete(ctx, id); err != nil {
		return fmt.Errorf("failed to deregister cluster: %w", err)
	}
	s.logger.WithField("cluster_id", id).Info("cluster deregistered")
	return nil
}
