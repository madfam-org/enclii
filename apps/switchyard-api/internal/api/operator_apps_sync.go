package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	if req.validateApplication != nil && req.validateApplication(app) != nil {
		return operatorOperationResponse{OperationID: operationID, Operation: operation,
			Status: "conflict", Summary: "Application release binding changed; no operation submitted"}, http.StatusConflict
	}

	if activeOperation, found, readErr := unstructured.NestedMap(app.Object, "operation"); readErr != nil || (found && len(activeOperation) > 0) {
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
	previousState, hasState, stateErr := unstructured.NestedMap(app.Object, "status", "operationState")
	phase, hasPhase, phaseErr := unstructured.NestedString(previousState, "phase")
	if stateErr != nil || (hasState && (phaseErr != nil || !hasPhase || (phase != "Succeeded" && phase != "Failed" && phase != "Error"))) {
		return operatorOperationResponse{
			OperationID: operationID, Operation: operation, Status: "conflict", DryRun: false,
			Summary:  "Application operation state is active or unconfirmed; no operation submitted",
			Warnings: []string{"inspect the current Application operation state before retrying"},
		}, http.StatusConflict
	}

	revision := strings.TrimSpace(req.Args["revision"])
	prune := true
	if strings.EqualFold(strings.TrimSpace(req.Args["prune"]), "false") {
		prune = false
	}
	syncOptions, err := appsSyncOptions(app, req.Args["sync_options"])
	if err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "failed",
			Summary:     fmt.Sprintf("failed to read sync options for Argo Application %s/%s; no operation submitted", namespace, target),
			Warnings:    []string{err.Error()},
		}, http.StatusInternalServerError
	}

	optionValues := make([]any, len(syncOptions))
	for i, option := range syncOptions {
		optionValues[i] = option
	}
	syncSpec := map[string]any{
		"prune":       prune,
		"syncOptions": optionValues,
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
	for key, value := range req.auditAnnotations {
		annotations[key] = value
	}
	// Replace the complete operation on the version we just inspected. A merge
	// patch can inherit a concurrent auto-sync's revision/resources/automated
	// fields; Update uses metadata.resourceVersion as an atomic precondition.
	// Never retry a conflict here: the caller must inspect the new operation.
	requested := app.DeepCopy()
	// Match Argo CD v3.2.5 SetAppOperation: its controller later merge-patches
	// the new state. Retaining old state would inherit omitted selective-sync
	// resources/automated fields on that patch and contaminate resumed work.
	// Clear only the confirmed terminal state in the same versioned Update.
	unstructured.RemoveNestedField(requested.Object, "status", "operationState")
	requested.Object["operation"] = map[string]any{
		"initiatedBy": map[string]any{"username": "enclii-ops"},
		"sync":        syncSpec,
	}
	existingAnnotations := requested.GetAnnotations()
	if existingAnnotations == nil {
		existingAnnotations = map[string]string{}
	}
	for key, value := range annotations {
		existingAnnotations[key] = value
	}
	requested.SetAnnotations(existingAnnotations)

	updated, err := appResource.Update(ctx, requested, metav1.UpdateOptions{})
	if err != nil {
		if k8serrors.IsConflict(err) {
			return operatorOperationResponse{
				OperationID: operationID,
				Operation:   operation,
				Status:      "conflict",
				DryRun:      false,
				Summary:     "Argo Application changed before sync submission; no operation was submitted",
				Warnings:    []string{"inspect the current Application and active operation before retrying"},
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

func appsSyncOptions(app *unstructured.Unstructured, override string) ([]string, error) {
	if options := strings.TrimSpace(override); options != "" {
		return splitCSV(options), nil
	}

	// Argo consumes the operation's options, so retain the policy on the exact
	// Application version reviewed above instead of replacing it with defaults.
	options, _, err := unstructured.NestedStringSlice(app.Object, "spec", "syncPolicy", "syncOptions")
	if err != nil {
		return nil, err
	}
	for _, option := range options {
		if strings.HasPrefix(option, "PruneLast=") {
			return options, nil
		}
	}
	return append(options, "PruneLast=true"), nil
}
