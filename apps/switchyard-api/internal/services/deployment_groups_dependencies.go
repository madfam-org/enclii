package services

import (
	"context"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/errors"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// =============================================================================
// Topological Sort (Dependency Order Calculation)
// =============================================================================

// TopologicalSort returns services in dependency order as layers
// Services in the same layer have no dependencies on each other and can be deployed in parallel
// Layers are returned in order: [[no-deps], [depends-on-layer-0], [depends-on-layer-1], ...]
func (s *DeploymentGroupService) TopologicalSort(ctx context.Context, serviceIDs []uuid.UUID) ([][]uuid.UUID, error) {
	if len(serviceIDs) == 0 {
		return nil, nil
	}

	// Build a set for quick lookup of which services are in scope
	inScope := make(map[uuid.UUID]bool)
	for _, id := range serviceIDs {
		inScope[id] = true
	}

	// Build in-degree map and adjacency list (only for services in scope)
	inDegree := make(map[uuid.UUID]int)
	dependents := make(map[uuid.UUID][]uuid.UUID) // service -> services that depend on it

	for _, id := range serviceIDs {
		inDegree[id] = 0
	}

	// Get dependencies for each service
	for _, serviceID := range serviceIDs {
		deps, err := s.repos.ServiceDependencies.GetByService(ctx, serviceID)
		if err != nil {
			s.logger.Warn("Failed to get dependencies for service", "service_id", serviceID, "error", err)
			continue
		}

		for _, dep := range deps {
			// Only count dependencies that are within our scope
			if inScope[dep.DependsOnServiceID] {
				inDegree[serviceID]++
				dependents[dep.DependsOnServiceID] = append(dependents[dep.DependsOnServiceID], serviceID)
			}
		}
	}

	// Kahn's algorithm for topological sort with layers
	var layers [][]uuid.UUID

	for len(inDegree) > 0 {
		// Find all nodes with in-degree 0
		var currentLayer []uuid.UUID
		for id, degree := range inDegree {
			if degree == 0 {
				currentLayer = append(currentLayer, id)
			}
		}

		if len(currentLayer) == 0 {
			// Cycle detected - no nodes with in-degree 0 but graph not empty
			return nil, errors.ErrValidation.WithDetails(map[string]any{
				"reason":   "Circular dependency detected in service graph",
				"services": getRemainingServiceIDs(inDegree),
			})
		}

		// Remove current layer from graph and update in-degrees
		for _, id := range currentLayer {
			delete(inDegree, id)
			for _, dependent := range dependents[id] {
				if _, exists := inDegree[dependent]; exists {
					inDegree[dependent]--
				}
			}
		}

		layers = append(layers, currentLayer)
	}

	s.logger.WithFields(logrus.Fields{
		"total_services": len(serviceIDs),
		"layers_count":   len(layers),
	}).Debug("Computed deployment order using topological sort")

	return layers, nil
}

// getRemainingServiceIDs extracts service IDs from in-degree map for error reporting
func getRemainingServiceIDs(inDegree map[uuid.UUID]int) []string {
	var ids []string
	for id := range inDegree {
		ids = append(ids, id.String())
	}
	return ids
}

// =============================================================================
// Service Dependency Management
// =============================================================================

// AddServiceDependencyRequest represents a request to add a service dependency
type AddServiceDependencyRequest struct {
	ServiceID          string
	DependsOnServiceID string
	DependencyType     string // "runtime", "build", "data"
	UserEmail          string
	UserRole           string
}

// AddServiceDependency adds a dependency between two services
func (s *DeploymentGroupService) AddServiceDependency(ctx context.Context, req *AddServiceDependencyRequest) (*db.ServiceDependency, error) {
	serviceID, err := uuid.Parse(req.ServiceID)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrInvalidInput)
	}
	dependsOnID, err := uuid.Parse(req.DependsOnServiceID)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrInvalidInput)
	}

	// Validate services exist
	if _, err := s.repos.Services.GetByID(serviceID); err != nil {
		return nil, errors.Wrap(err, errors.ErrServiceNotFound)
	}
	if _, err := s.repos.Services.GetByID(dependsOnID); err != nil {
		return nil, errors.ErrValidation.WithDetails(map[string]any{
			"field":  "depends_on_service_id",
			"reason": "Dependency service not found",
		})
	}

	// Check for cycles
	hasCycle, err := s.repos.ServiceDependencies.HasCycle(ctx, serviceID, dependsOnID)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrDatabaseError)
	}
	if hasCycle {
		return nil, errors.ErrValidation.WithDetails(map[string]any{
			"reason": "Adding this dependency would create a circular dependency",
		})
	}

	// Parse dependency type
	var depType db.DependencyType
	switch req.DependencyType {
	case "build":
		depType = db.DependencyTypeBuild
	case "data":
		depType = db.DependencyTypeData
	case "runtime", "":
		depType = db.DependencyTypeRuntime
	default:
		return nil, errors.ErrValidation.WithDetails(map[string]any{
			"field":  "dependency_type",
			"reason": "Invalid type: must be runtime, build, or data",
		})
	}

	dep := &db.ServiceDependency{
		ServiceID:          serviceID,
		DependsOnServiceID: dependsOnID,
		DependencyType:     depType,
	}

	if err := s.repos.ServiceDependencies.Create(ctx, dep); err != nil {
		return nil, errors.Wrap(err, errors.ErrDatabaseError)
	}

	// Audit log
	_ = s.repos.AuditLogs.Log(ctx, &types.AuditLog{
		ActorEmail:   req.UserEmail,
		ActorRole:    types.Role(req.UserRole),
		Action:       "service_dependency_added",
		ResourceType: "service_dependency",
		ResourceID:   dep.ID.String(),
		Outcome:      "success",
		Context: map[string]interface{}{
			"service_id":            req.ServiceID,
			"depends_on_service_id": req.DependsOnServiceID,
			"dependency_type":       string(depType),
		},
	})

	return dep, nil
}

