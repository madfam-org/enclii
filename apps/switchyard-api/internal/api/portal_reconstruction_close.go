package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

var retainedFenceJob = regexp.MustCompile(`^read-[a-f0-9]{24}$`)

func validReconstructionCloseRequest(r initialBootstrapRequest) bool {
	if r.Profile != k8s.PortalReconstructionCloseProfile || r.DryRun == nil || (r.Kernel != "01" && r.Kernel != "02" && r.Kernel != "03") || !retainedFenceJob.MatchString(r.FenceReadJob) || r.IdempotencyKey == "" || len(r.IdempotencyKey) > 128 || strings.IndexFunc(r.IdempotencyKey, unicode.IsControl) >= 0 {
		return false
	}
	if *r.DryRun {
		return r.ExpectedPlan == ""
	}
	return bootstrapHexKey.MatchString(r.ExpectedPlan) && strings.TrimSpace(r.Reason) != "" && len(r.Reason) <= 512 && strings.IndexFunc(r.Reason, unicode.IsControl) < 0
}

func (h *Handler) portalReconstructionClose(c *gin.Context, project *types.Project, addon, service uuid.UUID, r initialBootstrapRequest) {
	// Read a completed operation before resolving present-day custody: replay
	// remains the same historical close after credential rotation or a new turn.
	name := k8s.ReconstructionCloseJobName(project.ID, addon, service, r.IdempotencyKey)
	if a, e := h.repos.DatabaseAddons.GetByID(c.Request.Context(), addon); e == nil && a != nil && a.ProjectID == project.ID {
		if kept, e := h.k8sClient.RetainedBootstrapEvidence(c.Request.Context(), a.K8sNamespace, name, project.ID, addon); e == nil && kept.Profile == k8s.PortalReconstructionCloseProfile {
			if kept.ServiceID != service || kept.CloseInput == nil || kept.CloseEvidence == nil || kept.CloseInput.KernelID != "crea-sim-map"+r.Kernel || kept.CloseEvidence.FenceReadJob != r.FenceReadJob || kept.OperationSHA256 != initialPayloadSHA([]byte(r.IdempotencyKey)) || (!*r.DryRun && kept.ReviewedPlanSHA256 != r.ExpectedPlan) {
				c.JSON(http.StatusConflict, gin.H{"error": "retained reconstruction operation differs"})
				return
			}
			c.JSON(http.StatusOK, gin.H{"job": k8s.IsolatedInitialJobReceipt{Name: name, Namespace: a.K8sNamespace, DryRun: *r.DryRun, Succeeded: 1, Native: kept.Native, Classification: "FIXED_STEP_APPLIED"}, "plan_sha256": kept.ReviewedPlanSHA256, "scope": initialBootstrapScope(r.Profile), "retained": true})
			return
		}
	}
	plan, proof, err := h.reconstructionClosePlan(c.Request.Context(), project, addon, service, r, nil)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "server-owned original fence, successor seed and portal step004 evidence required"})
		return
	}
	reviewed, err := plan.ReconstructionPlanSHA256()
	if err != nil || (!*r.DryRun && reviewed != r.ExpectedPlan) {
		c.JSON(http.StatusConflict, gin.H{"error": "reconstruction plan changed; inspect a new dry run"})
		return
	}
	plan.Revalidate = func(ctx context.Context) error {
		current, _, e := h.reconstructionClosePlan(ctx, project, addon, service, r, proof)
		if e != nil {
			return e
		}
		hash, e := current.ReconstructionPlanSHA256()
		if e != nil || hash != reviewed {
			return fmt.Errorf("reconstruction authority changed")
		}
		return nil
	}
	receipt, err := h.k8sClient.RunIsolatedInitialJob(c.Request.Context(), plan, *r.DryRun)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "reconstruction close refused or held; inspect the same operation key"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"job": receipt, "producer": proof, "plan_sha256": reviewed, "reserva": gin.H{"nucleo": r.Kernel, "fase": "RESTABLECIENDO", "intento": true, "freno": true}, "scope": initialBootstrapScope(r.Profile)})
}

