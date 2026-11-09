package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

type initialBootstrapRequest struct {
	Profile        string `json:"profile"`
	ServiceID      string `json:"service_id"`
	IdempotencyKey string `json:"idempotency_key"`
	DryRun         *bool  `json:"dry_run"`
	Reason         string `json:"reason"`
	Kernel         string `json:"nucleo"`
	FenceReadJob   string `json:"fence_read_job"`
	ExpectedPlan   string `json:"expect_plan_sha256"`
}

// InitialBootstrapAddon accepts identifiers and a closed initial profile only.
// All owner/runtime credentials, commands and namespaces are server-resolved.
func (h *Handler) InitialBootstrapAddon(c *gin.Context) {
	var request initialBootstrapRequest
	body, bodyErr := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 8192))
	if bodyErr != nil || k8s.DecodeInitialBootstrapJSON(body, &request) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "initial bootstrap accepts only its closed request fields"})
		return
	}
	var supplied map[string]json.RawMessage
	if json.Unmarshal(body, &supplied) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "closed initial request required"})
		return
	}
	if request.Profile != k8s.PortalReconstructionCloseProfile {
		for _, key := range []string{"nucleo", "fence_read_job", "expect_plan_sha256"} {
			if _, present := supplied[key]; present {
				c.JSON(http.StatusBadRequest, gin.H{"error": "profile does not accept reconstruction fields"})
				return
			}
		}
	}
	profile, err := isolatedBootstrapProfileFor(request.Profile)
	addonID, addonErr := uuid.Parse(c.Param("id"))
	serviceID, serviceErr := uuid.Parse(request.ServiceID)
	dryRun := request.DryRun == nil || *request.DryRun
	if err != nil || request.Profile == k8s.KarafielSchemaProfile || k8s.IsProvenanceReadProfile(request.Profile) || addonErr != nil || serviceErr != nil || addonID == uuid.Nil || serviceID == uuid.Nil || (!dryRun && (request.IdempotencyKey == "" || strings.TrimSpace(request.Reason) == "")) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid owned IDs/profile and apply reason/idempotency key required"})
		return
	}
	if request.Profile == k8s.PortalReconstructionCloseProfile {
		if !validReconstructionCloseRequest(request) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "closed reconstruction request requires kernel, retained fence read, reviewed plan and operation key"})
			return
		}
	} else if request.Kernel != "" || request.FenceReadJob != "" || request.ExpectedPlan != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "profile does not accept reconstruction fields"})
		return
	}
	if h.repos == nil || h.repos.Projects == nil || h.repos.Services == nil || h.repos.DatabaseAddons == nil || h.repos.Onboardings == nil || h.addonService == nil || h.k8sClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "initial managed bootstrap adapter unavailable"})
		return
	}
	project, err := h.repos.Projects.GetBySlug(c.Param("slug"))
	if err != nil || project == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project unavailable"})
		return
	}
	if !h.enforceUserProjectAccess(c, project.ID) {
		return
	}
	if request.Profile == k8s.PortalReconstructionCloseProfile {
		h.portalReconstructionClose(c, project, addonID, serviceID, request)
		return
	}
	service, serviceErr := h.repos.Services.GetByID(serviceID)
	addon, addonErr := h.repos.DatabaseAddons.GetByID(c.Request.Context(), addonID)
	if serviceErr != nil || addonErr != nil || !profile.owns(project, service, addon) {
		c.JSON(http.StatusConflict, gin.H{"error": "Ready addon and exact held service must belong to the closed training project"})
		return
	}
	registration, err := h.repos.Onboardings.GetByRepo(c.Request.Context(), "madfam-org/crea-sim")
	if err != nil || registration == nil || registration.ID == uuid.Nil || registration.ProjectID != project.ID || registration.OnboardStatus != "completed" {
		c.JSON(http.StatusConflict, gin.H{"error": "completed owned source registration required"})
		return
	}
	bindings, err := h.repos.DatabaseAddons.GetBindingsByService(c.Request.Context(), serviceID)
	bound := 0
	bindingID := uuid.Nil
	runtimeKey := "DATABASE_URL"
	if profile.portalDatabase() {
		runtimeKey = "CREA_SIM_PORTAL_DATABASE_URL"
	}
	for _, binding := range bindings {
		if binding.AddonID == addonID && binding.ServiceID == serviceID && binding.Status == types.DatabaseAddonBindingStatusActive && binding.EnvVarName == runtimeKey && binding.ID != uuid.Nil {
			bound++
			bindingID = binding.ID
		}
	}
	if err != nil || bound != 1 {
		c.JSON(http.StatusConflict, gin.H{"error": "exact active same-project addon binding required"})
		return
	}
	deployment, err := h.initialWorkload(c.Request.Context(), profile, project.ID)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "exact owned workload must remain held at zero replicas (initial) or converged on its pinned image (fixed step)", "detail": err.Error()})
		return
	}
	image := deployment.Spec.Template.Spec.Containers[0].Image
	proof, err := h.verifyBootstrapProducer(c.Request.Context(), profile, image)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "canonical producer signature/OCI/CI/pin proof unavailable", "detail": err.Error()})
		return
	}
	credentials, err := h.addonService.InitialBootstrapCredentials(c.Request.Context(), addon)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "owned Ready CNPG owner/runtime/public-CA custody unavailable", "detail": err.Error()})
		return
	}
	data, err := h.initialJobData(c.Request.Context(), profile, deployment, credentials, addon.K8sNamespace)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "owned workload configuration/runtime binding not accredited", "detail": err.Error()})
		return
	}
	identity, err := h.initialBootstrapIdentity(c.Request.Context(), profile, project.ID, deployment, credentials, addon.K8sNamespace, bindingID, registration.ID, proof)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "initial retained workload identities unavailable", "detail": err.Error()})
		return
	}
	if dryRun {
		if request.IdempotencyKey == "" {
			request.IdempotencyKey = "dry-run"
		}
		if strings.TrimSpace(request.Reason) == "" {
			request.Reason = "read-only initial bootstrap admission preflight"
		}
	}
	plan := k8s.IsolatedInitialJobPlan{ProjectID: project.ID, AddonID: addonID, ServiceID: serviceID, Namespace: addon.K8sNamespace, Cluster: addon.K8sResourceName, Profile: profile.Name, Image: image, IdempotencyKey: request.IdempotencyKey, Reason: request.Reason, Data: data, Identity: identity}
	plan.Revalidate = func(ctx context.Context) error {
		currentService, e := h.repos.Services.GetByID(serviceID)
		if e != nil {
			return e
		}
		current, e := h.repos.DatabaseAddons.GetByID(ctx, addonID)
		if e != nil || !profile.owns(project, currentService, current) {
			return io.ErrUnexpectedEOF
		}
		currentBindings, e := h.repos.DatabaseAddons.GetBindingsByService(ctx, serviceID)
		if e != nil {
			return e
		}
		active := false
		for _, b := range currentBindings {
			if b.AddonID == addonID && b.ServiceID == serviceID && b.ID == bindingID && b.EnvVarName == runtimeKey && b.Status == types.DatabaseAddonBindingStatusActive {
				active = true
			}
		}
		if !active {
			return io.ErrUnexpectedEOF
		}
		currentDeployment, e := h.initialWorkload(ctx, profile, project.ID)
		if e != nil || currentDeployment.UID != deployment.UID || !sameWorkloadRevision(profile, currentDeployment, deployment) || currentDeployment.Spec.Template.Spec.Containers[0].Image != image {
			return io.ErrUnexpectedEOF
		}
		currentCredentials, e := h.addonService.InitialBootstrapCredentials(ctx, current)
		if e != nil || currentCredentials.ClusterUID != credentials.ClusterUID || currentCredentials.OwnerURI != credentials.OwnerURI || currentCredentials.RuntimeURI != credentials.RuntimeURI || currentCredentials.PublicCA != credentials.PublicCA {
			return io.ErrUnexpectedEOF
		}
		currentData, e := h.initialJobData(ctx, profile, currentDeployment, currentCredentials, current.K8sNamespace)
		if e != nil || !reflect.DeepEqual(currentData, data) {
			return io.ErrUnexpectedEOF
		}
		currentIdentity, e := h.initialBootstrapIdentity(ctx, profile, project.ID, currentDeployment, currentCredentials, current.K8sNamespace, bindingID, registration.ID, proof)
		if e != nil || !reflect.DeepEqual(currentIdentity, identity) {
			return io.ErrUnexpectedEOF
		}
		currentRegistration, e := h.repos.Onboardings.GetByRepo(ctx, "madfam-org/crea-sim")
		if e != nil || currentRegistration == nil || currentRegistration.ID != registration.ID || currentRegistration.ProjectID != project.ID || currentRegistration.OnboardStatus != "completed" {
			return io.ErrUnexpectedEOF
		}
		return nil
	}
	// Recheck authority before the Kube adapter resolves fixed registry custody.
	// The adapter refuses absent/foreign pull custody before activation.
	if !dryRun {
		if err = plan.Revalidate(c.Request.Context()); err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": "initial authority changed before registry custody"})
			return
		}
	}
	receipt, err := h.k8sClient.RunIsolatedInitialJob(c.Request.Context(), plan, dryRun)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "initial Job refused or held suspended; inspect the same operation key", "detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"job": receipt, "producer": proof, "scope": initialBootstrapScope(profile.Name)})
}

