package addons

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// AddonService handles database addon business logic
type AddonService struct {
	repos        *db.Repositories
	k8sClient    *k8s.Client
	logger       *logrus.Logger
	provisioners map[types.DatabaseAddonType]AddonProvisioner
}

// NewAddonService creates a new addon service
func NewAddonService(repos *db.Repositories, k8sClient *k8s.Client, logger *logrus.Logger) *AddonService {
	svc := &AddonService{
		repos:        repos,
		k8sClient:    k8sClient,
		logger:       logger,
		provisioners: make(map[types.DatabaseAddonType]AddonProvisioner),
	}

	// Register provisioners
	svc.provisioners[types.DatabaseAddonTypePostgres] = NewPostgresProvisioner(k8sClient, logger)
	svc.provisioners[types.DatabaseAddonTypeRedis] = NewRedisProvisioner(k8sClient, logger)
	svc.provisioners[types.DatabaseAddonTypeMySQL] = NewMySQLProvisioner(k8sClient, logger)

	return svc
}

// CreateAddonRequest represents a request to create a database addon
type CreateAddonRequest struct {
	ProjectID     uuid.UUID
	EnvironmentID *uuid.UUID
	Type          types.DatabaseAddonType
	Name          string
	Config        types.DatabaseAddonConfig
	UserID        *uuid.UUID
	UserEmail     string
}

// CreateAddon creates a new database addon
func (s *AddonService) CreateAddon(ctx context.Context, req *CreateAddonRequest) (*types.DatabaseAddon, error) {
	logger := s.logger.WithFields(logrus.Fields{
		"project_id": req.ProjectID,
		"type":       req.Type,
		"name":       req.Name,
	})

	logger.Info("Creating database addon")

	// Validate addon type
	provisioner, ok := s.provisioners[req.Type]
	if !ok {
		return nil, fmt.Errorf("unsupported addon type: %s", req.Type)
	}

	// Check if addon with same name already exists in project
	existing, err := s.repos.DatabaseAddons.GetByName(ctx, req.ProjectID, req.Name)
	if err == nil && existing != nil {
		return nil, fmt.Errorf("addon with name '%s' already exists in project", req.Name)
	}

	// Apply default config values
	config := applyDefaultConfig(req.Type, req.Config)

	// Create addon record
	addon := &types.DatabaseAddon{
		ID:             uuid.New(),
		ProjectID:      req.ProjectID,
		EnvironmentID:  req.EnvironmentID,
		Type:           req.Type,
		Name:           req.Name,
		Status:         types.DatabaseAddonStatusPending,
		Config:         config,
		CreatedBy:      req.UserID,
		CreatedByEmail: req.UserEmail,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := s.repos.DatabaseAddons.Create(ctx, addon); err != nil {
		logger.WithError(err).Error("Failed to create addon record")
		return nil, fmt.Errorf("failed to create addon: %w", err)
	}

	// Get project to determine namespace
	project, err := s.repos.Projects.GetByID(ctx, req.ProjectID)
	if err != nil {
		logger.WithError(err).Error("Failed to get project")
		return nil, fmt.Errorf("failed to get project: %w", err)
	}

	// Determine namespace - use project's K8s namespace
	namespace := fmt.Sprintf("project-%s", project.ID.String()[:8])

	// Update status to provisioning
	if err := s.repos.DatabaseAddons.UpdateStatus(ctx, addon.ID, types.DatabaseAddonStatusProvisioning, "Provisioning started"); err != nil {
		logger.WithError(err).Error("Failed to update addon status")
	}

	// Provision the addon asynchronously
	go s.provisionAddon(context.Background(), addon, provisioner, namespace)

	return addon, nil
}

// provisionAddon handles the asynchronous provisioning of a database addon
func (s *AddonService) provisionAddon(ctx context.Context, addon *types.DatabaseAddon, provisioner AddonProvisioner, namespace string) {
	logger := s.logger.WithFields(logrus.Fields{
		"addon_id":  addon.ID,
		"type":      addon.Type,
		"namespace": namespace,
	})

	logger.Info("Starting addon provisioning")

	result, err := provisioner.Provision(ctx, &ProvisionRequest{
		Addon:     addon,
		Namespace: namespace,
		ProjectID: addon.ProjectID,
	})

	if err != nil {
		logger.WithError(err).Error("Addon provisioning failed")
		_ = s.repos.DatabaseAddons.UpdateStatus(ctx, addon.ID, types.DatabaseAddonStatusFailed, err.Error())
		return
	}

	// Update addon with K8s resource info
	addon.K8sNamespace = namespace
	addon.K8sResourceName = result.K8sResourceName
	addon.ConnectionSecret = result.ConnectionSecret

	if err := s.repos.DatabaseAddons.Update(ctx, addon); err != nil {
		logger.WithError(err).Error("Failed to update addon with K8s info")
		return
	}

	logger.Info("Addon provisioning initiated successfully")
}

// GetAddon retrieves a database addon by ID
func (s *AddonService) GetAddon(ctx context.Context, addonID uuid.UUID) (*types.DatabaseAddon, error) {
	return s.repos.DatabaseAddons.GetByID(ctx, addonID)
}

// GetAddonWithBindings retrieves a database addon with its service bindings
func (s *AddonService) GetAddonWithBindings(ctx context.Context, addonID uuid.UUID) (*types.DatabaseAddonWithBindings, error) {
	addon, err := s.repos.DatabaseAddons.GetByID(ctx, addonID)
	if err != nil {
		return nil, err
	}

	bindings, err := s.repos.DatabaseAddons.GetBindingsByAddon(ctx, addonID)
	if err != nil {
		return nil, err
	}

	// Convert []*DatabaseAddonBinding to []DatabaseAddonBinding
	bindingValues := make([]types.DatabaseAddonBinding, len(bindings))
	for i, b := range bindings {
		bindingValues[i] = *b
	}

	return &types.DatabaseAddonWithBindings{
		DatabaseAddon: *addon,
		Bindings:      bindingValues,
	}, nil
}

// ListAddons lists all database addons for a project
func (s *AddonService) ListAddons(ctx context.Context, projectID uuid.UUID) ([]*types.DatabaseAddon, error) {
	return s.repos.DatabaseAddons.ListByProject(ctx, projectID)
}

// ListAllAddonsForUser lists all database addons the user has access to
func (s *AddonService) ListAllAddonsForUser(ctx context.Context, userID uuid.UUID) ([]*types.DatabaseAddon, error) {
	// Get all projects the user has access to
	projectAccess, err := s.repos.ProjectAccess.ListByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user projects: %w", err)
	}

	if len(projectAccess) == 0 {
		return []*types.DatabaseAddon{}, nil
	}

	// Extract project IDs
	projectIDs := make([]uuid.UUID, len(projectAccess))
	for i, pa := range projectAccess {
		projectIDs[i] = pa.ProjectID
	}

	// Fetch all addons for these projects in a single query
	return s.repos.DatabaseAddons.ListByProjects(ctx, projectIDs)
}

