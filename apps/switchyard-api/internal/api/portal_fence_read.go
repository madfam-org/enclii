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

type portalFenceReadRequest struct {
	Profile        string                    `json:"profile"`
	ServiceID      string                    `json:"service_id"`
	Input          *k8s.PortalFenceReadInput `json:"pedido"`
	DryRun         *bool                     `json:"dry_run"`
	IdempotencyKey string                    `json:"idempotency_key"`
	ExpectedPlan   string                    `json:"expect_plan_sha256,omitempty"`
	Reason         string                    `json:"reason,omitempty"`
}

func parsePortalFenceRead(b []byte) (portalFenceReadRequest, error) {
	var r portalFenceReadRequest
	bad := func() (portalFenceReadRequest, error) {
		return portalFenceReadRequest{}, fmt.Errorf("closed provenance read request refused")
	}
	if k8s.DecodeInitialBootstrapJSON(b, &r) != nil || r.Profile != k8s.PortalFenceReadProfile || r.DryRun == nil || k8s.ValidatePortalFenceReadInput(r.Input) != nil {
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

// PortalFenceRead creates only the closed independent observation Job.
// It cannot choose an image/namespace/command, reset or stop an active exercise.
func (h *Handler) PortalFenceRead(c *gin.Context) {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 8192))
	r, parseErr := parsePortalFenceRead(body)
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
	plan, proof, err := h.portalFenceReadPlan(c.Request.Context(), project, addonID, serviceID, r.Input, r.IdempotencyKey, reason, nil)
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
		current, _, err := h.portalFenceReadPlan(ctx, project, addonID, serviceID, r.Input, r.IdempotencyKey, reason, proof)
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
		c.JSON(http.StatusConflict, gin.H{"error": "provenance Job refused or held; inspect the same operation key"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"job": receipt, "producer": proof, "plan_sha256": sha, "scope": "PROVENANCE_ONLY retained fence snapshot; no reset, close, release or adoption authority"})
}

func (h *Handler) portalFenceReadPlan(ctx context.Context, project *types.Project, addonID, serviceID uuid.UUID, input *k8s.PortalFenceReadInput, key, reason string, knownProof *bootstrapProducerProof) (k8s.IsolatedInitialJobPlan, *bootstrapProducerProof, error) {
	return h.portalRuntimeReadPlan(ctx, project, addonID, serviceID, k8s.PortalFenceReadProfile, input, key, reason, knownProof)
}

func (h *Handler) PortalFenceReadStatus(c *gin.Context) {
	if !strings.HasPrefix(c.Param("job_name"), "read-") {
		c.JSON(http.StatusNotFound, gin.H{"error": "owned provenance read unavailable"})
		return
	}
	h.InitialBootstrapAddonStatus(c)
}
