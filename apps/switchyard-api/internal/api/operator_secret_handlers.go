package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
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

func (h *Handler) handleOpsSecretsRotateApply(ctx context.Context, operation string, req operatorOperationRequest) (operatorOperationResponse, int) {
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

	providerVersion := strings.TrimSpace(req.Args["provider_version"])
	if providerVersion == "" {
		providerVersion = strings.TrimSpace(req.Args["provider-version"])
	}
	now := time.Now().UTC()
	annotations := map[string]string{
		"force-sync":                            fmt.Sprintf("%d", now.Unix()),
		"enclii.dev/last-ops-operation":         operation,
		"enclii.dev/last-ops-reason":            req.Reason,
		"enclii.dev/last-ops-requested":         now.Format(time.RFC3339),
		"enclii.dev/refresh-requested-at":       now.Format(time.RFC3339Nano),
		"enclii.dev/rotation-mode":              "eso-cutover",
		"enclii.dev/rotation-phase":             "cutover-requested",
		"enclii.dev/rotation-requested-at":      now.Format(time.RFC3339Nano),
		"enclii.dev/rotation-provider-value":    "pre-staged",
		"enclii.dev/rotation-old-value-revoked": "pending-verification",
	}
	if providerVersion != "" {
		annotations["enclii.dev/rotation-provider-version"] = providerVersion
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
			Summary:     "failed to build ExternalSecret rotation patch",
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
			Summary:     fmt.Sprintf("failed to request ExternalSecret rotation for %s/%s", namespace, target),
			Warnings:    []string{err.Error()},
		}, http.StatusInternalServerError
	}

	warnings := []string{
		"this operation does not write secret values; the backing provider value must be staged before apply",
		"old provider value revocation remains a post-verification follow-up",
	}
	if providerVersion == "" {
		warnings = append(warnings, "provider_version was not supplied; audit metadata records the cutover without a provider version marker")
	}

	conditions, _, _ := unstructured.NestedSlice(secret.Object, "status", "conditions")
	return operatorOperationResponse{
		OperationID: operationID,
		Operation:   operation,
		Status:      "submitted",
		DryRun:      false,
		Summary:     fmt.Sprintf("requested ExternalSecret rotation cutover for %s/%s through Enclii", namespace, target),
		Data: map[string]any{
			"namespace":       namespace,
			"externalSecret":  target,
			"resourceVersion": updated.GetResourceVersion(),
			"forceSync":       annotations["force-sync"],
			"rotationMode":    annotations["enclii.dev/rotation-mode"],
			"rotationPhase":   annotations["enclii.dev/rotation-phase"],
			"providerVersion": providerVersion,
			"conditions":      conditions,
		},
		Steps: []operatorOperationStep{
			{Name: "authorize", Status: "completed", Detail: "reason supplied and caller passed endpoint authorization"},
			{Name: "load-state", Status: "completed", Detail: "loaded ExternalSecret from cluster"},
			{Name: "diff", Status: "completed", Detail: "patched rotation cutover annotations and force-sync without reading or writing secret values"},
			{Name: "audit", Status: "completed", Detail: "annotated ExternalSecret with operation reason, idempotency key, and rotation phase"},
		},
		Warnings: warnings,
		Next: []string{
			"poll ops.secrets.external until Ready=True and SecretSynced is current",
			"restart or reload consumers through Enclii if they do not pick up the refreshed Secret automatically",
			"revoke the old provider value only after consumer verification succeeds",
		},
	}, http.StatusAccepted
}

