package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"k8s.io/apimachinery/pkg/api/resource"
)

const januaStageRepository = "madfam-org/janua"

type januaStageDatabaseContract struct {
	Profile             string `json:"profile"`
	AddonName           string `json:"addon_name"`
	DatabaseName        string `json:"database_name"`
	RuntimeRole         string `json:"runtime_role"`
	MigratorRole        string `json:"migrator_role"`
	RuntimeAuthority    string `json:"runtime_authority"`
	OwnerCustody        string `json:"owner_custody"`
	PublicCACustody     string `json:"public_ca_custody"`
	RuntimeIntakeTarget string `json:"runtime_intake_target"`
	CAIntakeTarget      string `json:"ca_intake_target"`
	SSLMode             string `json:"ssl_mode"`
	StorageClass        string `json:"storage_class"`
}

type januaStageAddonPreview struct {
	Classification              string                     `json:"classification"`
	PreviewOnly                 bool                       `json:"preview_only"`
	ApplySupported              bool                       `json:"apply_supported"`
	ProjectID                   uuid.UUID                  `json:"project_id"`
	EnvironmentID               uuid.UUID                  `json:"environment_id"`
	RegistrationID              uuid.UUID                  `json:"registration_id"`
	ServiceID                   *uuid.UUID                 `json:"service_id,omitempty"`
	Plan                        *db.ManagedDBPlan          `json:"catalog_plan"`
	Contract                    januaStageDatabaseContract `json:"contract"`
	Namespaces                  []januaStageNamespaceRead  `json:"namespaces"`
	AddonRowsRead               int                        `json:"addon_rows_read"`
	StageCandidates             int                        `json:"stage_addon_candidates"`
	SharedRowsExcluded          int                        `json:"shared_addon_rows_excluded"`
	StorageClassRead            bool                       `json:"retained_storage_class_read"`
	StorageRetainsData          bool                       `json:"storage_retains_data"`
	StorageClassUID             string                     `json:"storage_class_uid,omitempty"`
	StorageClassResourceVersion string                     `json:"storage_class_resource_version,omitempty"`
	Gates                       []string                   `json:"unresolved_gates"`
	Fingerprint                 string                     `json:"observation_fingerprint"`
	ObservedAt                  string                     `json:"observed_at"`
}

func registerJanuaStagingAddonPlanRoutes(platform *gin.RouterGroup, h *Handler) {
	platform.GET("/projects/:slug/environments/staging/addon-plan", h.RequirePlatformOperator(), h.JanuaStagingAddonPlan)
}

