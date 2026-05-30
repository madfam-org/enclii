package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	batchv1 "k8s.io/api/batch/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

type operatorOperationRequest struct {
	Operation      string            `json:"operation"`
	DryRun         bool              `json:"dry_run"`
	Reason         string            `json:"reason,omitempty"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
	Scope          map[string]string `json:"scope,omitempty"`
	Args           map[string]string `json:"args,omitempty"`
}

type operatorOperationStep struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type operatorOperationResponse struct {
	OperationID string                  `json:"operation_id,omitempty"`
	AuditID     string                  `json:"audit_id,omitempty"`
	Operation   string                  `json:"operation"`
	Status      string                  `json:"status"`
	DryRun      bool                    `json:"dry_run"`
	Summary     string                  `json:"summary,omitempty"`
	Data        any                     `json:"data,omitempty"`
	Steps       []operatorOperationStep `json:"steps,omitempty"`
	Warnings    []string                `json:"warnings,omitempty"`
	Next        []string                `json:"next,omitempty"`
}

type operatorCapability struct {
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	Description string   `json:"description,omitempty"`
	Actions     []string `json:"actions,omitempty"`
	Scopes      []string `json:"scopes,omitempty"`
}

type operatorCapabilitiesResponse struct {
	Capabilities []operatorCapability `json:"capabilities"`
}

// HandleOpsOperation is the P0 operation contract endpoint for kubectl/Argo
// replacement workflows. Dry-run requests return a deterministic plan; apply
// requests return 501 until the concrete adapter is wired.
func (h *Handler) HandleOpsOperation(c *gin.Context) {
	domain := c.Param("domain")
	action := c.Param("action")
	h.handleOperatorOperation(c, "ops", domain, action, opsCapabilities)
}

// HandleProviderOperation is the P0 operation contract endpoint for
// gh/Cloudflare/Porkbun/Hetzner replacement workflows. Dry-run requests return
// a deterministic plan; apply requests return 501 until the provider adapter is
// wired.
func (h *Handler) HandleProviderOperation(c *gin.Context) {
	provider := c.Param("provider")
	action := c.Param("action")
	h.handleOperatorOperation(c, "providers", provider, action, providerCapabilities)
}

func (h *Handler) handleOperatorOperation(c *gin.Context, prefix, domain, action string, capabilities []operatorCapability) {
	var req operatorOperationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid operation request: %s", err.Error())})
		return
	}

	operation := strings.TrimSpace(req.Operation)
	if operation == "" {
		operation = fmt.Sprintf("%s.%s.%s", prefix, domain, action)
	}
	if !operationSupported(domain, action, capabilities) {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("unsupported operation %s.%s", domain, action)})
		return
	}
	if !req.DryRun && strings.TrimSpace(req.Reason) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "reason is required when dry_run=false"})
		return
	}

	if req.DryRun {
		if resp, handled := h.handleReadOnlyOperatorOperation(c, prefix, domain, action, operation, req); handled {
			c.JSON(http.StatusOK, resp)
			return
		}
		if resp, handled := h.handleApplyOperatorDryRun(c.Request.Context(), prefix, domain, action, operation, req); handled {
			c.JSON(http.StatusOK, resp)
			return
		}
	}
	if !req.DryRun {
		if resp, statusCode, handled := h.handleApplyOperatorOperation(c.Request.Context(), prefix, domain, action, operation, req); handled {
			c.JSON(statusCode, resp)
			return
		}
	}

	resp := operatorOperationResponse{
		OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
		Operation:   operation,
		Status:      "planned",
		DryRun:      req.DryRun,
		Summary:     fmt.Sprintf("%s.%s is covered by the Enclii operation contract", domain, action),
		Steps: []operatorOperationStep{
			{Name: "authorize", Status: "planned", Detail: "check caller RBAC and provider scope"},
			{Name: "load-state", Status: "planned", Detail: "read current live/provider state"},
			{Name: "diff", Status: "planned", Detail: "compare requested intent with current state"},
			{Name: "audit", Status: "planned", Detail: "write audit event before mutation"},
		},
		Warnings: []string{
			"adapter execution is not wired in this build; dry-run is safe, apply is blocked",
		},
		Next: []string{
			"wire a concrete adapter for this capability",
			"add policy gates and idempotent apply semantics",
			"bind Selva agents to this endpoint instead of raw shell tools",
		},
	}

	if req.DryRun {
		c.JSON(http.StatusOK, resp)
		return
	}

	resp.Status = "adapter_required"
	resp.DryRun = false
	c.JSON(http.StatusNotImplemented, resp)
}

func (h *Handler) handleApplyOperatorDryRun(ctx context.Context, prefix, domain, action, operation string, req operatorOperationRequest) (operatorOperationResponse, bool) {
	if prefix == "providers" {
		if domain == "cloudflare" && action == "dns-apply" {
			return h.handleProviderCloudflareDNSApplyDryRun(ctx, operation, req), true
		}
		if domain == "cloudflare" && action == "zone-add-apply" {
			return h.handleProviderCloudflareZoneAddApplyDryRun(ctx, operation, req), true
		}
		if domain == "cloudflare" && action == "tunnels-apply" {
			return h.handleProviderCloudflareTunnelsApplyDryRun(ctx, operation, req), true
		}
		if domain == "porkbun" && action == "dns-apply" {
			return h.handleProviderPorkbunDNSApplyDryRun(ctx, operation, req), true
		}
		if domain == "porkbun" && action == "nameservers-apply" {
			return h.handleProviderPorkbunNameserversApplyDryRun(ctx, operation, req), true
		}
		if domain == "resend" && action == "domain-add-apply" {
			return h.handleResendDomainAddApplyDryRun(ctx, operation, req), true
		}
		if domain == "resend" && action == "domain-verify-apply" {
			return h.handleResendDomainVerifyApplyDryRun(ctx, operation, req), true
		}
		if domain == "resend" && action == "domain-dns-apply" {
			return h.handleResendDomainDNSApplyDryRun(ctx, operation, req), true
		}
		if domain == "resend" && action == "send-test-apply" {
			return h.handleResendSendTestApplyDryRun(ctx, operation, req), true
		}
		return operatorOperationResponse{}, false
	}
	if prefix == "ops" && domain == "apps" && action == "sync-sweep" {
		return h.handleOpsAppsSyncSweepDryRun(ctx, operation, req), true
	}
	if prefix == "ops" && domain == "secrets" && action == "sync-sweep" {
		return h.handleOpsSecretsSyncSweepDryRun(ctx, operation, req), true
	}
	if prefix == "ops" && domain == "storage" && action == "storageclass-apply" {
		return h.handleOpsStorageStorageClassApplyDryRun(ctx, operation, req), true
	}
	if prefix == "ops" && domain == "policy" && action == "cosign-enable" {
		return h.handleOpsPolicyCosignEnableDryRun(ctx, operation, req), true
	}
	if prefix == "ops" && domain == "storage" && action == "settings-apply" {
		return h.handleOpsStorageSettingsApplyDryRun(ctx, operation, req), true
	}
	if prefix == "ops" && domain == "storage" && action == "prune-detached" {
		return h.handleOpsStoragePruneDetachedDryRun(ctx, operation, req), true
	}
	if prefix != "ops" {
		return operatorOperationResponse{}, false
	}

	operationID := fmt.Sprintf("op_%d", time.Now().UTC().UnixNano())
	namespace := operationNamespace(req, "default")
	if domain == "apps" {
		namespace = operationNamespace(req, "argocd")
	}
	target := operationTarget(req)
	var mutation string
	var adapterReady bool
	switch domain + "." + action {
	case "apps.sync":
		mutation = "patch Argo Application operation.sync"
		adapterReady = h != nil && h.k8sClient != nil && h.k8sClient.DynamicClient != nil
	case "apps.retire":
		mutation = "delete Argo Application with orphan propagation unless cascade=true"
		adapterReady = h != nil && h.k8sClient != nil && h.k8sClient.DynamicClient != nil
	case "jobs.trigger":
		mutation = "create one-off Job from the live CronJob template"
		adapterReady = h != nil && h.opsKubeClient() != nil
	case "secrets.refresh":
		mutation = "patch ExternalSecret force-sync and Enclii audit annotations"
		adapterReady = h != nil && h.k8sClient != nil && h.k8sClient.DynamicClient != nil
	case "secrets.sync":
		mutation = "patch ExternalSecret force-sync and Enclii audit annotations"
		adapterReady = h != nil && h.k8sClient != nil && h.k8sClient.DynamicClient != nil
	case "secrets.rotate":
		mutation = "plan Vault/ESO rotation, dual-consumer cutover, verification, and old-value revocation"
		adapterReady = false
	case "storage.storageclass-apply":
		mutation = "create missing Longhorn StorageClasses from git-backed GA manifest"
		adapterReady = h != nil && h.opsKubeClient() != nil
	case "policy.cosign-enable":
		mutation = "label namespaces enclii.dev/verify-signatures=true for Kyverno cosign enforce"
		adapterReady = h != nil && h.opsKubeClient() != nil
	case "storage.settings-apply":
		mutation = "patch Longhorn Setting CR CPU percentages to match helm values"
		adapterReady = h != nil && h.k8sClient != nil && h.k8sClient.DynamicClient != nil
	default:
		return operatorOperationResponse{}, false
	}

	warnings := []string{}
	if target == "" {
		warnings = append(warnings, "missing args.target or scope.target; apply would be rejected")
	}
	if !adapterReady {
		warnings = append(warnings, "apply adapter client is not configured in this Switchyard API instance")
	}

	status := "planned"
	if adapterReady && target != "" {
		status = "ready_to_apply"
	}
	return operatorOperationResponse{
		OperationID: operationID,
		Operation:   operation,
		Status:      status,
		DryRun:      true,
		Summary:     fmt.Sprintf("%s.%s dry-run completed through Enclii", domain, action),
		Data: map[string]any{
			"namespace": namespace,
			"target":    target,
			"mutation":  mutation,
			"apply":     adapterReady && target != "",
		},
		Steps: []operatorOperationStep{
			{Name: "authorize", Status: "planned", Detail: "check caller RBAC and reason on apply"},
			{Name: "load-state", Status: "planned", Detail: "load current live resource state through the configured adapter"},
			{Name: "diff", Status: "planned", Detail: mutation},
			{Name: "audit", Status: "planned", Detail: "write Enclii operation annotations/audit metadata before mutation"},
		},
		Warnings: warnings,
		Next: []string{
			"rerun with --apply and a reason to execute this Enclii operation",
			"poll the corresponding read operation until the target converges",
		},
	}, true
}

func (h *Handler) handleApplyOperatorOperation(ctx context.Context, prefix, domain, action, operation string, req operatorOperationRequest) (operatorOperationResponse, int, bool) {
	if prefix == "providers" && domain == "cloudflare" && action == "dns-apply" {
		resp, statusCode := h.handleProviderCloudflareDNSApply(ctx, operation, req)
		return resp, statusCode, true
	}
	if prefix == "providers" && domain == "cloudflare" && action == "zone-add-apply" {
		resp, statusCode := h.handleProviderCloudflareZoneAddApply(ctx, operation, req)
		return resp, statusCode, true
	}
	if prefix == "providers" && domain == "cloudflare" && action == "tunnels-apply" && h.tunnelRoutesService != nil {
		resp, statusCode := h.handleProviderCloudflareTunnelsApply(ctx, operation, req)
		return resp, statusCode, true
	}
	if prefix == "providers" && domain == "porkbun" && action == "dns-apply" {
		resp, statusCode := h.handleProviderPorkbunDNSApply(ctx, operation, req)
		return resp, statusCode, true
	}
	if prefix == "providers" && domain == "porkbun" && action == "nameservers-apply" {
		resp, statusCode := h.handleProviderPorkbunNameserversApply(ctx, operation, req)
		return resp, statusCode, true
	}
	if prefix == "providers" && domain == "resend" && action == "domain-add-apply" {
		resp, statusCode := h.handleResendDomainAddApply(ctx, operation, req)
		return resp, statusCode, true
	}
	if prefix == "providers" && domain == "resend" && action == "domain-verify-apply" {
		resp, statusCode := h.handleResendDomainVerifyApply(ctx, operation, req)
		return resp, statusCode, true
	}
	if prefix == "providers" && domain == "resend" && action == "domain-dns-apply" {
		resp, statusCode := h.handleResendDomainDNSApply(ctx, operation, req)
		return resp, statusCode, true
	}
	if prefix == "providers" && domain == "resend" && action == "send-test-apply" {
		resp, statusCode := h.handleResendSendTestApply(ctx, operation, req)
		return resp, statusCode, true
	}
	if prefix == "ops" && domain == "apps" && action == "sync-sweep" && h.k8sClient != nil && h.k8sClient.DynamicClient != nil {
		resp, statusCode := h.handleOpsAppsSyncSweepApply(ctx, operation, req)
		return resp, statusCode, true
	}
	if prefix == "ops" && domain == "secrets" && action == "sync-sweep" && h.k8sClient != nil && h.k8sClient.DynamicClient != nil {
		resp, statusCode := h.handleOpsSecretsSyncSweepApply(ctx, operation, req)
		return resp, statusCode, true
	}
	if prefix == "ops" && domain == "apps" && action == "sync" && h.k8sClient != nil && h.k8sClient.DynamicClient != nil {
		resp, statusCode := h.handleOpsAppsSyncApply(ctx, operation, req)
		return resp, statusCode, true
	}
	if prefix == "ops" && domain == "apps" && action == "retire" && h.k8sClient != nil && h.k8sClient.DynamicClient != nil {
		resp, statusCode := h.handleOpsAppsRetireApply(ctx, operation, req)
		return resp, statusCode, true
	}
	if prefix == "ops" && domain == "jobs" && action == "trigger" && h.opsKubeClient() != nil {
		resp, statusCode := h.handleOpsJobsTriggerApply(ctx, operation, req)
		return resp, statusCode, true
	}
	if prefix == "ops" && domain == "secrets" && action == "refresh" && h.k8sClient != nil && h.k8sClient.DynamicClient != nil {
		resp, statusCode := h.handleOpsSecretsRefreshApply(ctx, operation, req)
		return resp, statusCode, true
	}
	if prefix == "ops" && domain == "secrets" && action == "sync" && h.k8sClient != nil && h.k8sClient.DynamicClient != nil {
		resp, statusCode := h.handleOpsSecretsRefreshApply(ctx, operation, req)
		return resp, statusCode, true
	}
	if prefix == "ops" && domain == "storage" && action == "storageclass-apply" && h.opsKubeClient() != nil {
		resp, statusCode := h.handleOpsStorageStorageClassApply(ctx, operation, req)
		return resp, statusCode, true
	}
	if prefix == "ops" && domain == "policy" && action == "cosign-enable" && h.opsKubeClient() != nil {
		resp, statusCode := h.handleOpsPolicyCosignEnableApply(ctx, operation, req)
		return resp, statusCode, true
	}
	if prefix == "ops" && domain == "storage" && action == "settings-apply" && h.k8sClient != nil && h.k8sClient.DynamicClient != nil {
		resp, statusCode := h.handleOpsStorageSettingsApply(ctx, operation, req)
		return resp, statusCode, true
	}
	if prefix == "ops" && domain == "storage" && action == "prune-detached" && h.k8sClient != nil && h.k8sClient.DynamicClient != nil {
		resp, statusCode := h.handleOpsStoragePruneDetachedApply(ctx, operation, req)
		return resp, statusCode, true
	}
	return operatorOperationResponse{}, 0, false
}

func (h *Handler) handleOpsAppsSyncApply(ctx context.Context, operation string, req operatorOperationRequest) (operatorOperationResponse, int) {
	operationID := fmt.Sprintf("op_%d", time.Now().UTC().UnixNano())
	namespace := strings.TrimSpace(req.Scope["namespace"])
	if namespace == "" {
		namespace = strings.TrimSpace(req.Args["namespace"])
	}
	if namespace == "" {
		namespace = "argocd"
	}
	target := strings.TrimSpace(req.Args["target"])
	if target == "" {
		target = strings.TrimSpace(req.Scope["target"])
	}
	if target == "" {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "invalid_request",
			DryRun:      false,
			Summary:     "apps.sync requires a target Argo Application name",
			Warnings:    []string{"missing args.target or scope.target"},
		}, http.StatusBadRequest
	}

	appResource := h.k8sClient.DynamicClient.Resource(argoApplicationGVR).Namespace(namespace)
	app, err := appResource.Get(ctx, target, metav1.GetOptions{})
	if err != nil {
		statusCode := http.StatusInternalServerError
		status := "failed"
		if k8serrors.IsNotFound(err) {
			statusCode = http.StatusNotFound
			status = "not_found"
		}
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      status,
			DryRun:      false,
			Summary:     fmt.Sprintf("failed to load Argo Application %s/%s", namespace, target),
			Warnings:    []string{err.Error()},
		}, statusCode
	}

	if activeOperation, found, _ := unstructured.NestedMap(app.Object, "operation"); found && len(activeOperation) > 0 {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "already_running",
			DryRun:      false,
			Summary:     fmt.Sprintf("Argo Application %s/%s already has an active operation", namespace, target),
			Data: map[string]any{
				"application": target,
				"namespace":   namespace,
				"operation":   activeOperation,
			},
			Steps: []operatorOperationStep{
				{Name: "authorize", Status: "completed", Detail: "reason supplied and caller passed endpoint authorization"},
				{Name: "load-state", Status: "completed", Detail: "loaded Argo Application from cluster"},
				{Name: "diff", Status: "skipped", Detail: "existing Argo operation is still active"},
				{Name: "audit", Status: "skipped", Detail: "no mutation was submitted"},
			},
			Warnings: []string{"retry after the active Argo operation completes"},
		}, http.StatusConflict
	}

	revision := strings.TrimSpace(req.Args["revision"])
	prune := true
	if strings.EqualFold(strings.TrimSpace(req.Args["prune"]), "false") {
		prune = false
	}
	syncOptions := []string{"PruneLast=true"}
	if options := strings.TrimSpace(req.Args["sync_options"]); options != "" {
		syncOptions = splitCSV(options)
	}

	syncSpec := map[string]any{
		"prune":       prune,
		"syncOptions": syncOptions,
	}
	if revision != "" {
		syncSpec["revision"] = revision
	}
	now := time.Now().UTC().Format(time.RFC3339)
	annotations := map[string]string{
		"enclii.dev/last-ops-operation": operation,
		"enclii.dev/last-ops-reason":    req.Reason,
		"enclii.dev/last-ops-requested": now,
	}
	if req.IdempotencyKey != "" {
		annotations["enclii.dev/last-ops-idempotency-key"] = req.IdempotencyKey
	}
	patch := map[string]any{
		"metadata": map[string]any{
			"annotations": annotations,
		},
		"operation": map[string]any{
			"initiatedBy": map[string]any{
				"username": "enclii-ops",
			},
			"sync": syncSpec,
		},
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "failed",
			DryRun:      false,
			Summary:     "failed to build Argo sync patch",
			Warnings:    []string{err.Error()},
		}, http.StatusInternalServerError
	}

	updated, err := appResource.Patch(ctx, target, k8stypes.MergePatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "failed",
			DryRun:      false,
			Summary:     fmt.Sprintf("failed to submit Argo sync for %s/%s", namespace, target),
			Warnings:    []string{err.Error()},
		}, http.StatusInternalServerError
	}

	syncStatus, _, _ := unstructured.NestedString(app.Object, "status", "sync", "status")
	healthStatus, _, _ := unstructured.NestedString(app.Object, "status", "health", "status")
	currentRevision, _, _ := unstructured.NestedString(app.Object, "status", "sync", "revision")
	return operatorOperationResponse{
		OperationID: operationID,
		Operation:   operation,
		Status:      "submitted",
		DryRun:      false,
		Summary:     fmt.Sprintf("submitted Argo sync for %s/%s through Enclii", namespace, target),
		Data: map[string]any{
			"application":       target,
			"namespace":         namespace,
			"resourceVersion":   updated.GetResourceVersion(),
			"previousSync":      syncStatus,
			"previousHealth":    healthStatus,
			"previousRevision":  currentRevision,
			"requestedRevision": revision,
			"prune":             prune,
			"syncOptions":       syncOptions,
		},
		Steps: []operatorOperationStep{
			{Name: "authorize", Status: "completed", Detail: "reason supplied and caller passed endpoint authorization"},
			{Name: "load-state", Status: "completed", Detail: "loaded Argo Application from cluster"},
			{Name: "diff", Status: "completed", Detail: "operation submitted against current GitOps desired state"},
			{Name: "audit", Status: "completed", Detail: "annotated Application with operation reason and idempotency key"},
		},
		Next: []string{
			"poll ops.apps.status until sync and health converge",
			"escalate to human review if Argo reports Degraded, Error, or Unknown after timeout",
		},
	}, http.StatusAccepted
}

func (h *Handler) handleOpsAppsRetireApply(ctx context.Context, operation string, req operatorOperationRequest) (operatorOperationResponse, int) {
	operationID := fmt.Sprintf("op_%d", time.Now().UTC().UnixNano())
	namespace := strings.TrimSpace(req.Scope["namespace"])
	if namespace == "" {
		namespace = strings.TrimSpace(req.Args["namespace"])
	}
	if namespace == "" {
		namespace = "argocd"
	}
	target := strings.TrimSpace(req.Args["target"])
	if target == "" {
		target = strings.TrimSpace(req.Scope["target"])
	}
	if target == "" {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "invalid_request",
			DryRun:      false,
			Summary:     "apps.retire requires a target Argo Application name",
			Warnings:    []string{"missing args.target or scope.target"},
		}, http.StatusBadRequest
	}

	appResource := h.k8sClient.DynamicClient.Resource(argoApplicationGVR).Namespace(namespace)
	app, err := appResource.Get(ctx, target, metav1.GetOptions{})
	if err != nil {
		statusCode := http.StatusInternalServerError
		status := "failed"
		if k8serrors.IsNotFound(err) {
			statusCode = http.StatusNotFound
			status = "not_found"
		}
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      status,
			DryRun:      false,
			Summary:     fmt.Sprintf("failed to load Argo Application %s/%s", namespace, target),
			Warnings:    []string{err.Error()},
		}, statusCode
	}

	propagation := metav1.DeletePropagationOrphan
	if strings.EqualFold(strings.TrimSpace(req.Args["cascade"]), "true") {
		propagation = metav1.DeletePropagationForeground
	}
	if err := appResource.Delete(ctx, target, metav1.DeleteOptions{PropagationPolicy: &propagation}); err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "failed",
			DryRun:      false,
			Summary:     fmt.Sprintf("failed to retire Argo Application %s/%s", namespace, target),
			Warnings:    []string{err.Error()},
		}, http.StatusInternalServerError
	}

	syncStatus, _, _ := unstructured.NestedString(app.Object, "status", "sync", "status")
	healthStatus, _, _ := unstructured.NestedString(app.Object, "status", "health", "status")
	currentRevision, _, _ := unstructured.NestedString(app.Object, "status", "sync", "revision")
	return operatorOperationResponse{
		OperationID: operationID,
		Operation:   operation,
		Status:      "submitted",
		DryRun:      false,
		Summary:     fmt.Sprintf("retired Argo Application %s/%s through Enclii", namespace, target),
		Data: map[string]any{
			"application":      target,
			"namespace":        namespace,
			"previousSync":     syncStatus,
			"previousHealth":   healthStatus,
			"previousRevision": currentRevision,
			"propagation":      string(propagation),
		},
		Steps: []operatorOperationStep{
			{Name: "authorize", Status: "completed", Detail: "reason supplied and caller passed endpoint authorization"},
			{Name: "load-state", Status: "completed", Detail: "loaded Argo Application from cluster"},
			{Name: "diff", Status: "completed", Detail: "retire target selected; resources orphaned unless cascade=true"},
			{Name: "audit", Status: "completed", Detail: "operation executed through Enclii API audit path"},
		},
		Next: []string{
			"poll ops.apps.status for the successor application",
			"confirm shared resource warnings disappear",
			"only use cascade=true for reviewed destructive retirements",
		},
	}, http.StatusAccepted
}

func (h *Handler) handleOpsJobsTriggerApply(ctx context.Context, operation string, req operatorOperationRequest) (operatorOperationResponse, int) {
	operationID := fmt.Sprintf("op_%d", time.Now().UTC().UnixNano())
	namespace := operationNamespace(req, "default")
	target := operationTarget(req)
	if target == "" {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "invalid_request",
			DryRun:      false,
			Summary:     "jobs.trigger requires a target CronJob name",
			Warnings:    []string{"missing args.target or scope.target"},
		}, http.StatusBadRequest
	}

	kubeClient := h.opsKubeClient()
	cronJob, err := kubeClient.BatchV1().CronJobs(namespace).Get(ctx, target, metav1.GetOptions{})
	if err != nil {
		statusCode := http.StatusInternalServerError
		status := "failed"
		if k8serrors.IsNotFound(err) {
			statusCode = http.StatusNotFound
			status = "not_found"
		}
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      status,
			DryRun:      false,
			Summary:     fmt.Sprintf("failed to load CronJob %s/%s", namespace, target),
			Warnings:    []string{err.Error()},
		}, statusCode
	}

	now := time.Now().UTC()
	jobName := manualCronJobRunName(target, now)
	labels := copyStringMap(cronJob.Spec.JobTemplate.Labels)
	labels["app.kubernetes.io/managed-by"] = "enclii-ops"
	labels["enclii.dev/source-cronjob"] = target
	annotations := copyStringMap(cronJob.Spec.JobTemplate.Annotations)
	annotations["enclii.dev/last-ops-operation"] = operation
	annotations["enclii.dev/last-ops-reason"] = req.Reason
	annotations["enclii.dev/last-ops-requested"] = now.Format(time.RFC3339)
	if req.IdempotencyKey != "" {
		annotations["enclii.dev/last-ops-idempotency-key"] = req.IdempotencyKey
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        jobName,
			Namespace:   namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: *cronJob.Spec.JobTemplate.Spec.DeepCopy(),
	}

	created, err := kubeClient.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		statusCode := http.StatusInternalServerError
		status := "failed"
		if k8serrors.IsAlreadyExists(err) {
			statusCode = http.StatusConflict
			status = "already_exists"
		}
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      status,
			DryRun:      false,
			Summary:     fmt.Sprintf("failed to create one-off Job from CronJob %s/%s", namespace, target),
			Warnings:    []string{err.Error()},
		}, statusCode
	}

	warnings := []string{}
	suspended := cronJob.Spec.Suspend != nil && *cronJob.Spec.Suspend
	if suspended {
		warnings = append(warnings, "source CronJob is suspended; manual trigger was still submitted from its template")
	}
	return operatorOperationResponse{
		OperationID: operationID,
		Operation:   operation,
		Status:      "submitted",
		DryRun:      false,
		Summary:     fmt.Sprintf("created Job %s/%s from CronJob %s/%s through Enclii", namespace, created.Name, namespace, target),
		Data: map[string]any{
			"namespace":       namespace,
			"cronJob":         target,
			"job":             created.Name,
			"resourceVersion": created.ResourceVersion,
			"suspended":       suspended,
		},
		Steps: []operatorOperationStep{
			{Name: "authorize", Status: "completed", Detail: "reason supplied and caller passed endpoint authorization"},
			{Name: "load-state", Status: "completed", Detail: "loaded CronJob from cluster"},
			{Name: "diff", Status: "completed", Detail: "preserved existing CronJob jobTemplate without editing production workloads"},
			{Name: "audit", Status: "completed", Detail: "created Job with Enclii operation annotations"},
		},
		Warnings: warnings,
		Next: []string{
			"poll ops.pods.diagnose or ops.jobs.list for execution status",
			"inspect job pod logs through ops.pods.logs if the run fails",
		},
	}, http.StatusAccepted
}

func operationSupported(domain, action string, capabilities []operatorCapability) bool {
	for _, capability := range capabilities {
		if capability.Name != domain {
			continue
		}
		for _, supportedAction := range capability.Actions {
			if supportedAction == action {
				return true
			}
		}
	}
	return false
}

func manualCronJobRunName(cronJobName string, now time.Time) string {
	const separator = "-manual-"
	suffix := fmt.Sprintf("%d", now.UnixNano())
	maxPrefix := 63 - len(separator) - len(suffix)
	prefix := cronJobName
	if len(prefix) > maxPrefix {
		prefix = strings.TrimRight(prefix[:maxPrefix], "-")
	}
	if prefix == "" {
		prefix = "cronjob"
	}
	return prefix + separator + suffix
}

func copyStringMap(src map[string]string) map[string]string {
	out := make(map[string]string, len(src)+4)
	for key, value := range src {
		out[key] = value
	}
	return out
}

func (h *Handler) opsKubeClient() kubernetes.Interface {
	if h == nil || h.k8sClient == nil {
		return nil
	}
	if h.k8sClient.KubeClient != nil {
		return h.k8sClient.KubeClient
	}
	if h.k8sClient.Clientset != nil {
		return h.k8sClient.Clientset
	}
	return nil
}

var argoApplicationGVR = schema.GroupVersionResource{
	Group:    "argoproj.io",
	Version:  "v1alpha1",
	Resource: "applications",
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item != "" {
			items = append(items, item)
		}
	}
	return items
}