func (h *Handler) handleOpsSecretsVaultBackfillApply(ctx context.Context, operation string, req operatorOperationRequest) (operatorOperationResponse, int) {
	operationID := fmt.Sprintf("op_%d", time.Now().UTC().UnixNano())
	namespace := operationNamespace(req, "default")
	sourceSecret := operationTarget(req)
	vaultPath := operationArg(req, "vault_path", "vault-path")
	externalSecret := operationArg(req, "external_secret", "external-secret")
	if sourceSecret == "" || vaultPath == "" {
		warnings := []string{}
		if sourceSecret == "" {
			warnings = append(warnings, "missing args.target or scope.target")
		}
		if vaultPath == "" {
			warnings = append(warnings, "missing args.vault_path")
		}
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "invalid_request",
			DryRun:      false,
			Summary:     fmt.Sprintf("%s requires a source Kubernetes Secret and Vault path", operation),
			Warnings:    warnings,
		}, http.StatusBadRequest
	}

	secret, err := h.opsKubeClient().CoreV1().Secrets(namespace).Get(ctx, sourceSecret, metav1.GetOptions{})
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
			Summary:     fmt.Sprintf("failed to load Kubernetes Secret %s/%s", namespace, sourceSecret),
			Warnings:    []string{err.Error()},
		}, statusCode
	}

	existing, err := h.vaultClient.GetSecretData(ctx, vaultPath)
	if err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "failed",
			DryRun:      false,
			Summary:     fmt.Sprintf("failed to read Vault %s before merging; the writer policy must grant read on this path", vaultPath),
			Warnings:    []string{err.Error()},
		}, http.StatusInternalServerError
	}

	overwrite := vaultBackfillOverwriteRequested(req)
	plan, err := buildVaultBackfillPlan(secret.Data, existing, overwrite)
	if err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "invalid_request",
			DryRun:      false,
			Summary:     fmt.Sprintf("failed to prepare Vault updates from %s/%s", namespace, sourceSecret),
			Warnings:    []string{err.Error()},
		}, http.StatusBadRequest
	}
	keys := make([]string, 0, len(plan.Keys))
	for _, row := range plan.Keys {
		keys = append(keys, row.VaultProperty)
	}
	sort.Strings(keys)

	// A differing value means Vault already holds something else for this
	// property. Replacing it silently could undo a rotation done through Vault,
	// so the apply refuses and names the keys; --allow-overwrite is the explicit
	// opt-in. Nothing is written in this case.
	if len(plan.DifferingKeys) > 0 && !overwrite {
		data := plan.data()
		data["namespace"] = namespace
		data["sourceSecret"] = sourceSecret
		data["vaultPath"] = vaultPath
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "conflict",
			DryRun:      false,
			Summary: fmt.Sprintf("refused: %d key(s) in %s/%s differ from the value already in Vault %s; nothing was written",
				len(plan.DifferingKeys), namespace, sourceSecret, vaultPath),
			Data:     data,
			Warnings: []string{fmt.Sprintf("differing keys: %s", strings.Join(plan.DifferingKeys, ", "))},
			Next: []string{
				"confirm which value is current; rerun with --allow-overwrite only if the Kubernetes value is the one to keep",
			},
		}, http.StatusConflict
	}

	// Idempotent re-runs: when every key already matches, no new Vault version
	// is written. The ExternalSecret refresh below still runs if requested.
	vaultVersion := 0
	vaultWritten := false
	if len(plan.writes) > 0 {
		vaultVersion, err = h.vaultClient.MergeSecretData(ctx, vaultPath, plan.writes)
		if err != nil {
			return operatorOperationResponse{
				OperationID: operationID,
				Operation:   operation,
				Status:      "failed",
				DryRun:      false,
				Summary:     fmt.Sprintf("failed to merge %s/%s into Vault %s", namespace, sourceSecret, vaultPath),
				Warnings:    []string{err.Error()},
			}, http.StatusInternalServerError
		}
		vaultWritten = true
	}

	now := time.Now().UTC()
	warnings := []string{
		"secret values were read from the Kubernetes source Secret and compared or written server-side; values are omitted from the response",
		"source Kubernetes Secret cleanup remains a follow-up after ESO verification succeeds",
	}
	if !vaultWritten {
		warnings = append(warnings, "every key already matched Vault; no new Vault version was written")
	}
	externalSecretRefreshed := false
	externalSecretResourceVersion := ""
	if externalSecret != "" {
		if h.k8sClient == nil || h.k8sClient.DynamicClient == nil {
			warnings = append(warnings, "ExternalSecret refresh skipped because the dynamic Kubernetes client is not configured")
		} else {
			resourceVersion, refreshErr := h.patchExternalSecretOpsAnnotations(ctx, namespace, externalSecret, map[string]string{
				"force-sync":                          fmt.Sprintf("%d", now.Unix()),
				"enclii.dev/last-ops-operation":       operation,
				"enclii.dev/last-ops-reason":          req.Reason,
				"enclii.dev/last-ops-requested":       now.Format(time.RFC3339),
				"enclii.dev/refresh-requested-at":     now.Format(time.RFC3339Nano),
				"enclii.dev/vault-backfill":           "completed",
				"enclii.dev/vault-backfill-source":    sourceSecret,
				"enclii.dev/vault-backfill-requested": now.Format(time.RFC3339Nano),
			}, req.IdempotencyKey)
			if refreshErr != nil {
				warnings = append(warnings, fmt.Sprintf("ExternalSecret refresh skipped: %s", refreshErr.Error()))
			} else {
				externalSecretRefreshed = true
				externalSecretResourceVersion = resourceVersion
			}
		}
	}

	data := plan.data()
	for k, v := range map[string]any{
		"namespace":                     namespace,
		"sourceSecret":                  sourceSecret,
		"vaultPath":                     vaultPath,
		"vaultVersion":                  vaultVersion,
		"vaultWritten":                  vaultWritten,
		"normalizedKeys":                keys,
		"keyCount":                      len(keys),
		"externalSecret":                externalSecret,
		"externalSecretRefreshed":       externalSecretRefreshed,
		"externalSecretResourceVersion": externalSecretResourceVersion,
	} {
		data[k] = v
	}
	status := "submitted"
	summary := fmt.Sprintf("merged %d key(s) from %s/%s into Vault through Enclii (%d new, %d unchanged, %d overwritten)",
		len(plan.writes), namespace, sourceSecret, len(plan.NewKeys), len(plan.UnchangedKeys), len(plan.writes)-len(plan.NewKeys))
	if !vaultWritten {
		status = "unchanged"
		summary = fmt.Sprintf("all %d key(s) from %s/%s already match Vault %s; nothing written", len(keys), namespace, sourceSecret, vaultPath)
	}

	return operatorOperationResponse{
		OperationID: operationID,
		Operation:   operation,
		Status:      status,
		DryRun:      false,
		Summary:     summary,
		Data:        data,
		Steps: []operatorOperationStep{
			{Name: "authorize", Status: "completed", Detail: "reason supplied and caller passed endpoint authorization"},
			{Name: "load-state", Status: "completed", Detail: "loaded the Kubernetes source Secret and the Vault path"},
			{Name: "diff", Status: "completed", Detail: "classified each key as new, unchanged or differing without exposing values"},
			{Name: "vault-merge", Status: stepStatus(!vaultWritten, vaultWritten), Detail: "merged new (and, with --allow-overwrite, differing) keys into Vault KV v2"},
			{Name: "eso-refresh", Status: stepStatus(externalSecret == "", externalSecretRefreshed), Detail: "requested ExternalSecret force-sync when a target was supplied"},
			{Name: "audit", Status: "completed", Detail: "operation reason and idempotency metadata recorded on refreshed ExternalSecret when available"},
		},
		Warnings: warnings,
		Next: []string{
			"poll ops.secrets.external until Ready=True and SecretSynced is current",
			"switch consumers to the Vault-backed ExternalSecret once verification succeeds",
			"remove bridge/source Kubernetes Secret material only after the Vault-backed path is stable",
		},
	}, http.StatusAccepted
}

