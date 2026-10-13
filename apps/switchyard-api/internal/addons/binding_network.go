package addons

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/sirupsen/logrus"
)

var ErrCrossProjectBinding = errors.New("addon and service must belong to the same project")

func validateBindingProject(addon *types.DatabaseAddon, service *types.Service) error {
	if addon == nil || service == nil || addon.ProjectID == uuid.Nil || addon.ProjectID != service.ProjectID {
		return ErrCrossProjectBinding
	}
	return nil
}

// A failed network change retires the just-created binding before it can
// inject credentials. Cleanup is bounded by the binding's own policy labels.
func (s *AddonService) retireFailedNetworkBinding(ctx context.Context, binding *types.DatabaseAddonBinding, addon *types.DatabaseAddon, service *types.Service, actor EventActor) error {
	if err := s.repos.DatabaseAddons.DeleteBinding(ctx, binding.ID); err != nil {
		return fmt.Errorf("failed to deactivate addon binding after network failure")
	}
	s.emitEventWithActor(ctx, addon, actor, db.EventAddonBindingDeleted, map[string]interface{}{
		"service_id": service.ID.String(), "binding_id": binding.ID.String(), "reason": "network_policy_failed",
	})
	if service.K8sNamespace != nil {
		if err := s.k8sClient.RemoveBoundAddonEgressPolicy(ctx, *service.K8sNamespace, addon.ProjectID, binding.ID); err != nil {
			return fmt.Errorf("binding deactivated; owned network cleanup requires reconciliation")
		}
	}
	return nil
}

func (s *AddonService) runtimeBindingStillActive(ctx context.Context, binding *types.DatabaseAddonBinding) (bool, error) {
	bindings, err := s.repos.DatabaseAddons.GetBindingsByService(ctx, binding.ServiceID)
	if err != nil {
		return false, fmt.Errorf("addon binding status cannot be verified")
	}
	for _, current := range bindings {
		if current.ID == binding.ID && current.AddonID == binding.AddonID && current.Status == types.DatabaseAddonBindingStatusActive {
			return true, nil
		}
	}
	return false, nil
}

// CreateBinding creates a binding between an addon and a service. Back-compat
// wrapper; see CreateBindingBy for actor-aware variant.
func (s *AddonService) CreateBinding(ctx context.Context, addonID, serviceID uuid.UUID, envVarName string) (*types.DatabaseAddonBinding, error) {
	return s.CreateBindingBy(ctx, addonID, serviceID, envVarName, EventActor{})
}

// CreateBindingBy creates a binding and records the actor in the event
// ledger.
func (s *AddonService) CreateBindingBy(ctx context.Context, addonID, serviceID uuid.UUID, envVarName string, actor EventActor) (*types.DatabaseAddonBinding, error) {
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
	service, err := s.repos.Services.GetByID(serviceID)
	if err != nil {
		return nil, fmt.Errorf("service not found for addon binding")
	}
	if err := validateBindingProject(addon, service); err != nil {
		return nil, err
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

	s.emitEventWithActor(ctx, addon, actor, db.EventAddonBindingCreated, map[string]interface{}{
		"service_id":   serviceID.String(),
		"env_var_name": envVarName,
		"binding_id":   binding.ID.String(),
	})
	if runtimeProfile(addon.Config) {
		if err := s.k8sClient.EnsureBoundAddonEgressPolicy(ctx, binding.ID, service, addon); err != nil {
			if cleanupErr := s.retireFailedNetworkBinding(ctx, binding, addon, service, actor); cleanupErr != nil {
				return nil, cleanupErr
			}
			return nil, fmt.Errorf("addon binding deactivated: isolated network policy is not ready")
		}
	}

	logger.Info("Addon binding created successfully")
	return binding, nil
}

// DeleteBinding removes a binding between an addon and a service. Back-compat
// wrapper; see DeleteBindingBy for actor-aware variant.
func (s *AddonService) DeleteBinding(ctx context.Context, addonID, serviceID uuid.UUID) error {
	return s.DeleteBindingBy(ctx, addonID, serviceID, EventActor{})
}

// DeleteBindingBy removes a binding and records the actor.
func (s *AddonService) DeleteBindingBy(ctx context.Context, addonID, serviceID uuid.UUID, actor EventActor) error {
	addon, getErr := s.repos.DatabaseAddons.GetByID(ctx, addonID)
	var runtimeBindings []*types.DatabaseAddonBinding
	var service *types.Service
	if getErr == nil && addon != nil && runtimeProfile(addon.Config) {
		var err error
		runtimeBindings, err = s.repos.DatabaseAddons.GetBindingsByService(ctx, serviceID)
		if err != nil {
			return fmt.Errorf("could not load isolated bindings for removal")
		}
		service, err = s.repos.Services.GetByID(serviceID)
		if err != nil {
			return fmt.Errorf("could not load isolated binding service for removal")
		}
	}
	if err := s.repos.DatabaseAddons.DeleteBindingByAddonAndService(ctx, addonID, serviceID); err != nil {
		return err
	}
	// Only emit if we could resolve the addon; otherwise the event-write FK
	// would fail anyway.
	if getErr == nil && addon != nil {
		s.emitEventWithActor(ctx, addon, actor, db.EventAddonBindingDeleted, map[string]interface{}{
			"service_id": serviceID.String(),
		})
	}
	for _, binding := range runtimeBindings {
		if binding.AddonID == addonID && service.K8sNamespace != nil {
			if err := s.k8sClient.RemoveBoundAddonEgressPolicy(ctx, *service.K8sNamespace, addon.ProjectID, binding.ID); err != nil {
				return fmt.Errorf("binding deactivated; owned network cleanup requires reconciliation")
			}
		}
	}
	return nil
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
	if len(bindings) == 0 {
		return envVars, nil
	}
	service, err := s.repos.Services.GetByID(serviceID)
	if err != nil {
		return nil, fmt.Errorf("addon binding service cannot be verified")
	}

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
		if err := validateBindingProject(addon, service); err != nil {
			return nil, err
		}
		if runtimeProfile(addon.Config) {
			if err := s.k8sClient.EnsureBoundAddonEgressPolicy(ctx, binding.ID, service, addon); err != nil {
				if cleanupErr := s.retireFailedNetworkBinding(ctx, binding, addon, service, EventActor{}); cleanupErr != nil {
					return nil, cleanupErr
				}
				return nil, fmt.Errorf("isolated addon network policy cannot be verified")
			}
			active, err := s.runtimeBindingStillActive(ctx, binding)
			if err != nil || !active {
				if service.K8sNamespace != nil {
					_ = s.k8sClient.RemoveBoundAddonEgressPolicy(ctx, *service.K8sNamespace, addon.ProjectID, binding.ID)
				}
				return nil, fmt.Errorf("isolated addon binding is no longer active or cannot be verified")
			}
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
