package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

func (h *Handler) handleOpsSecretsRefreshApply(ctx context.Context, operation string, req operatorOperationRequest) (operatorOperationResponse, int) {
	operationID := fmt.Sprintf("op_%d", time.Now().UTC().UnixNano())
	namespace := operationNamespace(req, "default")
	target := operationTarget(req)
	if target == "" {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "invalid_request",
			DryRun:      false,
			Summary:     fmt.Sprintf("%s requires a target ExternalSecret name", operation),
			Warnings:    []string{"missing args.target or scope.target"},
		}, http.StatusBadRequest
	}

	externalSecrets := h.k8sClient.DynamicClient.Resource(externalSecretGVR).Namespace(namespace)
	secret, err := externalSecrets.Get(ctx, target, metav1.GetOptions{})
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
			Summary:     fmt.Sprintf("failed to load ExternalSecret %s/%s", namespace, target),
			Warnings:    []string{err.Error()},
		}, statusCode
	}

	now := time.Now().UTC()
	annotations := map[string]string{
		"force-sync":                      fmt.Sprintf("%d", now.Unix()),
		"enclii.dev/last-ops-operation":   operation,
		"enclii.dev/last-ops-reason":      req.Reason,
		"enclii.dev/last-ops-requested":   now.Format(time.RFC3339),
		"enclii.dev/refresh-requested-at": now.Format(time.RFC3339Nano),
	}
	if req.IdempotencyKey != "" {
		annotations["enclii.dev/last-ops-idempotency-key"] = req.IdempotencyKey
	}
	patch := map[string]any{
		"metadata": map[string]any{
			"annotations": annotations,
		},
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "failed",
			DryRun:      false,
			Summary:     "failed to build ExternalSecret refresh patch",
			Warnings:    []string{err.Error()},
		}, http.StatusInternalServerError
	}

	updated, err := externalSecrets.Patch(ctx, target, k8stypes.MergePatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "failed",
			DryRun:      false,
			Summary:     fmt.Sprintf("failed to refresh ExternalSecret %s/%s", namespace, target),
			Warnings:    []string{err.Error()},
		}, http.StatusInternalServerError
	}

	conditions, _, _ := unstructured.NestedSlice(secret.Object, "status", "conditions")
	return operatorOperationResponse{
		OperationID: operationID,
		Operation:   operation,
		Status:      "submitted",
		DryRun:      false,
		Summary:     fmt.Sprintf("requested ExternalSecret sync for %s/%s through Enclii", namespace, target),
		Data: map[string]any{
			"namespace":       namespace,
			"externalSecret":  target,
			"resourceVersion": updated.GetResourceVersion(),
			"forceSync":       annotations["force-sync"],
			"conditions":      conditions,
		},
		Steps: []operatorOperationStep{
			{Name: "authorize", Status: "completed", Detail: "reason supplied and caller passed endpoint authorization"},
			{Name: "load-state", Status: "completed", Detail: "loaded ExternalSecret from cluster"},
			{Name: "diff", Status: "completed", Detail: "patched force-sync annotation without reading or writing secret values"},
			{Name: "audit", Status: "completed", Detail: "annotated ExternalSecret with operation reason and idempotency key"},
		},
		Next: []string{
			"poll ops.secrets.external until Ready=True",
			"if SecretSyncedError persists, populate the backing provider path through the approved secret-management workflow",
		},
	}, http.StatusAccepted
}

var externalSecretGVR = schema.GroupVersionResource{
	Group:    "external-secrets.io",
	Version:  "v1beta1",
	Resource: "externalsecrets",
}
