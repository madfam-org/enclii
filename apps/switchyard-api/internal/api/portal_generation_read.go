package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

type portalGenerationReadRequest struct {
	Profile        string                         `json:"profile"`
	ServiceID      string                         `json:"service_id"`
	Input          *k8s.PortalGenerationReadInput `json:"pedido"`
	DryRun         *bool                          `json:"dry_run"`
	IdempotencyKey string                         `json:"idempotency_key"`
	ExpectedPlan   string                         `json:"expect_plan_sha256,omitempty"`
	Reason         string                         `json:"reason,omitempty"`
}

func parsePortalGenerationRead(b []byte) (portalGenerationReadRequest, error) {
	var r portalGenerationReadRequest
	bad := func() (portalGenerationReadRequest, error) {
		return portalGenerationReadRequest{}, fmt.Errorf("closed provenance read request refused")
	}
	if k8s.DecodeInitialBootstrapJSON(b, &r) != nil || r.Profile != k8s.PortalGenerationReadProfile || r.DryRun == nil || k8s.ValidatePortalGenerationReadInput(r.Input) != nil {
		return bad()
	}
	for _, s := range []string{r.ServiceID, r.IdempotencyKey} {
		id, err := uuid.Parse(s)
		if err != nil || id == uuid.Nil || id.String() != s {
			return bad()
		}
	}
	if *r.DryRun {
		if r.ExpectedPlan != "" || r.Reason != "" {
			return bad()
		}
	} else if !bootstrapHexKey.MatchString(r.ExpectedPlan) || strings.TrimSpace(r.Reason) == "" || len(r.Reason) > 512 || strings.IndexFunc(r.Reason, unicode.IsControl) >= 0 {
		return bad()
	}
	return r, nil
}

func registerPortalGenerationReadRoutes(platform *gin.RouterGroup, h *Handler) {
	platform.POST("/projects/:slug/addons/:id/retained-generation-read", h.RequirePlatformOperator(), h.PortalGenerationRead)
	platform.GET("/projects/:slug/addons/:id/retained-generation-read/:job_name", h.RequirePlatformOperator(), h.PortalGenerationReadStatus)
	platform.POST("/projects/:slug/addons/:id/retained-reset-fence-read", h.RequirePlatformOperator(), h.PortalFenceRead)
	platform.GET("/projects/:slug/addons/:id/retained-reset-fence-read/:job_name", h.RequirePlatformOperator(), h.PortalFenceReadStatus)
}

// PortalGenerationRead creates only the closed independent observation Job.
// It cannot choose an image/namespace/command, reset or stop an active exercise.
func (h *Handler) PortalGenerationRead(c *gin.Context) {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 8192))
	r, parseErr := parsePortalGenerationRead(body)
	addonID, idErr := uuid.Parse(c.Param("id"))
	if err != nil || parseErr != nil || idErr != nil || addonID == uuid.Nil || addonID.String() != c.Param("id") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "closed provenance read identifiers required"})
		return
	}
	if h.repos == nil || h.repos.Projects == nil || h.repos.Services == nil || h.repos.DatabaseAddons == nil || h.repos.Onboardings == nil || h.addonService == nil || h.k8sClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "provenance read adapter unavailable"})
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
	serviceID, _ := uuid.Parse(r.ServiceID)
	reason := r.Reason
	if *r.DryRun {
		reason = "provenance-only read admission preflight"
	}
	plan, proof, err := h.portalGenerationReadPlan(c.Request.Context(), project, addonID, serviceID, r.Input, r.IdempotencyKey, reason, nil)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "verified runtime provenance read authority unavailable"})
		return
	}
	sha, err := plan.ProvenancePlanSHA256()
	if err != nil || !*r.DryRun && sha != r.ExpectedPlan {
		c.JSON(http.StatusConflict, gin.H{"error": "provenance read plan changed; inspect a new dry run"})
		return
	}
	plan.Revalidate = func(ctx context.Context) error {
		current, _, err := h.portalGenerationReadPlan(ctx, project, addonID, serviceID, r.Input, r.IdempotencyKey, reason, proof)
		if err != nil {
			return err
		}
		currentSHA, err := current.ProvenancePlanSHA256()
		if err != nil || currentSHA != sha {
			return fmt.Errorf("provenance read authority changed")
		}
		return nil
	}
	receipt, err := h.k8sClient.RunIsolatedInitialJob(c.Request.Context(), plan, *r.DryRun)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "provenance Job refused or held; inspect the same operation key", "hold_stage": k8s.InitialJobHoldStage(err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"job": receipt, "producer": proof, "plan_sha256": sha, "scope": "PROVENANCE_ONLY historical snapshot; no fresh lease, intent, fence, writer exclusion or adoption authority"})
}

func (h *Handler) portalGenerationReadPlan(ctx context.Context, project *types.Project, addonID, serviceID uuid.UUID, input *k8s.PortalGenerationReadInput, key, reason string, knownProof *bootstrapProducerProof) (k8s.IsolatedInitialJobPlan, *bootstrapProducerProof, error) {
	return h.portalRuntimeReadPlan(ctx, project, addonID, serviceID, k8s.PortalGenerationReadProfile, input, key, reason, knownProof)
}

