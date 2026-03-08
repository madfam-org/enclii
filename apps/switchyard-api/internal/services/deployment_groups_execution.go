package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/errors"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// =============================================================================
// Group Execution (Orchestration and Deployment Strategies)
// =============================================================================

// ExecuteGroupDeploymentRequest represents a request to execute a deployment group
type ExecuteGroupDeploymentRequest struct {
	GroupID   string
	UserID    string
	UserEmail string
	UserRole  string
}

// ExecuteGroupDeploymentResponse represents the result of executing a deployment group
type ExecuteGroupDeploymentResponse struct {
	Group       *db.DeploymentGroup
	Deployments []*types.Deployment
	Errors      []error
}

// ExecuteGroupDeployment orchestrates the actual deployment of a group
func (s *DeploymentGroupService) ExecuteGroupDeployment(ctx context.Context, req *ExecuteGroupDeploymentRequest) (*ExecuteGroupDeploymentResponse, error) {
	groupID, err := uuid.Parse(req.GroupID)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrInvalidInput)
	}

	// Get the deployment group
	group, err := s.repos.DeploymentGroups.GetByID(ctx, groupID)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrDeploymentNotFound)
	}

	// Validate group is in pending status
	if group.Status != db.DeploymentGroupStatusPending {
		return nil, errors.ErrValidation.WithDetails(map[string]any{
			"reason": fmt.Sprintf("Deployment group is not pending, current status: %s", group.Status),
		})
	}

	// Mark group as started
	if err := s.repos.DeploymentGroups.UpdateStarted(ctx, groupID); err != nil {
		s.logger.Error("Failed to update group started status", "error", err)
		return nil, errors.Wrap(err, errors.ErrDatabaseError)
	}

	s.logger.WithFields(logrus.Fields{
		"group_id": req.GroupID,
		"strategy": group.Strategy,
	}).Info("Executing deployment group")

	// Get services for this project
	services, err := s.repos.Services.ListByProject(group.ProjectID)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrDatabaseError)
	}

	serviceIDs := make([]uuid.UUID, len(services))
	for i, svc := range services {
		serviceIDs[i] = svc.ID
	}

	// Get deployment order
	layers, err := s.TopologicalSort(ctx, serviceIDs)
	if err != nil {
		errMsg := err.Error()
		if updateErr := s.repos.DeploymentGroups.UpdateCompleted(ctx, groupID, db.DeploymentGroupStatusFailed, &errMsg); updateErr != nil {
			s.logger.Error("Failed to update group failed status", "error", updateErr)
		}
		return nil, err
	}

	var allDeployments []*types.Deployment
	var allErrors []error

	// Execute deployments based on strategy
	switch group.Strategy {
	case db.DeploymentGroupStrategyParallel:
		allDeployments, allErrors = s.executeParallel(ctx, group, layers, req)
	case db.DeploymentGroupStrategySequential:
		allDeployments, allErrors = s.executeSequential(ctx, group, layers, req)
	case db.DeploymentGroupStrategyDependencyOrdered:
		allDeployments, allErrors = s.executeDependencyOrdered(ctx, group, layers, req)
	}

	// Update group status based on results
	var finalStatus db.DeploymentGroupStatus
	var errMsg *string
	if len(allErrors) > 0 {
		finalStatus = db.DeploymentGroupStatusFailed
		errMsgStr := fmt.Sprintf("%d deployments failed", len(allErrors))
		errMsg = &errMsgStr
	} else {
		finalStatus = db.DeploymentGroupStatusSucceeded
	}

	if err := s.repos.DeploymentGroups.UpdateCompleted(ctx, groupID, finalStatus, errMsg); err != nil {
		s.logger.Error("Failed to update group completed status", "error", err)
	}

	// Audit log
	_ = s.repos.AuditLogs.Log(ctx, &types.AuditLog{
		ActorID:      nil,
		ActorEmail:   req.UserEmail,
		ActorRole:    types.Role(req.UserRole),
		Action:       "deployment_group_executed",
		ResourceType: "deployment_group",
		ResourceID:   group.ID.String(),
		ResourceName: *group.Name,
		Outcome:      string(finalStatus),
		Context: map[string]interface{}{
			"deployments_count": len(allDeployments),
			"errors_count":      len(allErrors),
		},
	})

	// Refresh group to get updated timestamps
	refreshedGroup, refreshErr := s.repos.DeploymentGroups.GetByID(ctx, groupID)
	if refreshErr != nil {
		s.logger.Warn("Failed to refresh deployment group after execution, using cached state",
			"group_id", groupID.String(),
			"error", refreshErr)
		// Use the original group if refresh fails
	} else {
		group = refreshedGroup
	}

	return &ExecuteGroupDeploymentResponse{
		Group:       group,
		Deployments: allDeployments,
		Errors:      allErrors,
	}, nil
}

