package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var longhornVolumeGVR = schema.GroupVersionResource{
	Group:    "longhorn.io",
	Version:  "v1beta2",
	Resource: "volumes",
}

type longhornVolumeRef struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

func longhornVolumeState(obj *unstructured.Unstructured) string {
	if obj == nil {
		return ""
	}
	state, _, _ := unstructured.NestedString(obj.Object, "status", "state")
	return strings.ToLower(strings.TrimSpace(state))
}

func (h *Handler) listLonghornVolumes(ctx context.Context) ([]longhornVolumeRef, error) {
	if h.k8sClient == nil || h.k8sClient.DynamicClient == nil {
		return nil, fmt.Errorf("kubernetes dynamic client is not configured")
	}
	resource := h.k8sClient.DynamicClient.Resource(longhornVolumeGVR).Namespace(longhornSystemNamespace)
	list, err := resource.List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]longhornVolumeRef, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, longhornVolumeRef{
			Name:  list.Items[i].GetName(),
			State: longhornVolumeState(&list.Items[i]),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func detachedLonghornVolumeNames(volumes []longhornVolumeRef, target string) []longhornVolumeRef {
	out := make([]longhornVolumeRef, 0)
	for _, vol := range volumes {
		if vol.State != "detached" {
			continue
		}
		if target != "" && vol.Name != target {
			continue
		}
		out = append(out, vol)
	}
	return out
}

func (h *Handler) handleOpsStoragePruneDetachedDryRun(ctx context.Context, operation string, req operatorOperationRequest) operatorOperationResponse {
	operationID := fmt.Sprintf("op_%d", time.Now().UTC().UnixNano())
	volumes, err := h.listLonghornVolumes(ctx)
	if err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "failed",
			DryRun:      true,
			Summary:     "failed to list Longhorn volumes",
			Warnings:    []string{err.Error()},
		}
	}
	target := operationTarget(req)
	detached := detachedLonghornVolumeNames(volumes, target)
	status := "ready_to_apply"
	summary := fmt.Sprintf("found %d detached Longhorn volume(s) eligible for prune", len(detached))
	if len(detached) == 0 {
		status = "succeeded"
		summary = "no detached Longhorn volumes to prune"
	}
	return operatorOperationResponse{
		OperationID: operationID,
		Operation:   operation,
		Status:      status,
		DryRun:      true,
		Summary:     summary,
		Data: map[string]any{
			"namespace": longhornSystemNamespace,
			"detached":  detached,
			"total":     len(volumes),
		},
		Steps: []operatorOperationStep{
			{Name: "authorize", Status: "planned", Detail: "admin role required on apply"},
			{Name: "load-state", Status: "completed", Detail: "listed Longhorn Volume CRs"},
			{Name: "diff", Status: "completed", Detail: summary},
			{Name: "audit", Status: "planned", Detail: "write operation reason before delete"},
		},
		Next: []string{
			"enclii ops storage prune-detached --apply --reason \"Commercial GA O-4 orphan cleanup\"",
			"confirm volumes are orphaned before apply; attached volumes are never deleted",
		},
	}
}

func (h *Handler) handleOpsStoragePruneDetachedApply(ctx context.Context, operation string, req operatorOperationRequest) (operatorOperationResponse, int) {
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

	volumes, err := h.listLonghornVolumes(ctx)
	if err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "failed",
			DryRun:      false,
			Summary:     "failed to list Longhorn volumes",
			Warnings:    []string{err.Error()},
		}, http.StatusInternalServerError
	}

	target := operationTarget(req)
	detached := detachedLonghornVolumeNames(volumes, target)
	resource := h.k8sClient.DynamicClient.Resource(longhornVolumeGVR).Namespace(longhornSystemNamespace)
	deleted := make([]string, 0, len(detached))
	skipped := make([]string, 0)
	warnings := []string{}

	for _, vol := range detached {
		current, err := resource.Get(ctx, vol.Name, metav1.GetOptions{})
		if err != nil {
			if k8serrors.IsNotFound(err) {
				skipped = append(skipped, vol.Name)
				continue
			}
			warnings = append(warnings, fmt.Sprintf("%s: get failed: %v", vol.Name, err))
			continue
		}
		if longhornVolumeState(current) != "detached" {
			skipped = append(skipped, vol.Name)
			warnings = append(warnings, fmt.Sprintf("%s: state changed to %q; skipped", vol.Name, longhornVolumeState(current)))
			continue
		}
		if err := resource.Delete(ctx, vol.Name, metav1.DeleteOptions{}); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: delete failed: %v", vol.Name, err))
			continue
		}
		deleted = append(deleted, vol.Name)
	}

	status := "submitted"
	summary := fmt.Sprintf("deleted %d detached Longhorn volume(s) through Enclii", len(deleted))
	code := http.StatusAccepted
	if len(deleted) == 0 && len(warnings) > 0 {
		status = "failed"
		summary = "no detached Longhorn volumes were deleted"
		code = http.StatusInternalServerError
	} else if len(deleted) == 0 {
		status = "succeeded"
		summary = "no detached Longhorn volumes to prune"
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
			"deleted":   deleted,
			"skipped":   skipped,
		},
		Steps: []operatorOperationStep{
			{Name: "authorize", Status: "completed", Detail: "reason supplied and caller passed endpoint authorization"},
			{Name: "load-state", Status: "completed", Detail: "listed Longhorn Volume CRs"},
			{Name: "diff", Status: "completed", Detail: summary},
			{Name: "audit", Status: "completed", Detail: "deleted detached volumes only"},
		},
		Warnings: warnings,
		Next: []string{
			"verify disk usage improved with ops.storage.longhorn read",
			"do not delete attached or healthy volumes",
		},
	}, code
}
