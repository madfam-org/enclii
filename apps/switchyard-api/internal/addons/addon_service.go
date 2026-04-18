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

// CreateAddonRequest represents a request to create a database addon.
// Plan is the customer-facing knob; resource presets (storage/cpu/memory) are
// hydrated from the plan catalog server-side. Config remains as an internal
// escape hatch for operator-initiated overrides.
type CreateAddonRequest struct {
	ProjectID     uuid.UUID
	EnvironmentID *uuid.UUID
	Type          types.DatabaseAddonType
	Name          string
	Plan          string
	Config        types.DatabaseAddonConfig
	UserID        *uuid.UUID
	UserSub       string // auth subject (for event actor fields)
	UserEmail     string
}

// CreateAddon creates a new database addon.
//
// Sprint 1 (P3.1) introduces server-side plan enforcement. The CLI passes a
// plan code (e.g. "standard-0"); we look it up in managed_db_plans, reject
// unknown plans with a typed error, and hydrate resource presets from the
// plan row. Every lifecycle transition also writes a row to
// managed_db_addon_events for audit and (Sprint 3) billing.
func (s *AddonService) CreateAddon(ctx context.Context, req *CreateAddonRequest) (*types.DatabaseAddon, error) {
	logger := s.logger.WithFields(logrus.Fields{
		"project_id": req.ProjectID,
		"type":       req.Type,
		"name":       req.Name,
		"plan":       req.Plan,
	})

	logger.Info("Creating database addon")

	// Validate addon type
	provisioner, ok := s.provisioners[req.Type]
	if !ok {
		return nil, fmt.Errorf("unsupported addon type: %s", req.Type)
	}

	// Validate and resolve plan (Sprint 1). Default to standard-0 for back-compat
	// with any caller that hasn't been updated yet.
	planCode := req.Plan
	if planCode == "" {
		planCode = "standard-0"
	}
	plan, err := s.repos.ManagedDBPlans.GetByCode(ctx, planCode)
	if err != nil {
		logger.WithError(err).WithField("plan", planCode).Warn("Unknown plan code")
		return nil, fmt.Errorf("unknown plan %q: %w", planCode, err)
	}
	if !plan.Available {
		return nil, fmt.Errorf("plan %q is not available for new provisions", planCode)
	}
	if string(req.Type) != plan.Engine {
		return nil, fmt.Errorf("plan %q is for engine %q, not %q", planCode, plan.Engine, req.Type)
	}

	// Check if addon with same name already exists in project
	existing, err := s.repos.DatabaseAddons.GetByName(ctx, req.ProjectID, req.Name)
	if err == nil && existing != nil {
		return nil, fmt.Errorf("addon with name '%s' already exists in project", req.Name)
	}

	// Hydrate config from plan (plan wins over caller config for Sprint 1).
	// Operator escape hatch: Config fields still present on the row for future
	// non-customer-facing overrides, but customer-facing knobs are the plan.
	config := hydrateConfigFromPlan(plan, req.Config)

	// Create addon record
	addon := &types.DatabaseAddon{
		ID:             uuid.New(),
		ProjectID:      req.ProjectID,
		EnvironmentID:  req.EnvironmentID,
		Type:           req.Type,
		Name:           req.Name,
		Plan:           plan.Code,
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

	// Emit lifecycle event: create requested.
	// Event write failure is logged but doesn't fail the request — the row is
	// already in the DB and the operator can reconstruct via row timestamps.
	s.emitEvent(ctx, addon, req, db.EventAddonCreateRequested, map[string]interface{}{
		"plan":           plan.Code,
		"engine":         plan.Engine,
		"tier":           plan.Tier,
		"storage_gb":     plan.StorageGB,
		"ha_enabled":     plan.HAEnabled,
		"environment_id": req.EnvironmentID,
	})

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

	// Emit provisioning.started event.
	s.emitEvent(ctx, addon, req, db.EventAddonProvisioningStarted, map[string]interface{}{
		"namespace": namespace,
		"plan":      plan.Code,
	})

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
		// System-emitted failure event (no user actor; this happens off the
		// request goroutine after the HTTP handler has returned).
		s.emitEventSystem(ctx, addon, db.EventAddonFailed, map[string]interface{}{
			"phase": "provision",
			"error": err.Error(),
		})
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

// EventActor captures who initiated a lifecycle-changing action. All fields
// are optional; an empty actor represents a system-initiated action
// (reconciler, goroutine, cron). Populated from the HTTP handler layer.
type EventActor struct {
	UserID    *uuid.UUID
	UserSub   string
	UserEmail string
}

// DeleteAddon deletes a database addon. Retained for back-compat; prefer
// DeleteAddonBy which records the actor in the event ledger.
func (s *AddonService) DeleteAddon(ctx context.Context, addonID uuid.UUID) error {
	return s.DeleteAddonBy(ctx, addonID, EventActor{})
}

// DeleteAddonBy deletes a database addon and records the actor in the
// lifecycle ledger. Call this from HTTP handlers; the plain DeleteAddon
// remains available for system-initiated calls.
func (s *AddonService) DeleteAddonBy(ctx context.Context, addonID uuid.UUID, actor EventActor) error {
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

	// Emit destroy-requested event before any mutation so we capture intent
	// even if the deprovision path fails.
	s.emitEventWithActor(ctx, addon, actor, db.EventAddonDestroyRequested, map[string]interface{}{
		"plan": addon.Plan,
	})

	// Update status to deleting
	if err := s.repos.DatabaseAddons.UpdateStatus(ctx, addonID, types.DatabaseAddonStatusDeleting, "Deletion in progress"); err != nil {
		logger.WithError(err).Error("Failed to update addon status")
	}

	// Deprovision from K8s
	if err := provisioner.Deprovision(ctx, addon); err != nil {
		logger.WithError(err).Error("Failed to deprovision addon")
		_ = s.repos.DatabaseAddons.UpdateStatus(ctx, addonID, types.DatabaseAddonStatusFailed, fmt.Sprintf("Deprovision failed: %s", err))
		s.emitEventWithActor(ctx, addon, actor, db.EventAddonFailed, map[string]interface{}{
			"phase": "deprovision",
			"error": err.Error(),
		})
		return fmt.Errorf("failed to deprovision addon: %w", err)
	}

	// Soft delete from database.
	// Note: this is idempotent at the SQL level (updates deleted_at=now()
	// regardless of prior state) but returns sql.ErrNoRows if the id is
	// unknown. A second delete call on the same id will succeed silently
	// if the row still exists (soft-deleted rows are still returned by the
	// underlying UPDATE … WHERE id = ? clause).
	if err := s.repos.DatabaseAddons.SoftDelete(ctx, addonID); err != nil {
		logger.WithError(err).Error("Failed to soft delete addon")
		return fmt.Errorf("failed to delete addon: %w", err)
	}

	s.emitEventWithActor(ctx, addon, actor, db.EventAddonDestroyed, map[string]interface{}{
		"plan": addon.Plan,
	})

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

	priorStatus := addon.Status

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

	// Emit transition event if we crossed into a terminal state. System
	// actor because RefreshStatus is typically called off a reconciler tick.
	if priorStatus != addon.Status {
		switch addon.Status {
		case types.DatabaseAddonStatusReady:
			s.emitEventSystem(ctx, addon, db.EventAddonReady, map[string]interface{}{
				"host": addon.Host,
				"port": addon.Port,
			})
		case types.DatabaseAddonStatusFailed:
			s.emitEventSystem(ctx, addon, db.EventAddonFailed, map[string]interface{}{
				"phase": "reconcile",
				"error": status.StatusMessage,
				"from":  priorStatus,
			})
		}
	}

	return addon, nil
}

// hydrateConfigFromPlan overlays plan presets onto a caller-supplied config.
// Plan wins; caller overrides are only honored for fields the plan does not
// pin (today: Version). This keeps customer-facing resource knobs server-side
// enforced while leaving room for internal operator overrides of engine
// version (e.g. pinning to 15 for a migration scenario).
func hydrateConfigFromPlan(plan *db.ManagedDBPlan, override types.DatabaseAddonConfig) types.DatabaseAddonConfig {
	cfg := types.DatabaseAddonConfig{
		Version:   override.Version, // Plan doesn't pin engine version yet.
		StorageGB: plan.StorageGB,
		CPU:       plan.CPURequest,
		Memory:    plan.MemoryRequest,
		HAEnabled: plan.HAEnabled,
		Replicas:  plan.ReplicaCount,
	}
	if cfg.Version == "" {
		switch plan.Engine {
		case "postgres":
			cfg.Version = fmt.Sprintf("%d", DefaultPostgresVersion)
		case "mysql":
			cfg.Version = "8.0"
		}
	}
	return cfg
}

// emitEvent writes a lifecycle event using the actor fields from a
// CreateAddonRequest. Used during create-flow.
func (s *AddonService) emitEvent(ctx context.Context, addon *types.DatabaseAddon, req *CreateAddonRequest, eventType db.ManagedDBAddonEventType, details map[string]interface{}) {
	actor := EventActor{
		UserID:    req.UserID,
		UserSub:   req.UserSub,
		UserEmail: req.UserEmail,
	}
	s.emitEventWithActor(ctx, addon, actor, eventType, details)
}

// emitEventSystem writes a lifecycle event with no user actor — for use from
// background reconcilers, goroutines, or cron.
func (s *AddonService) emitEventSystem(ctx context.Context, addon *types.DatabaseAddon, eventType db.ManagedDBAddonEventType, details map[string]interface{}) {
	s.emitEventWithActor(ctx, addon, EventActor{}, eventType, details)
}

// emitEventWithActor is the shared event-write path. Write failures are
// logged but do not propagate — we don't want a ledger outage to block a
// lifecycle transition. This matches budget_alert_events pattern (P2.2).
func (s *AddonService) emitEventWithActor(ctx context.Context, addon *types.DatabaseAddon, actor EventActor, eventType db.ManagedDBAddonEventType, details map[string]interface{}) {
	if s.repos == nil || s.repos.ManagedDBAddonEvents == nil {
		return
	}
	_, err := s.repos.ManagedDBAddonEvents.Insert(ctx, db.InsertEventParams{
		AddonID:        addon.ID,
		ProjectID:      addon.ProjectID,
		EventType:      eventType,
		ActorUserSub:   actor.UserSub,
		ActorUserEmail: actor.UserEmail,
		Details:        details,
	})
	if err != nil {
		s.logger.WithError(err).WithFields(logrus.Fields{
			"addon_id":   addon.ID,
			"event_type": eventType,
		}).Warn("Failed to write addon event")
	}
}

// applyDefaultConfig applies default values to addon configuration
//
// Deprecated: retained for backward compatibility with pre-P3.1 code paths
// that may still construct addons without a plan. New code should use
// hydrateConfigFromPlan which sources presets from the plan catalog.
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
