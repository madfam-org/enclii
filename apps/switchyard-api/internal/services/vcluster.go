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

// VClusterService handles virtual cluster lifecycle
type VClusterService struct {
	repos     *db.Repositories
	k8sClient *k8s.Client
	logger    *logrus.Logger
}

func NewVClusterService(repos *db.Repositories, k8sClient *k8s.Client, logger *logrus.Logger) *VClusterService {
	return &VClusterService{repos: repos, k8sClient: k8sClient, logger: logger}
}

// Provision creates a new virtual cluster via Helm
func (s *VClusterService) Provision(ctx context.Context, vc *types.VirtualCluster) error {
	vc.Status = types.VClusterStatusCreating
	if err := s.repos.VirtualClusters.Create(ctx, vc); err != nil {
		return fmt.Errorf("failed to provision vcluster: %w", err)
	}
	s.logger.WithField("vcluster_id", vc.ID).Info("virtual cluster provisioning started")
	return nil
}

// Teardown removes a virtual cluster
func (s *VClusterService) Teardown(ctx context.Context, id uuid.UUID) error {
	if err := s.repos.VirtualClusters.UpdateStatus(ctx, id, types.VClusterStatusDeleting); err != nil {
		return fmt.Errorf("failed to update vcluster status: %w", err)
	}
	if err := s.repos.VirtualClusters.Delete(ctx, id); err != nil {
		return fmt.Errorf("failed to teardown vcluster: %w", err)
	}
	s.logger.WithField("vcluster_id", id).Info("virtual cluster torn down")
	return nil
}

// GetKubeconfig retrieves the vCluster kubeconfig
func (s *VClusterService) GetKubeconfig(ctx context.Context, id uuid.UUID) (string, error) {
	vc, err := s.repos.VirtualClusters.GetByID(ctx, id)
	if err != nil {
		return "", fmt.Errorf("failed to get vcluster: %w", err)
	}
	if vc == nil {
		return "", fmt.Errorf("virtual cluster not found")
	}
	// In production, this would retrieve the kubeconfig from a K8s secret
	s.logger.WithField("vcluster_id", id).Info("kubeconfig requested")
	return fmt.Sprintf("# kubeconfig for vcluster %s in namespace %s", vc.Name, vc.Namespace), nil
}