func initialBootstrapScope(profile string) string {
	switch profile {
	case k8s.PortalReconstructionCloseProfile:
		return "fixed close-by-rebuild only, bound to immutable original kernel/fence and successor initial seed evidence; no reset, DDL or inferred readiness"
	case "portal-sim-turno-vigente":
		return "fixed step 003 (portal turno vigente) only; no reset, delivery or other DDL; the database's own turno_vigente_recibos row remains the authority"
	case "portal-sim-cierre-reconstruccion-paso":
		return "fixed step 004 (portal close-by-rebuild catalogue) only; closes no reservation; no reset, delivery or other DDL; the database's own cierre_reconstruccion_recibos row remains the authority"
	}
	return "fixed initial migration/seed only; no reset, delivery or inferred VACIO accreditation"
}

func (h *Handler) InitialBootstrapAddonStatus(c *gin.Context) {
	if h.repos == nil || h.repos.Projects == nil || h.repos.DatabaseAddons == nil || h.repos.Services == nil || h.k8sClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "initial Job status unavailable"})
		return
	}
	project, err := h.repos.Projects.GetBySlug(c.Param("slug"))
	if err != nil || project == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project unavailable"})
		return
	}
	if !h.enforceUserProjectAccess(c, project.ID) {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid addon ID"})
		return
	}
	addon, err := h.repos.DatabaseAddons.GetByID(c.Request.Context(), id)
	if err != nil || addon == nil || addon.ProjectID != project.ID {
		c.JSON(http.StatusNotFound, gin.H{"error": "owned addon unavailable"})
		return
	}
	receipt, labels, err := h.k8sClient.IsolatedInitialJobStatus(c.Request.Context(), addon.K8sNamespace, c.Param("job_name"), project.ID, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "owned initial Job unavailable"})
		return
	}
	profile, err := isolatedBootstrapProfileFor(labels["profile"])
	serviceID, idErr := uuid.Parse(labels["service_id"])
	if err != nil || idErr != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "owned initial profile unavailable"})
		return
	}
	service, err := h.repos.Services.GetByID(serviceID)
	if err != nil || service == nil || service.ProjectID != project.ID || service.Name != profile.Service || project.Slug != profile.Project {
		c.JSON(http.StatusNotFound, gin.H{"error": "owned initial service unavailable"})
		return
	}
	scope := "INITIAL_ONLY retained history; current readiness, kernel control and reset accreditation remain separate"
	if k8s.IsProvenanceReadProfile(profile.Name) {
		scope = "PROVENANCE_ONLY retained historical snapshot; no fresh lease, intent, fence, writer exclusion or adoption authority"
	} else if profile.Name == k8s.PortalReconstructionCloseProfile {
		scope = "FIXED_STEP retained close-by-rebuild evidence only; gateway independently verifies the empty successor before admission"
	} else if profile.Name == "portal-sim-cierre-reconstruccion-paso" {
		scope = "FIXED_STEP retained history of this operation key; the database's own cierre_reconstruccion_recibos row remains the authority"
	} else if k8s.IsFixedStepProfile(profile.Name) {
		scope = "FIXED_STEP retained history of this operation key; the database's own turno_vigente_recibos row remains the authority"
	}
	c.JSON(http.StatusOK, gin.H{"job": receipt, "scope": scope})
}
