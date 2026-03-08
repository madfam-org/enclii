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

// =============================================================================
// Deployment Group Service (Core Types and Group Creation)
// =============================================================================

// DeploymentGroupService handles deployment group business logic
// including multi-service coordinated deployments with topological ordering
type DeploymentGroupService struct {
	repos             *db.Repositories
	deploymentService *DeploymentService
	logger            *logrus.Logger
}

// NewDeploymentGroupService creates a new deployment group service
func NewDeploymentGroupService(
	repos *db.Repositories,
	deploymentService *DeploymentService,
	logger *logrus.Logger,
) *DeploymentGroupService {
	return &DeploymentGroupService{
		repos:             repos,
		deploymentService: deploymentService,
		logger:            logger,
	}
}

// =============================================================================
// Request/Response Types
// =============================================================================

// CreateGroupDeploymentRequest represents a request to create a group deployment
type CreateGroupDeploymentRequest struct {
	ProjectID     string
	EnvironmentID string
	ServiceIDs    []string // Services to deploy (all if empty)
	Strategy      string   // "parallel", "dependency_ordered", "sequential"
	GitSHA        string
	PRURL         string
	TriggeredBy   string
	UserID        string
	UserEmail     string
	UserRole      string
}

// CreateGroupDeploymentResponse represents the response from creating a group deployment
type CreateGroupDeploymentResponse struct {
	Group           *db.DeploymentGroup
	DeploymentOrder [][]uuid.UUID // Layers of services to deploy in order
}

// =============================================================================
// Group Creation
// =============================================================================

// CreateGroupDeployment creates a new coordinated deployment group
func (s *DeploymentGroupService) CreateGroupDeployment(ctx context.Context, req *CreateGroupDeploymentRequest) (*CreateGroupDeploymentResponse, error) {
	// Parse UUIDs
	projectID, err := uuid.Parse(req.ProjectID)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrInvalidInput)
	}
	environmentID, err := uuid.Parse(req.EnvironmentID)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrInvalidInput)
	}

	// Validate project exists
	project, err := s.repos.Projects.GetByID(ctx, projectID)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrProjectNotFound)
	}

	// Get services to deploy
	var serviceIDs []uuid.UUID
	if len(req.ServiceIDs) == 0 {
		// Deploy all project services
		services, err := s.repos.Services.ListByProject(projectID)
		if err != nil {
			return nil, errors.Wrap(err, errors.ErrDatabaseError)
		}
		for _, svc := range services {
			serviceIDs = append(serviceIDs, svc.ID)
		}
	} else {
		for _, id := range req.ServiceIDs {
			svcID, err := uuid.Parse(id)
			if err != nil {
				return nil, errors.Wrap(err, errors.ErrInvalidInput)
			}
			serviceIDs = append(serviceIDs, svcID)
		}
	}

	if len(serviceIDs) == 0 {
		return nil, errors.ErrValidation.WithDetails(map[string]any{
			"reason": "No services to deploy",
		})
	}

	// Parse strategy
	var strategy db.DeploymentGroupStrategy
	switch req.Strategy {
	case "parallel":
		strategy = db.DeploymentGroupStrategyParallel
	case "sequential":
		strategy = db.DeploymentGroupStrategySequential
	case "dependency_ordered", "":
		strategy = db.DeploymentGroupStrategyDependencyOrdered
	default:
		return nil, errors.ErrValidation.WithDetails(map[string]any{
			"field":  "strategy",
			"reason": "Invalid strategy: must be parallel, sequential, or dependency_ordered",
		})
	}

	s.logger.WithFields(logrus.Fields{
		"project_id":     req.ProjectID,
		"environment_id": req.EnvironmentID,
		"services_count": len(serviceIDs),
		"strategy":       strategy,
	}).Info("Creating deployment group")

	// Calculate deployment order using topological sort
	deploymentOrder, err := s.TopologicalSort(ctx, serviceIDs)
	if err != nil {
		return nil, err
	}

	// Create deployment group
	name := fmt.Sprintf("deploy-%s-%s", project.Slug, time.Now().Format("20060102-150405"))
	group := &db.DeploymentGroup{
		ProjectID:     projectID,
		EnvironmentID: environmentID,
		Name:          &name,
		Status:        db.DeploymentGroupStatusPending,
		Strategy:      strategy,
		TriggeredBy:   &req.TriggeredBy,
		GitSHA:        &req.GitSHA,
	}
	if req.PRURL != "" {
		group.PRURL = &req.PRURL
	}

	if err := s.repos.DeploymentGroups.Create(ctx, group); err != nil {
		s.logger.Error("Failed to create deployment group", "error", err)
		return nil, errors.Wrap(err, errors.ErrDatabaseError)
	}

	// Audit log
	s.repos.AuditLogs.Log(ctx, &types.AuditLog{
		ActorID:      nil,
		ActorEmail:   req.UserEmail,
		ActorRole:    types.Role(req.UserRole),
		Action:       "deployment_group_created",
		ResourceType: "deployment_group",
		ResourceID:   group.ID.String(),
		ResourceName: name,
		Outcome:      "success",
		Context: map[string]interface{}{
			"project_id":     req.ProjectID,
			"environment_id": req.EnvironmentID,
			"services_count": len(serviceIDs),
			"strategy":       string(strategy),
			"layers_count":   len(deploymentOrder),
		},
	})

	return &CreateGroupDeploymentResponse{
		Group:           group,
		DeploymentOrder: deploymentOrder,
	}, nil
}

// =============================================================================
// Get and List Operations
// =============================================================================

// GetGroupDeployment retrieves a deployment group by ID
func (s *DeploymentGroupService) GetGroupDeployment(ctx context.Context, groupID string) (*db.DeploymentGroup, error) {
	id, err := uuid.Parse(groupID)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrInvalidInput)
	}

	group, err := s.repos.DeploymentGroups.GetByID(ctx, id)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrDeploymentNotFound)
	}

	return group, nil
}

// ListGroupDeployments lists deployment groups for a project
func (s *DeploymentGroupService) ListGroupDeployments(ctx context.Context, projectSlug string, limit, offset int) ([]*db.DeploymentGroup, error) {
	project, err := s.repos.Projects.GetBySlug(projectSlug)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrProjectNotFound)
	}

	groups, err := s.repos.DeploymentGroups.ListByProject(ctx, project.ID, limit, offset)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrDatabaseError)
	}

	return groups, nil
}
