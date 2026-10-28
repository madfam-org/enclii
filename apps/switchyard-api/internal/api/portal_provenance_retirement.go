package api

import (
	"context"
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

const provenanceRetirementAction = "RETIRE_UNEXECUTED_PROVENANCE_READ"
const provenanceRetirementScope = "non-execution retirement only; no fence, seed, reset, release, adoption or reconstruction-close authority"

type provenanceRetirementRequest struct {
	Action         string `json:"action"`
	Profile        string `json:"profile"`
	ServiceID      string `json:"service_id"`
	IdempotencyKey string `json:"idempotency_key"`
	DryRun         *bool  `json:"dry_run"`
	ExpectedPlan   string `json:"expect_plan_sha256,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

func parseProvenanceRetirement(body []byte) (provenanceRetirementRequest, error) {
	var request provenanceRetirementRequest
	bad := func() (provenanceRetirementRequest, error) {
		return provenanceRetirementRequest{}, fmt.Errorf("closed provenance retirement request required")
	}
	if k8s.DecodeInitialBootstrapJSON(body, &request) != nil || request.Action != provenanceRetirementAction || !k8s.IsProvenanceReadProfile(request.Profile) || request.DryRun == nil {
		return bad()
	}
	for _, value := range []string{request.ServiceID, request.IdempotencyKey} {
		id, err := uuid.Parse(value)
		if err != nil || id == uuid.Nil || id.String() != value {
			return bad()
		}
	}
	if *request.DryRun {
		if request.ExpectedPlan != "" || request.Reason != "" {
			return bad()
		}
	} else if !bootstrapHexKey.MatchString(request.ExpectedPlan) || strings.TrimSpace(request.Reason) == "" || len(request.Reason) > 512 || strings.IndexFunc(request.Reason, unicode.IsControl) >= 0 {
		return bad()
	}
	return request, nil
}

func (h *Handler) provenanceRetirementPlan(ctx context.Context, project *types.Project, addonID, serviceID uuid.UUID, profileName, key string) (k8s.IsolatedInitialJobPlan, error) {
	bad := func() (k8s.IsolatedInitialJobPlan, error) {
		return k8s.IsolatedInitialJobPlan{}, fmt.Errorf("owned provenance retirement scope unavailable")
	}
	profile, err := isolatedBootstrapProfileFor(profileName)
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
	return k8s.IsolatedInitialJobPlan{ProjectID: project.ID, AddonID: addonID, ServiceID: serviceID, Namespace: addon.K8sNamespace, Profile: profileName, IdempotencyKey: key}, nil
}

// Retirement does not resolve producer/database custody: it can only stop an
// exact never-executed owned native reader, under platform+project authority.
func (h *Handler) RetirePortalProvenanceRead(c *gin.Context) {
	body, bodyErr := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	request, requestErr := parseProvenanceRetirement(body)
	addonID, idErr := uuid.Parse(c.Param("id"))
	if bodyErr != nil || requestErr != nil || idErr != nil || addonID == uuid.Nil || addonID.String() != c.Param("id") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "closed provenance retirement identifiers required"})
		return
	}
	if h.repos == nil || h.repos.Projects == nil || h.repos.DatabaseAddons == nil || h.repos.Services == nil || h.k8sClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "provenance retirement adapter unavailable"})
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
	serviceID, _ := uuid.Parse(request.ServiceID)
	plan, err := h.provenanceRetirementPlan(c.Request.Context(), project, addonID, serviceID, request.Profile, request.IdempotencyKey)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "owned provenance retirement scope unavailable"})
		return
	}
	plan.Revalidate = func(ctx context.Context) error {
		current, err := h.provenanceRetirementPlan(ctx, project, addonID, serviceID, request.Profile, request.IdempotencyKey)
		if err != nil || current.Namespace != plan.Namespace {
			return fmt.Errorf("provenance retirement scope changed")
		}
		return nil
	}
	plan.Reason = request.Reason
	receipt, err := h.k8sClient.RetireProvenanceRead(c.Request.Context(), plan, c.Param("job_name"), request.ExpectedPlan, *request.DryRun)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "provenance retirement refused or held; inspect the same operation key", "hold_stage": k8s.InitialJobHoldStage(err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"retirement": receipt, "scope": provenanceRetirementScope})
}

func (h *Handler) PortalProvenanceRetirementStatus(c *gin.Context) {
	if h.repos == nil || h.repos.Projects == nil || h.repos.DatabaseAddons == nil || h.repos.Services == nil || h.k8sClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "provenance retirement status unavailable"})
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
	addonID, idErr := uuid.Parse(c.Param("id"))
	if idErr != nil || addonID == uuid.Nil || addonID.String() != c.Param("id") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid addon ID"})
		return
	}
	addon, err := h.repos.DatabaseAddons.GetByID(c.Request.Context(), addonID)
	if err != nil || addon == nil || addon.ProjectID != project.ID {
		c.JSON(http.StatusNotFound, gin.H{"error": "owned addon unavailable"})
		return
	}
	receipt, labels, err := h.k8sClient.ReadProvenanceRetirement(c.Request.Context(), addon.K8sNamespace, c.Param("job_name"), project.ID, addonID)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "provenance retirement not conclusive", "hold_stage": k8s.InitialJobHoldStage(err)})
		return
	}
	serviceID, err := uuid.Parse(labels["service_id"])
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "owned retirement service unavailable"})
		return
	}
	if _, err = h.provenanceRetirementPlan(c.Request.Context(), project, addonID, serviceID, labels["profile"], ""); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "owned retirement scope unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"retirement": receipt, "scope": provenanceRetirementScope})
}