func (h *Handler) portalRuntimeReadPlan(ctx context.Context, project *types.Project, addonID, serviceID uuid.UUID, profileName string, input any, key, reason string, knownProof *bootstrapProducerProof) (k8s.IsolatedInitialJobPlan, *bootstrapProducerProof, error) {
	bad := func() (k8s.IsolatedInitialJobPlan, *bootstrapProducerProof, error) {
		return k8s.IsolatedInitialJobPlan{}, nil, fmt.Errorf("provenance read authority refused")
	}
	profile, err := isolatedBootstrapProfileFor(profileName)
	var generation *k8s.PortalGenerationReadInput
	var fence *k8s.PortalFenceReadInput
	switch v := input.(type) {
	case *k8s.PortalGenerationReadInput:
		if profileName != k8s.PortalGenerationReadProfile || k8s.ValidatePortalGenerationReadInput(v) != nil {
			return bad()
		}
		generation = v
	case *k8s.PortalFenceReadInput:
		if profileName != k8s.PortalFenceReadProfile || k8s.ValidatePortalFenceReadInput(v) != nil {
			return bad()
		}
		fence = v
	default:
		return bad()
	}
	if err != nil || !k8s.IsProvenanceReadProfile(profileName) {
		return bad()
	}
	service, err := h.repos.Services.GetByID(serviceID)
	if err != nil {
		return bad()
	}
	addon, err := h.repos.DatabaseAddons.GetByID(ctx, addonID)
	if err != nil || !profile.owns(project, service, addon) {
		return bad()
	}
	registration, err := h.repos.Onboardings.GetByRepo(ctx, "madfam-org/crea-sim")
	if err != nil || registration == nil || registration.ID == uuid.Nil || registration.ProjectID != project.ID || registration.OnboardStatus != "completed" {
		return bad()
	}
	bindings, err := h.repos.DatabaseAddons.GetBindingsByService(ctx, serviceID)
	if err != nil {
		return bad()
	}
	bound := 0
	bindingID := uuid.Nil
	for _, b := range bindings {
		if b.AddonID == addonID && b.ServiceID == serviceID && b.Status == types.DatabaseAddonBindingStatusActive && b.EnvVarName == "CREA_SIM_PORTAL_DATABASE_URL" && b.ID != uuid.Nil {
			bound++
			bindingID = b.ID
		}
	}
	if bound != 1 {
		return bad()
	}
	deployment, err := h.initialWorkload(ctx, profile, project.ID)
	if err != nil {
		return bad()
	}
	image := deployment.Spec.Template.Spec.Containers[0].Image
	proof := knownProof
	if proof == nil {
		proof, err = h.verifyBootstrapProducer(ctx, profile, image)
		if err != nil {
			return bad()
		}
	}
	transport, err := h.addonService.RuntimeObservationCredentials(ctx, addon)
	if err != nil {
		return bad()
	}
	appURI, err := h.initialWorkloadValue(ctx, profile, deployment, "CREA_SIM_PORTAL_DATABASE_URL")
	if err != nil || !sameInitialDatabase(string(appURI), transport.RuntimeURI) || !strictInitialPortalRuntime(string(appURI)) {
		return bad()
	}
	runtime, err := strictInitialURI(transport.RuntimeURI, false)
	if err != nil {
		return bad()
	}
	identity, err := h.bootstrapTransportIdentity(ctx, profile, project.ID, deployment, transport.ClusterUID, transport.PublicCA, addon.K8sNamespace, bindingID, registration.ID, proof)
	if err != nil {
		return bad()
	}
	data, err := json.Marshal(input)
	if err != nil {
		return bad()
	}
	plan := k8s.IsolatedInitialJobPlan{ProjectID: project.ID, AddonID: addonID, ServiceID: serviceID, Namespace: addon.K8sNamespace, Cluster: addon.K8sResourceName, Profile: profile.Name, Image: image, IdempotencyKey: key, Reason: reason, Identity: identity, GenerationRead: generation, FenceRead: fence,
		Data: map[string][]byte{"ca.crt": []byte(transport.PublicCA), "CREA_SIM_PORTAL_DATABASE_URL": []byte(runtime), "CREA_SIM_DATABASE_NAMESPACE": []byte(addon.K8sNamespace), "request.json": data}}
	if profileName == k8s.PortalFenceReadProfile {
		origin, e := h.captureFenceOrigin(ctx, project, fence.KernelID)
		if e != nil {
			return bad()
		}
		step, e := h.k8sClient.AppliedPortalCloseSchema(ctx, addon.K8sNamespace, project.ID, addonID, serviceID, identity.ClusterUID)
		if e != nil || step.Identity.BindingID != identity.BindingID {
			return bad()
		}
		plan.FenceOrigin = origin
	}
	return plan, proof, nil
}

func (h *Handler) PortalGenerationReadStatus(c *gin.Context) {
	if !strings.HasPrefix(c.Param("job_name"), "read-") {
		c.JSON(http.StatusNotFound, gin.H{"error": "owned provenance read unavailable"})
		return
	}
	h.InitialBootstrapAddonStatus(c)
}
