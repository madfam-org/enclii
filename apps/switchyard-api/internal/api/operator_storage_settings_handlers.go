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

const longhornSystemNamespace = "longhorn-system"

var longhornSettingGVR = schema.GroupVersionResource{
	Group:    "longhorn.io",
	Version:  "v1beta1",
	Resource: "settings",
}

// gaLonghornCPUSettings mirrors infra/helm/longhorn/values.yaml defaultSettings
// (Commercial GA O-5 — instance-managers should stay under 200m CPU each).
var gaLonghornCPUSettings = []longhornSettingSpec{
	{Name: "guaranteed-engine-manager-cpu", Value: "3"},
	{Name: "guaranteed-replica-manager-cpu", Value: "3"},
	{Name: "guaranteed-instance-manager-cpu", Value: "3"},
}

type longhornSettingSpec struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type longhornSettingChange struct {
	Name    string `json:"name"`
	Current string `json:"current,omitempty"`
	Target  string `json:"target"`
	Apply   bool   `json:"apply"`
	Reason  string `json:"reason,omitempty"`
}

func longhornSettingValue(obj *unstructured.Unstructured) string {
	if obj == nil {
		return ""
	}
	if v, ok, _ := unstructured.NestedString(obj.Object, "value"); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func planLonghornSettingChanges(existing map[string]string, targets []longhornSettingSpec) []longhornSettingChange {
	changes := make([]longhornSettingChange, 0, len(targets))
	for _, target := range targets {
		current, seen := existing[target.Name]
		change := longhornSettingChange{
			Name:   target.Name,
			Target: target.Value,
		}
		if !seen {
			change.Reason = "setting not present in cluster (skipped)"
			change.Apply = false
		} else {
			change.Current = current
			if current == target.Value {
				change.Reason = "already at target"
				change.Apply = false
			} else {
				change.Apply = true
			}
		}
		changes = append(changes, change)
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Name < changes[j].Name })
	return changes
}

func (h *Handler) loadLonghornSettingValues(ctx context.Context) (map[string]string, error) {
	if h.k8sClient == nil || h.k8sClient.DynamicClient == nil {
		return nil, fmt.Errorf("kubernetes dynamic client is not configured")
	}
	resource := h.k8sClient.DynamicClient.Resource(longhornSettingGVR).Namespace(longhornSystemNamespace)
	list, err := resource.List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(list.Items))
	for i := range list.Items {
		name := list.Items[i].GetName()
		out[name] = longhornSettingValue(&list.Items[i])
	}
	return out, nil
}

func (h *Handler) handleOpsStorageSettingsApplyDryRun(ctx context.Context, operation string, req operatorOperationRequest) operatorOperationResponse {
	operationID := fmt.Sprintf("op_%d", time.Now().UTC().UnixNano())
	existing, err := h.loadLonghornSettingValues(ctx)
	if err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "failed",
			DryRun:      true,
			Summary:     "failed to read Longhorn settings",
			Warnings:    []string{err.Error()},
		}
	}
	changes := planLonghornSettingChanges(existing, gaLonghornCPUSettings)
	toApply := 0
	for _, c := range changes {
		if c.Apply {
			toApply++
		}
	}
	status := "ready_to_apply"
	summary := fmt.Sprintf("Longhorn CPU settings plan: %d change(s) pending", toApply)
	if toApply == 0 {
		status = "succeeded"
		summary = "Longhorn CPU settings already match helm values (no changes needed)"
	}
	return operatorOperationResponse{
		OperationID: operationID,
		Operation:   operation,
		Status:      status,
		DryRun:      true,
		Summary:     summary,
		Data: map[string]any{
			"namespace": longhornSystemNamespace,
			"changes":   changes,
			"source":    "infra/helm/longhorn/values.yaml defaultSettings",
		},
		Steps: []operatorOperationStep{
			{Name: "authorize", Status: "planned", Detail: "admin role required on apply"},
			{Name: "load-state", Status: "completed", Detail: "read Longhorn Setting CRs"},
			{Name: "diff", Status: "completed", Detail: summary},
			{Name: "audit", Status: "planned", Detail: "write operation reason before patch"},
		},
		Next: []string{
			"enclii ops storage settings-apply --apply --reason \"Commercial GA O-5 Longhorn CPU\"",
			"verify instance-manager pods stay under 200m CPU after Longhorn reconciles",
		},
	}
}