// RemoveServiceDependency removes a dependency between two services
func (s *DeploymentGroupService) RemoveServiceDependency(ctx context.Context, serviceID, dependsOnID string, userEmail, userRole string) error {
	svcID, err := uuid.Parse(serviceID)
	if err != nil {
		return errors.Wrap(err, errors.ErrInvalidInput)
	}
	depID, err := uuid.Parse(dependsOnID)
	if err != nil {
		return errors.Wrap(err, errors.ErrInvalidInput)
	}

	if err := s.repos.ServiceDependencies.Delete(ctx, svcID, depID); err != nil {
		return errors.Wrap(err, errors.ErrDatabaseError)
	}

	// Audit log
	_ = s.repos.AuditLogs.Log(ctx, &types.AuditLog{
		ActorEmail:   userEmail,
		ActorRole:    types.Role(userRole),
		Action:       "service_dependency_removed",
		ResourceType: "service_dependency",
		Outcome:      "success",
		Context: map[string]interface{}{
			"service_id":            serviceID,
			"depends_on_service_id": dependsOnID,
		},
	})

	return nil
}

// GetServiceDependencies returns all dependencies for a service
func (s *DeploymentGroupService) GetServiceDependencies(ctx context.Context, serviceID string) ([]*db.ServiceDependency, error) {
	svcID, err := uuid.Parse(serviceID)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrInvalidInput)
	}

	deps, err := s.repos.ServiceDependencies.GetByService(ctx, svcID)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrDatabaseError)
	}

	return deps, nil
}

// GetServiceDependents returns all services that depend on a given service
func (s *DeploymentGroupService) GetServiceDependents(ctx context.Context, serviceID string) ([]*db.ServiceDependency, error) {
	svcID, err := uuid.Parse(serviceID)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrInvalidInput)
	}

	deps, err := s.repos.ServiceDependencies.GetDependents(ctx, svcID)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrDatabaseError)
	}

	return deps, nil
}
