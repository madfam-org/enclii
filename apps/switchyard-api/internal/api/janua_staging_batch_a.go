package api

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Review checkpoint: no existing route invokes this adapter. Both registration
// and this constant must be reviewed separately before native writes are enabled.
const januaBatchAApplySupported = false

type januaBatchARequest struct {
	Phase        string `json:"phase"`
	Plan         string `json:"plan"`
	OperationID  string `json:"operation_id"`
	ExpectedPlan string `json:"expected_plan"`
	Reason       string `json:"reason"`
	DryRun       *bool  `json:"dry_run"`
}

func registerJanuaStagingBatchARoutes(platform *gin.RouterGroup, h *Handler) {
	platform.POST("/projects/:slug/environments/staging/prepare", h.RequirePlatformOperator(), h.JanuaStagingBatchAPrepare)
}

func (h *Handler) JanuaStagingBatchAPrepare(c *gin.Context) {
	var request januaBatchARequest
	if decodeKarafielStageBody(c, &request) != nil || c.Param("slug") != "janua" || !januaBatchAPhaseValid(request.Phase) || request.Plan == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "closed Janua staging phase, catalog plan and operation required"})
		return
	}
	operation, err := uuid.Parse(request.OperationID)
	actor, actorErr := uuid.Parse(c.GetString("user_id"))
	if err != nil || operation == uuid.Nil || operation.String() != request.OperationID || actorErr != nil || actor == uuid.Nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "authenticated platform actor and canonical operation UUID required"})
		return
	}
	dry := request.DryRun == nil || *request.DryRun
	if !dry && (len(request.ExpectedPlan) != 64 || len(strings.TrimSpace(request.Reason)) < 8 || len(request.Reason) > 256 || strings.ContainsAny(request.Reason, "\r\n\x00")) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "apply requires a bound plan fingerprint and bounded reason"})
		return
	}
	if !dry && !januaBatchAApplySupported {
		c.JSON(http.StatusConflict, gin.H{"apply_supported": false, "error": "BatchA native writer review is incomplete; no effects were invoked"})
		return
	}
	source, err := h.readJanuaBatchASource(c.Request.Context(), actor, operation, request.Plan, request.Phase)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"apply_supported": false, "error": "current persisted Janua staging authority refused"})
		return
	}
	response := gin.H{"apply_supported": januaBatchAApplySupported, "dry_run": dry, "phase": request.Phase, "operation": source.Operation, "state": "PREVIEW_ONLY", "activation_held": true, "migrations_supported": false, "promotion_supported": false}
	if dry {
		c.JSON(http.StatusOK, response)
		return
	}
	if request.ExpectedPlan != source.Operation.PlanSHA256 {
		c.JSON(http.StatusConflict, gin.H{"error": "Janua staging plan changed; preview again"})
		return
	}
	if h.k8sClient == nil || h.k8sClient.Kube() == nil || h.k8sClient.DynamicClient == nil || h.addonService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "closed Janua native adapter unavailable"})
		return
	}
	// A stable fingerprint detects changes; it is never an authorization token.
	revalidate := func(ctx context.Context) error {
		current, e := h.readJanuaBatchASource(ctx, actor, operation, request.Plan, request.Phase)
		if e != nil || current.Operation != source.Operation {
			return fmt.Errorf("Janua staging persisted source changed")
		}
		return nil
	}
	state, err := h.applyJanuaBatchAPhase(c.Request.Context(), source, request.Phase, strings.TrimSpace(request.Reason), revalidate)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Janua staging phase held; inspect the same operation without retrying effects", "operation_id": operation, "activation_held": true})
		return
	}
	response["state"] = state
	c.JSON(http.StatusOK, response)
}

func januaBatchAPhaseValid(phase string) bool {
	return phase == "reserve" || phase == "namespaces" || phase == "provision" || phase == "custody"
}