func (h *Handler) handleOpsStorageSettingsApply(ctx context.Context, operation string, req operatorOperationRequest) (operatorOperationResponse, int) {
	operationID := fmt.Sprintf("op_%d", time.Now().UTC().UnixNano())
	if h.k8sClient == nil || h.k8sClient.DynamicClient == nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "adapter_unconfigured",
			DryRun:      false,
			Summary:     "kubernetes dynamic client is not configured on switchyard-api",
		}, http.StatusServiceUnavailable
	}

	existing, err := h.loadLonghornSettingValues(ctx)
	if err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "failed",
			DryRun:      false,
			Summary:     "failed to read Longhorn settings",
			Warnings:    []string{err.Error()},
		}, http.StatusInternalServerError
	}

	changes := planLonghornSettingChanges(existing, gaLonghornCPUSettings)
	resource := h.k8sClient.DynamicClient.Resource(longhornSettingGVR).Namespace(longhornSystemNamespace)
	now := time.Now().UTC()
	applied := make([]longhornSettingChange, 0)
	skipped := make([]longhornSettingChange, 0)
	warnings := []string{}

	for _, change := range changes {
		if !change.Apply {
			skipped = append(skipped, change)
			continue
		}
		patch := map[string]any{
			"value": change.Target,
			"metadata": map[string]any{
				"annotations": map[string]string{
					"enclii.dev/last-ops-operation": operation,
					"enclii.dev/last-ops-reason":    req.Reason,
					"enclii.dev/last-ops-requested": now.Format(time.RFC3339),
				},
			},
		}
		if req.IdempotencyKey != "" {
			patch["metadata"].(map[string]any)["annotations"].(map[string]string)["enclii.dev/last-ops-idempotency-key"] = req.IdempotencyKey
		}
		patchBytes, err := json.Marshal(patch)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: marshal patch: %v", change.Name, err))
			continue
		}
		_, err = resource.Patch(ctx, change.Name, k8stypes.MergePatchType, patchBytes, metav1.PatchOptions{})
		if err != nil {
			if k8serrors.IsNotFound(err) {
				skipped = append(skipped, longhornSettingChange{
					Name:   change.Name,
					Target: change.Target,
					Reason: "not found during apply",
				})
				continue
			}
			warnings = append(warnings, fmt.Sprintf("%s: patch failed: %v", change.Name, err))
			continue
		}
		applied = append(applied, change)
	}

	status := "submitted"
	summary := fmt.Sprintf("patched %d Longhorn setting(s) through Enclii", len(applied))
	if len(applied) == 0 && len(warnings) > 0 {
		status = "failed"
		summary = "no Longhorn settings were patched"
	} else if len(applied) == 0 {
		status = "succeeded"
		summary = "Longhorn CPU settings already at target"
	}

	code := http.StatusAccepted
	if status == "failed" {
		code = http.StatusInternalServerError
	} else if status == "succeeded" {
		code = http.StatusOK
	}

	return operatorOperationResponse{
		OperationID: operationID,
		Operation:   operation,
		Status:      status,
		DryRun:      false,
		Summary:     summary,
		Data: map[string]any{
			"namespace": longhornSystemNamespace,
			"applied":   applied,
			"skipped":   skipped,
		},
		Steps: []operatorOperationStep{
			{Name: "authorize", Status: "completed", Detail: "reason supplied and caller passed endpoint authorization"},
			{Name: "load-state", Status: "completed", Detail: "read Longhorn Setting CRs"},
			{Name: "diff", Status: "completed", Detail: summary},
			{Name: "audit", Status: "completed", Detail: "patched Setting.value with Enclii operation annotations"},
		},
		Warnings: warnings,
		Next: []string{
			"poll ops.storage.longhorn and node CPU metrics",
			"confirm instance-manager pods drop below 200m CPU",
		},
	}, code
}
