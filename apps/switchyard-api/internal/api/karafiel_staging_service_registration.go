package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

type karafielStageServiceRequest struct {
	OperationID  string `json:"operation_id"`
	DryRun       *bool  `json:"dry_run"`
	ExpectedPlan string `json:"expect_plan_sha256,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

func parseKarafielStageServiceRequest(raw []byte) (karafielStageServiceRequest, uuid.UUID, bool) {
	var request karafielStageServiceRequest
	var keys map[string]json.RawMessage
	if len(raw) > 8192 || k8s.DecodeInitialBootstrapJSON(raw, &keys) != nil || k8s.DecodeInitialBootstrapJSON(raw, &request) != nil || request.DryRun == nil {
		return request, uuid.Nil, false
	}
	id, err := uuid.Parse(request.OperationID)
	if err != nil || id == uuid.Nil || id.String() != request.OperationID {
		return request, uuid.Nil, false
	}
	if *request.DryRun {
		return request, id, len(keys) == 2 && request.ExpectedPlan == "" && request.Reason == ""
	}
	return request, id, len(keys) == 4 && len(request.ExpectedPlan) == 64 && januaStagingIntentHash.MatchString(request.ExpectedPlan) && request.Reason != "" && strings.TrimSpace(request.Reason) == request.Reason && len(request.Reason) <= 512 && strings.IndexFunc(request.Reason, unicode.IsControl) < 0
}

func registerKarafielStageServiceAdminRoutes(platform *gin.RouterGroup, h *Handler) {
	registerKarafielProductionTargetAdminRoutes(platform, h)
	base := "/projects/:slug/environments/staging/service-registration"
	platform.POST(base, h.RequirePlatformOperator(), h.KarafielStageServiceRegistration)
	platform.GET(base+"/:operation_id", h.RequirePlatformOperator(), h.ReadKarafielStageServiceRegistration)
}

func (h *Handler) karafielStageRegistrationOperator(c *gin.Context) (uuid.UUID, bool) {
	actor, err := uuid.Parse(c.GetString("user_id"))
	if err != nil || actor == uuid.Nil || c.Param("slug") != "karafiel" || c.Request.URL.RawQuery != "" || h.repos == nil || h.repos.DB() == nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "held_stage_registration_refused"})
		return uuid.Nil, false
	}
	var allowed bool
	if h.repos.DB().QueryRowContext(c.Request.Context(), `SELECT is_platform_admin AND active FROM users WHERE id=$1`, actor).Scan(&allowed) != nil || !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "held_stage_registration_refused"})
		return uuid.Nil, false
	}
	return actor, true
}

func (h *Handler) KarafielStageServiceRegistration(c *gin.Context) {
	actor, ok := h.karafielStageRegistrationOperator(c)
	if !ok {
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 8192))
	r, id, ok := parseKarafielStageServiceRequest(raw)
	if err != nil || !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "closed_held_stage_registration_required"})
		return
	}
	if !*r.DryRun {
		h.applyKarafielStageServiceRegistration(c, actor, id, r)
		return
	}
	snapshot, err := h.readKarafielStageRegistrationSnapshot(c.Request.Context(), id)
	if err != nil {
		h.targetPlanRefusal(c, err)
		return
	}
	plan, err := workloadtargets.ValidateKarafielStagingSnapshot(snapshot)
	if err != nil {
		h.targetPlanRefusal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"schema": "enclii-karafiel-held-stage-service-plan-v1", "phase": "HELD_METADATA_PLAN", "activation_held": true, "namespace_ownership_pending": plan.Identity.NamespaceOwnershipPending, "dry_run": true, "operation_id": id, "plan": plan})
}

func (h *Handler) applyKarafielStageServiceRegistration(c *gin.Context, actor, id uuid.UUID, r karafielStageServiceRequest) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	project, err := h.repos.Projects.GetBySlug("karafiel")
	if err != nil || project == nil {
		h.targetPlanRefusal(c, workloadtargets.ErrIntent)
		return
	}
	session, err := (workloadtargets.Store{DB: h.repos.DB()}).LockProject(ctx, project.ID)
	if err != nil {
		h.targetPlanRefusal(c, err)
		return
	}
	defer session.Close()
	if err = session.RequireOperator(ctx, actor); err != nil {
		h.targetPlanRefusal(c, err)
		return
	}
	var active bool
	if session.Tx().QueryRowContext(ctx, `SELECT active FROM users WHERE id=$1 FOR SHARE`, actor).Scan(&active) != nil || !active {
		c.JSON(http.StatusForbidden, gin.H{"error": "held_stage_registration_refused"})
		return
	}
	request := workloadtargets.Request{Project: "karafiel", ServiceID: workloadtargets.KarafielStagingServiceID(project.ID, id), Environment: "staging", Namespace: workloadtargets.KarafielStagingNamespace, Deployment: "karafiel-api", IdempotencyID: id, ExpectedPlan: r.ExpectedPlan, Reason: r.Reason}
	want, err := workloadtargets.RequestHash(request, actor)
	if err != nil {
		h.targetPlanRefusal(c, err)
		return
	}
	previous, err := session.Store().Receipt(ctx, id)
	if err != nil {
		h.targetPlanRefusal(c, err)
		return
	}
	if previous != nil {
		if previous.RequestSHA != want || previous.Binding.Identity.SourceAuthority != workloadtargets.KarafielStagingHeldAuthority || previous.Binding.Identity.ServiceID != request.ServiceID {
			h.targetPlanRefusal(c, workloadtargets.ErrConflict)
			return
		}
		c.JSON(http.StatusOK, heldStageRegistrationResponse(previous, true))
		return
	}
	scoped := *h
	scoped.repos = h.repos.InTransaction(session.Tx())
	scoped.workloadTargetSession = session
	snapshot, err := scoped.readKarafielStageRegistrationSnapshot(ctx, id)
	if err != nil {
		h.targetPlanRefusal(c, err)
		return
	}
	if err = session.LockIntent(ctx, snapshot.Environment.ID, snapshot.Registration.ID); err != nil {
		h.targetPlanRefusal(c, err)
		return
	}
	snapshot, err = scoped.readKarafielStageRegistrationSnapshot(ctx, id)
	if err != nil {
		h.targetPlanRefusal(c, err)
		return
	}
	plan, err := workloadtargets.ValidateKarafielStagingSnapshot(snapshot)
	if err != nil || plan.SHA != r.ExpectedPlan {
		h.targetPlanRefusal(c, workloadtargets.ErrConflict)
		return
	}
	if err = session.InsertKarafielStagingService(ctx, id, snapshot.Service); err != nil {
		h.targetPlanRefusal(c, err)
		return
	}
	receipt, err := session.Persist(ctx, request, actor, plan)
	if err != nil {
		h.targetPlanRefusal(c, err)
		return
	}
	if err = session.Commit(); err != nil {
		previous, readErr := (workloadtargets.Store{DB: h.repos.DB()}).Receipt(ctx, id)
		if readErr != nil || previous == nil || previous.RequestSHA != want {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "held_stage_registration_commit_unknown", "operation_id": id})
			return
		}
		receipt = previous
	}
	c.JSON(http.StatusOK, heldStageRegistrationResponse(receipt, false))
}

func heldStageRegistrationResponse(receipt *workloadtargets.Receipt, replay bool) gin.H {
	return gin.H{"schema": "enclii-karafiel-held-stage-service-receipt-v1", "phase": "HELD_METADATA_REGISTERED", "activation_held": true, "namespace_ownership_pending": receipt.Binding.Identity.NamespaceOwnershipPending, "dry_run": false, "idempotent": replay, "receipt": receipt}
}

func (h *Handler) ReadKarafielStageServiceRegistration(c *gin.Context) {
	if _, ok := h.karafielStageRegistrationOperator(c); !ok {
		return
	}
	id, err := uuid.Parse(c.Param("operation_id"))
	if err != nil || id == uuid.Nil || id.String() != c.Param("operation_id") || c.Request.ContentLength != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "closed_held_stage_registration_required"})
		return
	}
	r, err := (workloadtargets.Store{DB: h.repos.DB()}).Receipt(c.Request.Context(), id)
	p, projectErr := h.repos.Projects.GetBySlug("karafiel")
	if err != nil || projectErr != nil || p == nil || r == nil || r.Binding.Identity.SourceAuthority != workloadtargets.KarafielStagingHeldAuthority ||
		r.Binding.Identity.ProjectID != p.ID || r.Binding.Identity.ServiceID != workloadtargets.KarafielStagingServiceID(p.ID, id) {
		c.JSON(http.StatusNotFound, gin.H{"error": "held_stage_registration_unavailable"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, heldStageRegistrationResponse(r, true))
}

type stageRegistrationQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func stageServiceCandidate(project, id uuid.UUID) *types.Service {
	namespace := workloadtargets.KarafielStagingNamespace
	return &types.Service{ID: workloadtargets.KarafielStagingServiceID(project, id), ProjectID: project, Name: workloadtargets.KarafielStagingServiceName, Type: types.ServiceTypeWeb, Region: "default", GitRepo: "https://github.com/madfam-org/karafiel", AppPath: "apps/api", AutoDeploy: false, AutoDeployBranch: "release/karafiel-staging", AutoDeployEnv: "staging", K8sNamespace: &namespace, BuildConfig: types.BuildConfig{Type: types.BuildTypeDockerfile, Dockerfile: "apps/api/Dockerfile", Context: "."}}
}
