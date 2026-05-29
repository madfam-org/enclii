package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// gaLonghornStorageClasses mirrors infra/helm/longhorn/storageclass.yaml plus the
// default longhorn class documented in docs/infrastructure/STORAGE.md.
var gaLonghornStorageClasses = []storagev1.StorageClass{
	{
		ObjectMeta: metav1.ObjectMeta{
			Name: "longhorn",
			Annotations: map[string]string{
				"storageclass.kubernetes.io/is-default-class": "true",
			},
		},
		Provisioner:          "driver.longhorn.io",
		AllowVolumeExpansion: boolPtr(true),
		ReclaimPolicy:        reclaimPolicyPtr(corev1.PersistentVolumeReclaimDelete),
		VolumeBindingMode:    volumeBindingModePtr(storagev1.VolumeBindingImmediate),
		Parameters: map[string]string{
			"numberOfReplicas":    "1",
			"staleReplicaTimeout": "2880",
			"fromBackup":          "",
			"fsType":              "ext4",
		},
	},
	{
		ObjectMeta: metav1.ObjectMeta{
			Name: "longhorn-replicated",
			Annotations: map[string]string{
				"storageclass.kubernetes.io/is-default-class": "false",
			},
		},
		Provisioner:          "driver.longhorn.io",
		AllowVolumeExpansion: boolPtr(true),
		ReclaimPolicy:        reclaimPolicyPtr(corev1.PersistentVolumeReclaimRetain),
		VolumeBindingMode:    volumeBindingModePtr(storagev1.VolumeBindingImmediate),
		Parameters: map[string]string{
			"numberOfReplicas":    "1",
			"staleReplicaTimeout": "30",
			"fromBackup":          "",
			"fsType":              "ext4",
			"dataLocality":        "disabled",
		},
	},
	{
		ObjectMeta: metav1.ObjectMeta{
			Name: "longhorn-fast",
			Annotations: map[string]string{
				"storageclass.kubernetes.io/is-default-class": "false",
			},
		},
		Provisioner:          "driver.longhorn.io",
		AllowVolumeExpansion: boolPtr(true),
		ReclaimPolicy:        reclaimPolicyPtr(corev1.PersistentVolumeReclaimDelete),
		VolumeBindingMode:    volumeBindingModePtr(storagev1.VolumeBindingImmediate),
		Parameters: map[string]string{
			"numberOfReplicas":    "1",
			"staleReplicaTimeout": "30",
			"dataLocality":        "strict",
		},
	},
}

type storageClassPlanItem struct {
	Name   string `json:"name"`
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
}

func boolPtr(v bool) *bool { return &v }
func reclaimPolicyPtr(p corev1.PersistentVolumeReclaimPolicy) *corev1.PersistentVolumeReclaimPolicy {
	return &p
}
func volumeBindingModePtr(m storagev1.VolumeBindingMode) *storagev1.VolumeBindingMode {
	return &m
}

func desiredStorageClasses(target string) []storagev1.StorageClass {
	if target == "" {
		return gaLonghornStorageClasses
	}
	out := make([]storagev1.StorageClass, 0, 1)
	for _, sc := range gaLonghornStorageClasses {
		if sc.Name == target {
			out = append(out, sc)
		}
	}
	return out
}