// JanuaStagingAddonPlan reads the existing source of ownership, plan catalog
// and infrastructure metadata only. It has no create/apply mode or credentials
// adapter. Its fingerprint identifies an observation, never execution authority.
func (h *Handler) JanuaStagingAddonPlan(c *gin.Context) {
	query := c.Request.URL.Query()
	for key, values := range query {
		if (key != "plan" && key != "service_id") || len(values) != 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "plan preview accepts only plan and an optional persisted service_id"})
			return
		}
	}
	if c.Param("slug") != "janua" || query.Get("plan") == "" || c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Janua staging plan code required; request bodies and apply selectors are not accepted"})
		return
	}
	var serviceID *uuid.UUID
	if raw := query.Get("service_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil || id == uuid.Nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "valid persisted staging service_id required"})
			return
		}
		serviceID = &id
	}
	if h.repos == nil || h.repos.Projects == nil || h.repos.Environments == nil || h.repos.Onboardings == nil || h.repos.Services == nil || h.repos.DatabaseAddons == nil || h.repos.ManagedDBPlans == nil || h.k8sClient == nil || h.k8sClient.Kube() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "read-only staging addon plan adapter unavailable"})
		return
	}
	project, err := h.repos.Projects.GetBySlug("janua")
	if err != nil || project == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project unavailable"})
		return
	}
	if !h.enforceUserProjectAccess(c, project.ID) {
		return
	}
	environment, envErr := h.repos.Environments.GetByProjectAndName(project.ID, "staging")
	registration, repoErr := h.repos.Onboardings.GetByRepo(c.Request.Context(), januaStageRepository)
	if envErr != nil || repoErr != nil || !stagingNamespaceProof(project, environment, registration, januaStageRepository) {
		c.JSON(http.StatusConflict, gin.H{"error": "persisted Janua staging environment and completed repository ownership must match"})
		return
	}
	if serviceID != nil {
		service, serviceErr := h.repos.Services.GetByID(*serviceID)
		if serviceErr != nil || !januaStageServiceOwned(project, environment, service) {
			c.JSON(http.StatusConflict, gin.H{"error": "service must be a persisted same-project Janua API in the exact staging namespace; production is never retargeted"})
			return
		}
	}
	plan, planErr := h.repos.ManagedDBPlans.GetByCode(c.Request.Context(), query.Get("plan"))
	if planErr != nil || !januaStageCatalogPlanValid(plan) {
		c.JSON(http.StatusConflict, gin.H{"error": "available PostgreSQL plan with explicit resource and storage allocation required"})
		return
	}
	rows, err := h.repos.DatabaseAddons.ListByProject(c.Request.Context(), project.ID)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "owned addon inventory could not be read"})
		return
	}
	preview := januaStageAddonPreview{
		Classification: "READ_ONLY_BLOCKED_STAGING_ADDON_PLAN", PreviewOnly: true, ApplySupported: false,
		ProjectID: project.ID, EnvironmentID: environment.ID, RegistrationID: registration.ID, ServiceID: serviceID,
		Plan: plan, AddonRowsRead: len(rows), Contract: fixedJanuaStageDatabaseContract(),
		Gates: []string{"TYPED_STAGE_PROVISIONING_AND_CUSTODY_ADAPTER_NOT_IMPLEMENTED", "DEDICATED_NONOWNER_PUBLIC_CA_AND_OWNER_CUSTODY_NOT_EXERCISED", "CAPACITY_AND_EXACT_NETWORK_BINDING_NOT_EXERCISED", "FIXED_STAGE_MIGRATOR_NOT_IMPLEMENTED"},
	}
	if serviceID == nil {
		preview.Gates = append(preview.Gates, "EXACT_STAGING_API_SERVICE_REGISTRATION_NOT_PROVEN")
	}
	for _, addon := range rows {
		if addon == nil || addon.ProjectID != project.ID {
			c.JSON(http.StatusConflict, gin.H{"error": "addon inventory ownership is inconsistent"})
			return
		}
		if addon.Name == preview.Contract.AddonName || (addon.EnvironmentID != nil && *addon.EnvironmentID == environment.ID) {
			preview.StageCandidates++
		}
		if addon.Plan == "shared-discovered" {
			preview.SharedRowsExcluded++
		}
	}
	if preview.StageCandidates > 0 {
		preview.Gates = append(preview.Gates, "EXISTING_STAGE_ADDON_REQUIRES_SEPARATE_IDENTITY_REVIEW_NO_ADOPTION")
	}
	for _, namespace := range []string{environment.KubeNamespace, "project-" + project.ID.String()[:8]} {
		observation, readErr := h.readJanuaStageNamespace(c.Request.Context(), namespace, project.ID)
		if readErr != nil {
			c.JSON(http.StatusConflict, gin.H{"error": "namespace ownership or baseline policy read refused; no objects were changed"})
			return
		}
		preview.Namespaces = append(preview.Namespaces, observation)
		if observation.State != "OWNED" || !observation.SignatureAdmissionLabel || observation.DNSOnlyBaseline != "EXACT_OWNED" {
			preview.Gates = append(preview.Gates, "OWNED_NAMESPACE_SIGNATURE_AND_DNS_BASELINE_NOT_PROVEN:"+namespace)
		}
		if observation.OtherPoliciesRead > 0 {
			preview.Gates = append(preview.Gates, "OTHER_NETWORK_POLICIES_REQUIRE_SEPARATE_REVIEW:"+namespace)
		}
		if observation.LegacySharedDataAccessLabel || observation.LegacySharedPostgresConsumerLabel {
			preview.Gates = append(preview.Gates, "LEGACY_SHARED_DATA_LABELS_REQUIRE_ISOLATION_REVIEW:"+namespace)
		}
	}
	if err = h.readJanuaStageRetainedStorage(c.Request.Context(), &preview); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "retained storage class could not be read"})
		return
	}
	if !preview.StorageRetainsData {
		preview.Gates = append(preview.Gates, "RETAINED_STORAGE_CLASS_NOT_PROVEN")
	}
	// Keep the synthetic-only runtime guard intact. The proposed stage contract
	// cannot accidentally become executable through the legacy create adapter.
	if addons.ValidatePostgresRuntimeConfig(types.DatabaseAddonTypePostgres, types.DatabaseAddonConfig{DatabaseName: preview.Contract.DatabaseName, RuntimeRole: preview.Contract.RuntimeRole}) != nil {
		preview.Gates = append(preview.Gates, "CURRENT_RUNTIME_ADAPTER_REMAINS_SYNTHETIC_ONLY")
	}
	sort.Strings(preview.Gates)
	encoded, err := json.Marshal(preview)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "plan observation could not be encoded"})
		return
	}
	fingerprint := sha256.Sum256(encoded)
	preview.Fingerprint = "sha256:" + hex.EncodeToString(fingerprint[:])
	preview.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	c.JSON(http.StatusOK, preview)
}