func (h *Handler) patchExternalSecretOpsAnnotations(ctx context.Context, namespace, name string, annotations map[string]string, idempotencyKey string) (string, error) {
	if idempotencyKey != "" {
		annotations["enclii.dev/last-ops-idempotency-key"] = idempotencyKey
	}
	patch := map[string]any{
		"metadata": map[string]any{
			"annotations": annotations,
		},
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return "", fmt.Errorf("build ExternalSecret patch: %w", err)
	}
	updated, err := h.k8sClient.DynamicClient.Resource(externalSecretGVR).Namespace(namespace).Patch(ctx, name, k8stypes.MergePatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		return "", fmt.Errorf("patch ExternalSecret %s/%s: %w", namespace, name, err)
	}
	return updated.GetResourceVersion(), nil
}

func stepStatus(skipped, completed bool) string {
	if skipped {
		return "skipped"
	}
	if completed {
		return "completed"
	}
	return "warning"
}

func normalizeVaultSecretKey(key string) string {
	var b strings.Builder
	lastUnderscore := true
	for i := 0; i < len(key); i++ {
		ch := key[i]
		if ch >= 'A' && ch <= 'Z' {
			ch += 'a' - 'A'
		}
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			b.WriteByte(ch)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

var externalSecretGVR = schema.GroupVersionResource{
	Group:    "external-secrets.io",
	Version:  "v1beta1",
	Resource: "externalsecrets",
}
