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
//
// numberOfReplicas is 2 for the classes that hold data (longhorn,
// longhorn-replicated) and 1 only for longhorn-fast (strict-local caches). It
// was 1 everywhere from the single-node era; the cluster has 4 Longhorn nodes,
// and the chart's own default class already asks for 2
// (persistence.defaultClassReplicaCount in infra/argocd/apps/storage.yaml).
//
// StorageClass parameters are immutable, so this reconcile only CREATES
// missing classes. A live class whose replica count differs is reported as
// drift and left alone; changing it is a delete-and-recreate (see
// infra/helm/longhorn/storageclass.yaml), and existing volumes keep their own
// spec.numberOfReplicas until patched.
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
			"numberOfReplicas":    "2",
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
			"numberOfReplicas":    "2",
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

// storageClassDriftParameters are the parameters compared against a live
// class. Only the replica count: it decides whether a volume survives a node,
// and the chart-managed default class legitimately differs from this mirror
// in the others (staleReplicaTimeout, dataEngine, ...).
var storageClassDriftParameters = []string{"numberOfReplicas"}

const (
	storageClassActionCreate = "create"
	storageClassActionSkip   = "skip"
	storageClassActionDrift  = "drift"
)

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
				Action: storageClassActionCreate,
				Reason: "StorageClass missing in cluster",
			})
			continue
		}
		if current.Provisioner != want.Provisioner {
			items = append(items, storageClassPlanItem{
				Name:   want.Name,
				Action: storageClassActionSkip,
				Reason: fmt.Sprintf("existing provisioner %q differs; manual reconcile required", current.Provisioner),
			})
			continue
		}
		if reason := storageClassDriftReason(current, want); reason != "" {
			items = append(items, storageClassPlanItem{
				Name:   want.Name,
				Action: storageClassActionDrift,
				Reason: reason,
			})
			continue
		}
		items = append(items, storageClassPlanItem{
			Name:   want.Name,
			Action: storageClassActionSkip,
			Reason: "already present",
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items
}

// storageClassDriftReason explains how a live class differs from the GA
// manifest in a compared parameter, or returns "" when it does not.
func storageClassDriftReason(current *storagev1.StorageClass, want storagev1.StorageClass) string {
	for _, key := range storageClassDriftParameters {
		wantValue, ok := want.Parameters[key]
		if !ok {
			continue
		}
		liveValue, present := current.Parameters[key]
		if present && liveValue == wantValue {
			continue
		}
		if !present {
			liveValue = "(unset)"
		}
		fix := "StorageClass parameters are immutable: delete the class, then run storageclass-apply to recreate it; existing volumes keep their own replica count until patched (infra/helm/longhorn/storageclass.yaml)"
		if want.Name == "longhorn" {
			fix = "this class is created by the Longhorn chart: set persistence.defaultClassReplicaCount in infra/argocd/apps/storage.yaml; existing volumes keep their own replica count until patched"
		}
		return fmt.Sprintf("%s is %s live, %s in the GA manifest; %s", key, liveValue, wantValue, fix)
	}
	return ""
}

// countStorageClassActions counts plan items by action.
func countStorageClassActions(plan []storageClassPlanItem) (create, drift int) {
	for _, item := range plan {
		switch item.Action {
		case storageClassActionCreate:
			create++
		case storageClassActionDrift:
			drift++
		}
	}
	return create, drift
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
	createCount, driftCount := countStorageClassActions(plan)
	status := "ready_to_apply"
	summary := fmt.Sprintf("plan to create %d Longhorn StorageClass(es)", createCount)
	switch {
	case createCount == 0 && driftCount > 0:
		status = "drift_detected"
		summary = fmt.Sprintf("%d Longhorn StorageClass(es) differ from the GA manifest; apply does not change existing classes", driftCount)
	case createCount == 0:
		status = "succeeded"
		summary = "Longhorn StorageClasses already present"
	case driftCount > 0:
		summary = fmt.Sprintf("%s; %d existing class(es) differ from the GA manifest and are left alone", summary, driftCount)
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
			"drift": driftCount,
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
	drifted := make([]string, 0)
	warnings := make([]string, 0)
	createFailures := 0
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
				createFailures++
				continue
			}
			created = append(created, item.Name)
		case storageClassActionDrift:
			// Never mutated here: parameters are immutable, and recreating a
			// class is an operator decision. Report it on every apply.
			warnings = append(warnings, fmt.Sprintf("%s: drift: %s", item.Name, item.Reason))
			drifted = append(drifted, item.Name)
			skipped = append(skipped, item.Name)
		default:
			skipped = append(skipped, item.Name)
		}
	}

	status := "submitted"
	summary := fmt.Sprintf("created %d Longhorn StorageClass(es) through Enclii", len(created))
	code := http.StatusAccepted
	switch {
	case len(created) == 0 && createFailures > 0:
		status = "failed"
		summary = "no StorageClasses were created"
		code = http.StatusInternalServerError
	case len(created) == 0 && len(drifted) > 0:
		status = "drift_detected"
		summary = fmt.Sprintf("no StorageClass created; %d existing class(es) differ from the GA manifest and were left alone", len(drifted))
		code = http.StatusOK
	case len(created) == 0:
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
			"drifted": drifted,
		},
		Warnings: warnings,
	}, code
}