// DeleteAddon deletes a database addon
func (s *AddonService) DeleteAddon(ctx context.Context, addonID uuid.UUID) error {
	logger := s.logger.WithField("addon_id", addonID)
	logger.Info("Deleting database addon")

	addon, err := s.repos.DatabaseAddons.GetByID(ctx, addonID)
	if err != nil {
		return fmt.Errorf("addon not found: %w", err)
	}

	// Get provisioner
	provisioner, ok := s.provisioners[addon.Type]
	if !ok {
		return fmt.Errorf("unsupported addon type: %s", addon.Type)
	}

	// Update status to deleting
	if err := s.repos.DatabaseAddons.UpdateStatus(ctx, addonID, types.DatabaseAddonStatusDeleting, "Deletion in progress"); err != nil {
		logger.WithError(err).Error("Failed to update addon status")
	}

	// Deprovision from K8s
	if err := provisioner.Deprovision(ctx, addon); err != nil {
		logger.WithError(err).Error("Failed to deprovision addon")
		_ = s.repos.DatabaseAddons.UpdateStatus(ctx, addonID, types.DatabaseAddonStatusFailed, fmt.Sprintf("Deprovision failed: %s", err))
		return fmt.Errorf("failed to deprovision addon: %w", err)
	}

	// Soft delete from database
	if err := s.repos.DatabaseAddons.SoftDelete(ctx, addonID); err != nil {
		logger.WithError(err).Error("Failed to soft delete addon")
		return fmt.Errorf("failed to delete addon: %w", err)
	}

	logger.Info("Addon deleted successfully")
	return nil
}

// GetCredentials retrieves connection credentials for an addon
func (s *AddonService) GetCredentials(ctx context.Context, addonID uuid.UUID) (*types.DatabaseAddonCredentials, error) {
	addon, err := s.repos.DatabaseAddons.GetByID(ctx, addonID)
	if err != nil {
		return nil, fmt.Errorf("addon not found: %w", err)
	}

	if addon.Status != types.DatabaseAddonStatusReady {
		return nil, fmt.Errorf("addon is not ready (status: %s)", addon.Status)
	}

	provisioner, ok := s.provisioners[addon.Type]
	if !ok {
		return nil, fmt.Errorf("unsupported addon type: %s", addon.Type)
	}

	return provisioner.GetCredentials(ctx, addon)
}