func (h *Handler) reconstructionClosePlan(ctx context.Context, project *types.Project, addonID, serviceID uuid.UUID, r initialBootstrapRequest, knownProof *bootstrapProducerProof) (k8s.IsolatedInitialJobPlan, *bootstrapProducerProof, error) {
	bad := func() (k8s.IsolatedInitialJobPlan, *bootstrapProducerProof, error) {
		return k8s.IsolatedInitialJobPlan{}, nil, fmt.Errorf("reconstruction close authority unavailable")
	}
	profile, err := isolatedBootstrapProfileFor(k8s.PortalReconstructionCloseProfile)
	if err != nil {
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
	bindingID := uuid.Nil
	for _, b := range bindings {
		if b.ServiceID == serviceID && b.AddonID == addonID && b.EnvVarName == "CREA_SIM_PORTAL_DATABASE_URL" && b.Status == types.DatabaseAddonBindingStatusActive {
			if bindingID != uuid.Nil || b.ID == uuid.Nil {
				return bad()
			}
			bindingID = b.ID
		}
	}
	if bindingID == uuid.Nil {
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
	if proof.Image != image || proof.PinMatch != "current" {
		return bad()
	}
	custody, err := h.addonService.InitialBootstrapCredentials(ctx, addon)
	if err != nil {
		return bad()
	}
	data, err := h.initialJobData(ctx, profile, deployment, custody, addon.K8sNamespace)
	if err != nil {
		return bad()
	}
	identity, err := h.initialBootstrapIdentity(ctx, profile, project.ID, deployment, custody, addon.K8sNamespace, bindingID, registration.ID, proof)
	if err != nil {
		return bad()
	}
	fence, err := h.k8sClient.RetainedBootstrapEvidence(ctx, addon.K8sNamespace, r.FenceReadJob, project.ID, addonID)
	if err != nil || fence.Profile != k8s.PortalFenceReadProfile || fence.ServiceID != serviceID || fence.FenceOrigin == nil || fence.Native.FenceRead == nil || fence.Native.FenceRead.KernelID != "crea-sim-map"+r.Kernel || fence.Identity.ClusterUID != identity.ClusterUID || fence.Identity.BindingID != identity.BindingID || fence.Identity.ApplicationNamespaceUID != identity.ApplicationNamespaceUID || fence.Identity.DeploymentUID != identity.DeploymentUID {
		return bad()
	}
	origin := fence.FenceOrigin
	current, err := h.reconstructionKernel(ctx, project, origin.KernelID)
	if err != nil {
		return bad()
	}
	oldAddon, err := h.repos.DatabaseAddons.GetByID(ctx, origin.AddonID)
	if err != nil || oldAddon == nil || oldAddon.ID != origin.AddonID || oldAddon.ProjectID != project.ID || oldAddon.Config.DatabaseName != current.Profile.Database || oldAddon.Config.RuntimeRole != current.Profile.RuntimeRole {
		return bad()
	}
	activeOld, err := h.repos.DatabaseAddons.GetNonDeletedBindingsByAddon(ctx, origin.AddonID)
	if err != nil || len(activeOld) != 0 {
		return bad()
	}
	seed, err := h.k8sClient.InitialMAPEvidence(ctx, current.Addon.K8sNamespace, project.ID, current.Addon.ID, current.Service.ID, current.Profile.Name)
	if err != nil {
		return bad()
	}
	if _, err = h.verifyBootstrapProducer(ctx, current.Profile, current.Deployment.Spec.Template.Spec.Containers[0].Image); err != nil {
		return bad()
	}
	step, err := h.k8sClient.AppliedPortalCloseSchema(ctx, addon.K8sNamespace, project.ID, addonID, serviceID, identity.ClusterUID)
	if err != nil {
		return bad()
	}
	input, evidence, err := constructReconstructionClose(project.ID, addonID, identity, image, r, origin, fence, current, seed, step)
	if err != nil {
		return bad()
	}
	inputBytes, err := k8s.ReconstructionCloseBytes(input)
	if err != nil {
		return bad()
	}
	data["request.json"] = inputBytes
	reason := r.Reason
	if strings.TrimSpace(reason) == "" {
		reason = "review retained close-by-rebuild evidence"
	}
	plan := k8s.IsolatedInitialJobPlan{ProjectID: project.ID, AddonID: addonID, ServiceID: serviceID, Namespace: addon.K8sNamespace, Cluster: addon.K8sResourceName, Profile: profile.Name, Image: image, IdempotencyKey: r.IdempotencyKey, Reason: reason, Data: data, Identity: identity, ReconstructionClose: input, ReconstructionEvidence: evidence}
	return plan, proof, nil
}

func reconstructionEvidenceHash(v any) string { b, _ := json.Marshal(v); return initialPayloadSHA(b) }