func januaStageServiceOwned(project *types.Project, environment *types.Environment, service *types.Service) bool {
	return project != nil && environment != nil && service != nil && service.ID != uuid.Nil && service.ProjectID == project.ID && service.Type == types.ServiceTypeWeb &&
		service.K8sNamespace != nil && *service.K8sNamespace == environment.KubeNamespace && service.AppPath == "apps/api" &&
		(service.GitRepo == "https://github.com/madfam-org/janua" || service.GitRepo == "https://github.com/madfam-org/janua.git")
}

func januaStageCatalogPlanValid(plan *db.ManagedDBPlan) bool {
	if plan == nil || !plan.Available || plan.Engine != "postgres" || plan.Code == "" || plan.Code == "shared-discovered" || plan.StorageGB <= 0 || plan.ReplicaCount <= 0 || plan.PriceCentsMonth < 0 {
		return false
	}
	cpu, cpuErr := resource.ParseQuantity(plan.CPURequest)
	memory, memoryErr := resource.ParseQuantity(plan.MemoryRequest)
	return cpuErr == nil && memoryErr == nil && cpu.Sign() > 0 && memory.Sign() > 0
}

func fixedJanuaStageDatabaseContract() januaStageDatabaseContract {
	return januaStageDatabaseContract{
		Profile: "janua-api-staging-v1", AddonName: "janua-staging-db", DatabaseName: "janua_staging", RuntimeRole: "janua_staging_runtime", MigratorRole: "enclii_migrator",
		RuntimeAuthority:    "LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION NOINHERIT; no schema ownership",
		OwnerCustody:        "exact Ready dedicated CNPG Cluster UID and owned migrator Secret; server-only fixed Job, never runtime/intake/output",
		PublicCACustody:     "exact Cluster-owned reconciled server CA; valid public certificates only, never ca.key",
		RuntimeIntakeTarget: "janua-staging/database", CAIntakeTarget: "janua-staging/database-ca", SSLMode: "verify-full", StorageClass: addons.RetainStorageClass,
	}
}

// Keep errors generic: neither API responses nor logs carry raw namespace or
// storage API error bodies, credential data or unreviewed external topology.
var errJanuaStagePlanRead = fmt.Errorf("staging plan metadata read refused")