func (h *Handler) applyJanuaBatchAPhase(ctx context.Context, source *januaBatchASource, phase, reason string, revalidate func(context.Context) error) (string, error) {
	if h == nil || source == nil || !januaBatchAPhaseValid(phase) || revalidate == nil || h.k8sClient == nil || h.k8sClient.Kube() == nil {
		return "", fmt.Errorf("closed Janua phase authority unavailable")
	}
	if err := revalidate(ctx); err != nil {
		return "", err
	}
	o := source.Operation
	created, journalUID, err := addons.ClaimJanuaStagingOperation(ctx, h.k8sClient.Kube(), o)
	if err != nil {
		return "", err
	}
	if created && phase != "reserve" {
		return "", fmt.Errorf("Janua staging reservation claim missing")
	}
	alreadyClaimed, err := addons.ReadJanuaStagingPhase(ctx, h.k8sClient.Kube(), o, journalUID, phase, reason)
	if err != nil {
		return "", err
	}
	if alreadyClaimed {
		return "READ_ONLY_REPLAY_INSPECT_RECEIPT", nil
	}
	// Complete read-only prerequisites BEFORE consuming the one execution claim.
	var binding addons.JanuaStagingNamespaceBinding
	var peers []addons.KarafielControlPeer
	var operator metav1.LabelSelector
	if phase == "namespaces" {
		for _, name := range []string{addons.JanuaStagingNamespace, "project-" + o.ProjectID.String()[:8]} {
			observation, e := h.readJanuaStageNamespace(ctx, name, o.ProjectID)
			if e != nil || observation.State != "ABSENT" {
				return "", fmt.Errorf("fresh Janua staging namespace required; no adoption")
			}
		}
	}
	if phase == "provision" || phase == "custody" {
		binding, err = addons.ReadJanuaStagingNamespaces(ctx, h.k8sClient.Kube(), o, journalUID)
		if err != nil {
			return "", err
		}
		fixed, endpointVersions, e := h.readJanuaStageAPIPeers(ctx)
		if e != nil {
			return "", e
		}
		for _, peer := range fixed {
			peers = append(peers, addons.KarafielControlPeer{HostRoute: peer.HostRoute, Port: peer.Port})
		}
		var operatorVersions []string
		operator, operatorVersions, err = h.readJanuaStageOperator(ctx)
		if err != nil {
			return "", err
		}
		storage := januaStageAddonPreview{Contract: fixedJanuaStageDatabaseContract()}
		if h.readJanuaStageRetainedStorage(ctx, &storage) != nil || !storage.StorageRetainsData || storage.StorageClassResourceVersion == "" {
			return "", fmt.Errorf("retained Janua staging storage authority unavailable")
		}
		persistedRevalidate := revalidate
		revalidate = func(current context.Context) error {
			if e := persistedRevalidate(current); e != nil {
				return e
			}
			actual, versions, e := h.readJanuaStageAPIPeers(current)
			if e != nil || !reflect.DeepEqual(actual, fixed) || !reflect.DeepEqual(versions, endpointVersions) {
				return fmt.Errorf("Janua staging API endpoint authority changed")
			}
			selector, versions, e := h.readJanuaStageOperator(current)
			if e != nil || !reflect.DeepEqual(selector, operator) || !reflect.DeepEqual(versions, operatorVersions) {
				return fmt.Errorf("Janua staging CNPG operator authority changed")
			}
			observed := januaStageAddonPreview{Contract: fixedJanuaStageDatabaseContract()}
			if h.readJanuaStageRetainedStorage(current, &observed) != nil || !observed.StorageRetainsData || observed.StorageClassUID != storage.StorageClassUID || observed.StorageClassResourceVersion != storage.StorageClassResourceVersion {
				return fmt.Errorf("Janua staging storage authority changed")
			}
			return nil
		}
		if phase == "provision" {
			_, err = h.addonService.ProvisionJanuaStaging(ctx, source.Addon, o, journalUID, binding, source.Plan.MaxConnections, peers, operator, revalidate, true)
		} else {
			_, err = h.addonService.ReadJanuaStagingCustody(ctx, source.Addon, o, journalUID, source.Plan.MaxConnections, peers, operator, revalidate)
		}
		if err != nil {
			return "", err
		}
	}
	fresh, err := addons.ClaimJanuaStagingPhase(ctx, h.k8sClient.Kube(), o, journalUID, phase, reason)
	if err != nil {
		return "", err
	}
	if !fresh {
		return "READ_ONLY_REPLAY", nil
	}
	if err = revalidate(ctx); err != nil {
		return "", err
	}
	switch phase {
	case "reserve":
		_, err = h.repos.ReserveJanuaStaging(ctx, o.ProjectID, o.EnvironmentID, o.RegistrationID, o.ServiceID, o.AddonID, o.ActorID, o.OperationID, source.Plan.Code, reason)
	case "namespaces":
		app, e := h.k8sClient.PrepareJanuaStagingNamespace(ctx, addons.JanuaStagingNamespace, o.ProjectID, "", revalidate, false)
		if e != nil {
			return "", e
		}
		binding.ApplicationUID = app.UID
		if err = revalidate(ctx); err != nil {
			return "", err
		}
		database, e := h.k8sClient.PrepareJanuaStagingNamespace(ctx, "project-"+o.ProjectID.String()[:8], o.ProjectID, "", revalidate, false)
		if e != nil {
			return "", e
		}
		binding.AddonUID = database.UID
		err = addons.WriteJanuaStagingNamespaces(ctx, h.k8sClient.Kube(), o, journalUID, binding)
	case "provision":
		var birth *addons.JanuaStagingBirthReceipt
		birth, err = h.addonService.ProvisionJanuaStaging(ctx, source.Addon, o, journalUID, binding, source.Plan.MaxConnections, peers, operator, revalidate, false)
		if err == nil {
			if e := revalidate(ctx); e != nil {
				return "", e
			}
			err = h.repos.RecordJanuaStagingNativeBirth(ctx, o.ProjectID, o.EnvironmentID, o.RegistrationID, o.AddonID, o.ActorID, o.OperationID, string(birth.ClusterUID), string(birth.RuntimeSecretUID), reason)
		}
	case "custody":
		_, err = h.projectJanuaStagingCustody(ctx, source.Addon, o, journalUID, source.Plan.MaxConnections, peers, operator, revalidate)
	}
	if err != nil {
		return "", err
	}
	return "PHASE_RECORDED_ACTIVATION_HELD", nil
}
