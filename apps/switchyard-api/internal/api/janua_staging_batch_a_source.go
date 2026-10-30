package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

type januaBatchASource struct {
	Operation addons.JanuaStagingOperation
	Plan      *db.ManagedDBPlan
	Addon     *types.DatabaseAddon
}

func (h *Handler) readJanuaBatchASource(ctx context.Context, actor, operation uuid.UUID, code, phase string) (*januaBatchASource, error) {
	if actor == uuid.Nil || operation == uuid.Nil || h == nil || h.repos == nil || h.repos.TenantScope == nil || h.repos.Projects == nil || h.repos.Environments == nil || h.repos.Onboardings == nil || h.repos.Services == nil || h.repos.DatabaseAddons == nil || h.repos.ManagedDBPlans == nil {
		return nil, fmt.Errorf("Janua staging source adapter unavailable")
	}
	// Re-resolve the current platform rank on every effect, regardless of any
	// cached middleware rank or tenant-scope rollback switch.
	allowed, err := h.repos.TenantScope.IsActivePlatformAdmin(ctx, actor)
	if err != nil || !allowed {
		return nil, fmt.Errorf("current platform operator authority required")
	}
	project, err := h.repos.Projects.GetBySlug("janua")
	if err != nil || project == nil {
		return nil, fmt.Errorf("Janua project unavailable")
	}
	environment, e := h.repos.Environments.GetByProjectAndName(project.ID, "staging")
	registration, r := h.repos.Onboardings.GetByRepo(ctx, januaStageRepository)
	if e != nil || r != nil || !stagingNamespaceProof(project, environment, registration, januaStageRepository) {
		return nil, fmt.Errorf("persisted Janua staging ownership refused")
	}
	plan, err := h.repos.ManagedDBPlans.GetByCode(ctx, code)
	if err != nil || !januaStageCatalogPlanValid(plan) || plan.MaxConnections <= 0 || plan.HAEnabled && plan.ReplicaCount < 3 {
		return nil, fmt.Errorf("Janua staging catalog allocation refused")
	}
	o := addons.JanuaStagingOperation{OperationID: operation, ActorID: actor, ProjectID: project.ID, EnvironmentID: environment.ID, RegistrationID: registration.ID, ServiceID: uuid.NewSHA1(operation, []byte(addons.JanuaStagingProfile+"/service")), AddonID: uuid.NewSHA1(operation, []byte(addons.JanuaStagingProfile+"/addon"))}
	// Plan observations are compared, never treated as execution authorization.
	encoded, err := json.Marshal(struct {
		Operation addons.JanuaStagingOperation
		Plan      *db.ManagedDBPlan
		Contract  januaStageDatabaseContract
	}{o, plan, fixedJanuaStageDatabaseContract()})
	if err != nil {
		return nil, fmt.Errorf("Janua staging source fingerprint unavailable")
	}
	sum := sha256.Sum256(encoded)
	o.PlanSHA256 = hex.EncodeToString(sum[:])
	result := &januaBatchASource{Operation: o, Plan: plan}
	if phase == "reserve" {
		return result, nil
	}
	service, err := h.repos.Services.GetByID(o.ServiceID)
	if err != nil || !januaStageServiceOwned(project, environment, service) || service.Name != "janua-api-staging" || service.AutoDeploy || service.AutoDeployEnv != "staging" || service.AutoDeployBranch != "main" || len(service.Jobs) != 0 || len(service.Volumes) != 0 {
		return nil, fmt.Errorf("held Janua staging service identity changed")
	}
	addon, err := h.repos.DatabaseAddons.GetByID(ctx, o.AddonID)
	if err != nil || !addons.JanuaStagingAddonOwned(addon, project.ID, environment.ID) || addon.Plan != plan.Code || addon.Config.CPU != plan.CPURequest || addon.Config.Memory != plan.MemoryRequest || addon.Config.StorageGB != plan.StorageGB || addon.Config.Replicas != plan.ReplicaCount || addon.Config.HAEnabled != plan.HAEnabled {
		return nil, fmt.Errorf("Janua staging catalog/addon binding changed")
	}
	result.Addon = addon
	return result, nil
}