func planStorageClassChanges(existing map[string]*storagev1.StorageClass, desired []storagev1.StorageClass) []storageClassPlanItem {
	items := make([]storageClassPlanItem, 0, len(desired))
	for _, want := range desired {
		current, ok := existing[want.Name]
		if !ok {
			items = append(items, storageClassPlanItem{
				Name:   want.Name,
				Action: "create",
				Reason: "StorageClass missing in cluster",
			})
			continue
		}
		if current.Provisioner != want.Provisioner {
			items = append(items, storageClassPlanItem{
				Name:   want.Name,
				Action: "skip",
				Reason: fmt.Sprintf("existing provisioner %q differs; manual reconcile required", current.Provisioner),
			})
			continue
		}
		items = append(items, storageClassPlanItem{
			Name:   want.Name,
			Action: "skip",
			Reason: "already present",
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items
}

func (h *Handler) planLonghornStorageClasses(ctx context.Context, req operatorOperationRequest) ([]storageClassPlanItem, error) {
	client := h.opsKubeClient()
	if client == nil {
		return nil, fmt.Errorf("kubernetes client is not configured")
	}
	list, err := client.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	existing := make(map[string]*storagev1.StorageClass, len(list.Items))
	for i := range list.Items {
		existing[list.Items[i].Name] = &list.Items[i]
	}
	return planStorageClassChanges(existing, desiredStorageClasses(operationTarget(req))), nil
}

func (h *Handler) handleOpsStorageStorageClassApplyDryRun(ctx context.Context, operation string, req operatorOperationRequest) operatorOperationResponse {
	operationID := fmt.Sprintf("op_%d", time.Now().UTC().UnixNano())
	plan, err := h.planLonghornStorageClasses(ctx, req)
	if err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "failed",
			DryRun:      true,
			Summary:     "failed to plan Longhorn StorageClasses",
			Warnings:    []string{err.Error()},
		}
	}
	createCount := 0
	for _, item := range plan {
		if item.Action == "create" {
			createCount++
		}
	}
	status := "ready_to_apply"
	summary := fmt.Sprintf("plan to create %d Longhorn StorageClass(es)", createCount)
	if createCount == 0 {
		status = "succeeded"
		summary = "Longhorn StorageClasses already present"
	}
	return operatorOperationResponse{
		OperationID: operationID,
		Operation:   operation,
		Status:      status,
		DryRun:      true,
		Summary:     summary,
		Data: map[string]any{
			"plan":  plan,
			"count": createCount,
		},
		Next: []string{
			"enclii ops storage storageclass-apply --apply --reason \"Commercial GA Longhorn StorageClass reconcile\"",
		},
	}
}

func (h *Handler) handleOpsStorageStorageClassApply(ctx context.Context, operation string, req operatorOperationRequest) (operatorOperationResponse, int) {
	operationID := fmt.Sprintf("op_%d", time.Now().UTC().UnixNano())
	client := h.opsKubeClient()
	if client == nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "adapter_unconfigured",
			DryRun:      false,
			Summary:     "kubernetes client is not configured on switchyard-api",
		}, http.StatusServiceUnavailable
	}

	plan, err := h.planLonghornStorageClasses(ctx, req)
	if err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "failed",
			DryRun:      false,
			Summary:     "failed to plan Longhorn StorageClasses",
			Warnings:    []string{err.Error()},
		}, http.StatusInternalServerError
	}

	created := make([]string, 0)
	skipped := make([]string, 0)
	warnings := make([]string, 0)
	desiredByName := map[string]storagev1.StorageClass{}
	for _, sc := range desiredStorageClasses(operationTarget(req)) {
		desiredByName[sc.Name] = sc
	}

	for _, item := range plan {
		switch item.Action {
		case "create":
			want := desiredByName[item.Name]
			_, err := client.StorageV1().StorageClasses().Create(ctx, &want, metav1.CreateOptions{})
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: create failed: %v", item.Name, err))
				skipped = append(skipped, item.Name)
				continue
			}
			created = append(created, item.Name)
		default:
			skipped = append(skipped, item.Name)
		}
	}

	status := "submitted"
	summary := fmt.Sprintf("created %d Longhorn StorageClass(es) through Enclii", len(created))
	code := http.StatusAccepted
	if len(created) == 0 && len(warnings) > 0 {
		status = "failed"
		summary = "no StorageClasses were created"
		code = http.StatusInternalServerError
	} else if len(created) == 0 {
		status = "succeeded"
		summary = "Longhorn StorageClasses already present"
		code = http.StatusOK
	}

	return operatorOperationResponse{
		OperationID: operationID,
		Operation:   operation,
		Status:      status,
		DryRun:      false,
		Summary:     summary,
		Data: map[string]any{
			"created": created,
			"skipped": skipped,
		},
		Warnings: warnings,
	}, code
}
