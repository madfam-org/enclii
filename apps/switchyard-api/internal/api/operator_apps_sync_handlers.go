package api

import (
	"context"
	"encoding/json"
	"fmt"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"net/http"
	"strings"
	"time"
)

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
	// A merge patch can inherit a concurrent Argo self-heal's selective resource
	// list and skip hooks. Test the observed version and replace the entire
	// operation atomically; never take over or retry against a running operation.
	if app.GetResourceVersion() == "" {
		return operatorOperationResponse{
			OperationID: operationID, Operation: operation, Status: "failed",
			Summary: "Argo Application has no resourceVersion; refusing unsafe sync submission",
		}, http.StatusInternalServerError
	}
	mergedAnnotations := app.GetAnnotations()
	if mergedAnnotations == nil {
		mergedAnnotations = map[string]string{}
	}
	for key, value := range annotations {
		mergedAnnotations[key] = value
	}
	patch := []map[string]any{
		{"op": "test", "path": "/metadata/resourceVersion", "value": app.GetResourceVersion()},
		{"op": "add", "path": "/metadata/annotations", "value": mergedAnnotations},
		{"op": "add", "path": "/operation", "value": map[string]any{
			"initiatedBy": map[string]any{"username": "enclii-ops"},
			"sync":        syncSpec,
		}},
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

	updated, err := appResource.Patch(ctx, target, k8stypes.JSONPatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		// Kubernetes reports a failed JSON Patch test as Invalid (422).
		if k8serrors.IsConflict(err) || k8serrors.IsInvalid(err) {
			return operatorOperationResponse{
				OperationID: operationID, Operation: operation, Status: "state_changed",
				Summary:  fmt.Sprintf("Argo Application %s/%s changed or rejected the sync precondition", namespace, target),
				Warnings: []string{err.Error(), "refresh apps.status and retry after any active operation completes; no sync was submitted"},
			}, http.StatusConflict
		}
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