// CreateBinding creates a binding between an addon and a service
func (s *AddonService) CreateBinding(ctx context.Context, addonID, serviceID uuid.UUID, envVarName string) (*types.DatabaseAddonBinding, error) {
	logger := s.logger.WithFields(logrus.Fields{
		"addon_id":   addonID,
		"service_id": serviceID,
	})

	logger.Info("Creating addon binding")

	// Validate addon exists and is ready
	addon, err := s.repos.DatabaseAddons.GetByID(ctx, addonID)
	if err != nil {
		return nil, fmt.Errorf("addon not found: %w", err)
	}

	if addon.Status != types.DatabaseAddonStatusReady {
		return nil, fmt.Errorf("addon is not ready for binding (status: %s)", addon.Status)
	}

	// Create binding
	binding := &types.DatabaseAddonBinding{
		ID:         uuid.New(),
		AddonID:    addonID,
		ServiceID:  serviceID,
		EnvVarName: envVarName,
		Status:     types.DatabaseAddonBindingStatusActive,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	if err := s.repos.DatabaseAddons.CreateBinding(ctx, binding); err != nil {
		return nil, fmt.Errorf("failed to create binding: %w", err)
	}

	logger.Info("Addon binding created successfully")
	return binding, nil
}

// DeleteBinding removes a binding between an addon and a service
func (s *AddonService) DeleteBinding(ctx context.Context, addonID, serviceID uuid.UUID) error {
	return s.repos.DatabaseAddons.DeleteBindingByAddonAndService(ctx, addonID, serviceID)
}

// GetBindingsForService retrieves all addon bindings for a service
func (s *AddonService) GetBindingsForService(ctx context.Context, serviceID uuid.UUID) ([]*types.DatabaseAddonBinding, error) {
	return s.repos.DatabaseAddons.GetBindingsByService(ctx, serviceID)
}

// GetEnvVarsForService retrieves environment variables for a service from all bound addons
func (s *AddonService) GetEnvVarsForService(ctx context.Context, serviceID uuid.UUID) (map[string]string, error) {
	bindings, err := s.repos.DatabaseAddons.GetBindingsByService(ctx, serviceID)
	if err != nil {
		return nil, err
	}

	envVars := make(map[string]string)

	for _, binding := range bindings {
		if binding.Status != types.DatabaseAddonBindingStatusActive {
			continue
		}

		addon, err := s.repos.DatabaseAddons.GetByID(ctx, binding.AddonID)
		if err != nil {
			s.logger.WithError(err).WithField("addon_id", binding.AddonID).Warn("Failed to get addon for binding")
			continue
		}

		if addon.Status != types.DatabaseAddonStatusReady {
			continue
		}

		provisioner, ok := s.provisioners[addon.Type]
		if !ok {
			continue
		}

		uri, err := provisioner.GetConnectionURI(ctx, addon)
		if err != nil {
			s.logger.WithError(err).WithField("addon_id", addon.ID).Warn("Failed to get connection URI")
			continue
		}

		envVars[binding.EnvVarName] = uri
	}

	return envVars, nil
}

// RefreshStatus updates the status of a pending/provisioning addon
func (s *AddonService) RefreshStatus(ctx context.Context, addonID uuid.UUID) (*types.DatabaseAddon, error) {
	addon, err := s.repos.DatabaseAddons.GetByID(ctx, addonID)
	if err != nil {
		return nil, err
	}

	// Only refresh if not already in a terminal state
	if addon.Status == types.DatabaseAddonStatusReady ||
		addon.Status == types.DatabaseAddonStatusDeleted ||
		addon.Status == types.DatabaseAddonStatusFailed {
		return addon, nil
	}

	provisioner, ok := s.provisioners[addon.Type]
	if !ok {
		return nil, fmt.Errorf("unsupported addon type: %s", addon.Type)
	}

	status, err := provisioner.GetStatus(ctx, addon)
	if err != nil {
		return nil, fmt.Errorf("failed to get addon status: %w", err)
	}

	// Update addon with new status
	addon.Status = status.Status
	addon.StatusMessage = status.StatusMessage
	addon.Host = status.Host
	addon.Port = status.Port
	addon.DatabaseName = status.DatabaseName
	addon.Username = status.Username
	addon.UpdatedAt = time.Now()

	if status.Ready {
		now := time.Now()
		addon.ProvisionedAt = &now
	}

	if err := s.repos.DatabaseAddons.Update(ctx, addon); err != nil {
		return nil, fmt.Errorf("failed to update addon: %w", err)
	}

	return addon, nil
}

// applyDefaultConfig applies default values to addon configuration
func applyDefaultConfig(addonType types.DatabaseAddonType, config types.DatabaseAddonConfig) types.DatabaseAddonConfig {
	result := config

	switch addonType {
	case types.DatabaseAddonTypePostgres:
		if result.Version == "" {
			result.Version = fmt.Sprintf("%d", DefaultPostgresVersion)
		}
		if result.StorageGB == 0 {
			result.StorageGB = 10
		}
		if result.CPU == "" {
			result.CPU = DefaultCPU
		}
		if result.Memory == "" {
			result.Memory = DefaultMemory
		}
		if result.Replicas == 0 {
			result.Replicas = DefaultInstances
		}
	case types.DatabaseAddonTypeRedis:
		if result.Memory == "" {
			result.Memory = DefaultMemory
		}
		if result.Replicas == 0 {
			result.Replicas = 1
		}
	case types.DatabaseAddonTypeMySQL:
		if result.Version == "" {
			result.Version = "8.0"
		}
		if result.StorageGB == 0 {
			result.StorageGB = 10
		}
	}

	return result
}