// executeParallel deploys all services simultaneously
func (s *DeploymentGroupService) executeParallel(
	ctx context.Context,
	group *db.DeploymentGroup,
	layers [][]uuid.UUID,
	req *ExecuteGroupDeploymentRequest,
) ([]*types.Deployment, []error) {
	// Flatten all layers into single parallel deployment
	var allServiceIDs []uuid.UUID
	for _, layer := range layers {
		allServiceIDs = append(allServiceIDs, layer...)
	}

	return s.deployServicesInParallel(ctx, group, allServiceIDs, req, 0)
}

// executeSequential deploys services one at a time in order
func (s *DeploymentGroupService) executeSequential(
	ctx context.Context,
	group *db.DeploymentGroup,
	layers [][]uuid.UUID,
	req *ExecuteGroupDeploymentRequest,
) ([]*types.Deployment, []error) {
	var allDeployments []*types.Deployment
	var allErrors []error
	deployOrder := 0

	for _, layer := range layers {
		for _, serviceID := range layer {
			deployment, err := s.deployService(ctx, group, serviceID, req, deployOrder)
			deployOrder++
			if err != nil {
				allErrors = append(allErrors, err)
				// Continue to next service in sequential mode
			} else if deployment != nil {
				allDeployments = append(allDeployments, deployment)
			}
		}
	}

	return allDeployments, allErrors
}

// executeDependencyOrdered deploys services layer by layer, parallel within each layer
func (s *DeploymentGroupService) executeDependencyOrdered(
	ctx context.Context,
	group *db.DeploymentGroup,
	layers [][]uuid.UUID,
	req *ExecuteGroupDeploymentRequest,
) ([]*types.Deployment, []error) {
	var allDeployments []*types.Deployment
	var allErrors []error
	deployOrder := 0

	for layerIdx, layer := range layers {
		s.logger.WithFields(logrus.Fields{
			"group_id":       group.ID,
			"layer":          layerIdx,
			"services_count": len(layer),
		}).Debug("Deploying layer")

		// Deploy all services in this layer in parallel
		deployments, errors := s.deployServicesInParallel(ctx, group, layer, req, deployOrder)
		allDeployments = append(allDeployments, deployments...)
		allErrors = append(allErrors, errors...)
		deployOrder += len(layer)

		// If any errors in this layer, stop (dependencies for next layer may fail)
		if len(errors) > 0 {
			s.logger.WithFields(logrus.Fields{
				"group_id":     group.ID,
				"layer":        layerIdx,
				"errors_count": len(errors),
			}).Warn("Layer deployment failed, stopping group deployment")
			break
		}
	}

	return allDeployments, allErrors
}

// deployServicesInParallel deploys multiple services concurrently
func (s *DeploymentGroupService) deployServicesInParallel(
	ctx context.Context,
	group *db.DeploymentGroup,
	serviceIDs []uuid.UUID,
	req *ExecuteGroupDeploymentRequest,
	startingOrder int,
) ([]*types.Deployment, []error) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var deployments []*types.Deployment
	var errors []error

	for i, serviceID := range serviceIDs {
		wg.Add(1)
		go func(svcID uuid.UUID, order int) {
			defer wg.Done()

			deployment, err := s.deployService(ctx, group, svcID, req, order)

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errors = append(errors, err)
			} else if deployment != nil {
				deployments = append(deployments, deployment)
			}
		}(serviceID, startingOrder+i)
	}

	wg.Wait()
	return deployments, errors
}

// deployService deploys a single service within a group
func (s *DeploymentGroupService) deployService(
	ctx context.Context,
	group *db.DeploymentGroup,
	serviceID uuid.UUID,
	req *ExecuteGroupDeploymentRequest,
	deployOrder int,
) (*types.Deployment, error) {
	// Get latest release for the service
	releases, err := s.repos.Releases.ListByService(serviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get releases for service %s: %w", serviceID, err)
	}

	// Find latest ready release
	var latestRelease *types.Release
	for _, r := range releases {
		if r.Status == types.ReleaseStatusReady {
			latestRelease = r
			break
		}
	}

	if latestRelease == nil {
		return nil, fmt.Errorf("no ready release found for service %s", serviceID)
	}

	// Create deployment
	deployment := &types.Deployment{
		ID:            uuid.New(),
		ReleaseID:     latestRelease.ID,
		EnvironmentID: group.EnvironmentID,
		GroupID:       &group.ID,
		DeployOrder:   deployOrder,
		Replicas:      1, // Default replicas
		Status:        types.DeploymentStatusPending,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if err := s.repos.Deployments.Create(deployment); err != nil {
		return nil, fmt.Errorf("failed to create deployment for service %s: %w", serviceID, err)
	}

	s.logger.WithFields(logrus.Fields{
		"deployment_id": deployment.ID,
		"service_id":    serviceID,
		"release_id":    latestRelease.ID,
		"group_id":      group.ID,
		"deploy_order":  deployOrder,
	}).Debug("Created deployment for service in group")

	return deployment, nil
}

// =============================================================================
// Group Rollback
// =============================================================================

// RollbackGroupRequest represents a request to rollback a deployment group
type RollbackGroupRequest struct {
	GroupID   string
	UserID    string
	UserEmail string
	UserRole  string
}

// RollbackGroupResponse represents the result of rolling back a group
type RollbackGroupResponse struct {
	Group        *db.DeploymentGroup
	RolledBack   int
	FailedToRoll int
	Errors       []error
}

// RollbackGroup rolls back all deployments in a group
func (s *DeploymentGroupService) RollbackGroup(ctx context.Context, req *RollbackGroupRequest) (*RollbackGroupResponse, error) {
	groupID, err := uuid.Parse(req.GroupID)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrInvalidInput)
	}

	// Get the deployment group
	group, err := s.repos.DeploymentGroups.GetByID(ctx, groupID)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrDeploymentNotFound)
	}

	s.logger.WithFields(logrus.Fields{
		"group_id": req.GroupID,
		"status":   group.Status,
	}).Info("Rolling back deployment group")

	// Get all deployments in this group
	deployments, err := s.repos.Deployments.ListByGroup(ctx, groupID)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrDatabaseError)
	}

	var rolledBack int
	var failedToRoll int
	var rollbackErrors []error

	// Rollback each deployment (in reverse order)
	for i := len(deployments) - 1; i >= 0; i-- {
		dep := deployments[i]
		_, err := s.deploymentService.Rollback(ctx, &RollbackRequest{
			DeploymentID: dep.ID.String(),
			UserID:       req.UserID,
			UserEmail:    req.UserEmail,
			UserRole:     req.UserRole,
		})
		if err != nil {
			failedToRoll++
			rollbackErrors = append(rollbackErrors, fmt.Errorf("failed to rollback deployment %s: %w", dep.ID, err))
			s.logger.Warn("Failed to rollback deployment", "deployment_id", dep.ID, "error", err)
		} else {
			rolledBack++
		}
	}

	// Update group status
	var finalStatus db.DeploymentGroupStatus
	var errMsg *string
	if failedToRoll > 0 {
		finalStatus = db.DeploymentGroupStatusFailed
		errMsgStr := fmt.Sprintf("Rollback partially failed: %d succeeded, %d failed", rolledBack, failedToRoll)
		errMsg = &errMsgStr
	} else {
		finalStatus = db.DeploymentGroupStatusRolledBack
	}

	if err := s.repos.DeploymentGroups.UpdateStatus(ctx, groupID, finalStatus, errMsg); err != nil {
		s.logger.Error("Failed to update group rollback status", "error", err)
	}

	// Audit log
	_ = s.repos.AuditLogs.Log(ctx, &types.AuditLog{
		ActorID:      nil,
		ActorEmail:   req.UserEmail,
		ActorRole:    types.Role(req.UserRole),
		Action:       "deployment_group_rollback",
		ResourceType: "deployment_group",
		ResourceID:   group.ID.String(),
		ResourceName: *group.Name,
		Outcome:      string(finalStatus),
		Context: map[string]interface{}{
			"rolled_back":    rolledBack,
			"failed_to_roll": failedToRoll,
		},
	})

	// Refresh group
	refreshedGroup, refreshErr := s.repos.DeploymentGroups.GetByID(ctx, groupID)
	if refreshErr != nil {
		s.logger.Warn("Failed to refresh deployment group after rollback, using cached state",
			"group_id", groupID.String(),
			"error", refreshErr)
		// Use the original group if refresh fails
	} else {
		group = refreshedGroup
	}

	return &RollbackGroupResponse{
		Group:        group,
		RolledBack:   rolledBack,
		FailedToRoll: failedToRoll,
		Errors:       rollbackErrors,
	}, nil
}
