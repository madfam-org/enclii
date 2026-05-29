package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const cosignVerifySignaturesLabel = "enclii.dev/verify-signatures"

var gaCosignEnforceNamespaces = []string{"enclii", "status", "monitoring"}

type cosignEnablePlanItem struct {
	Namespace string `json:"namespace"`
	Action    string `json:"action"`
	Reason    string `json:"reason,omitempty"`
}

func desiredCosignNamespaces(target string) []string {
	if target == "" {
		return gaCosignEnforceNamespaces
	}
	for _, ns := range gaCosignEnforceNamespaces {
		if ns == target {
			return []string{target}
		}
	}
	if strings.TrimSpace(target) != "" {
		return []string{target}
	}
	return gaCosignEnforceNamespaces
}

func planCosignEnableChanges(existing map[string]*corev1.Namespace, namespaces []string) []cosignEnablePlanItem {
	items := make([]cosignEnablePlanItem, 0, len(namespaces))
	for _, name := range namespaces {
		ns, ok := existing[name]
		if !ok {
			items = append(items, cosignEnablePlanItem{
				Namespace: name,
				Action:    "skip",
				Reason:    "namespace not found",
			})
			continue
		}
		if ns.Labels != nil && ns.Labels[cosignVerifySignaturesLabel] == "true" {
			items = append(items, cosignEnablePlanItem{
				Namespace: name,
				Action:    "skip",
				Reason:    "label already true",
			})
			continue
		}
		items = append(items, cosignEnablePlanItem{
			Namespace: name,
			Action:    "label",
			Reason:    "enable Kyverno cosign verification",
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Namespace < items[j].Namespace })
	return items
}

func (h *Handler) planCosignEnable(ctx context.Context, req operatorOperationRequest) ([]cosignEnablePlanItem, error) {
	client := h.opsKubeClient()
	if client == nil {
		return nil, fmt.Errorf("kubernetes client is not configured")
	}
	namespaces := desiredCosignNamespaces(operationTarget(req))
	existing := make(map[string]*corev1.Namespace, len(namespaces))
	for _, name := range namespaces {
		ns, err := client.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if k8serrors.IsNotFound(err) {
				existing[name] = nil
				continue
			}
			return nil, err
		}
		existing[name] = ns
	}
	return planCosignEnableChanges(existing, namespaces), nil
}

func (h *Handler) handleOpsPolicyCosignEnableDryRun(ctx context.Context, operation string, req operatorOperationRequest) operatorOperationResponse {
	operationID := fmt.Sprintf("op_%d", time.Now().UTC().UnixNano())
	plan, err := h.planCosignEnable(ctx, req)
	if err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "failed",
			DryRun:      true,
			Summary:     "failed to plan cosign namespace labels",
			Warnings:    []string{err.Error()},
		}
	}
	labelCount := 0
	for _, item := range plan {
		if item.Action == "label" {
			labelCount++
		}
	}
	status := "ready_to_apply"
	summary := fmt.Sprintf("plan to label %d namespace(s) for cosign enforce", labelCount)
	if labelCount == 0 {
		status = "succeeded"
		summary = "cosign enforce labels already present on target namespaces"
	}
	return operatorOperationResponse{
		OperationID: operationID,
		Operation:   operation,
		Status:      status,
		DryRun:      true,
		Summary:     summary,
		Data: map[string]any{
			"plan":  plan,
			"count": labelCount,
		},
		Next: []string{
			"enclii ops policy cosign-enable --apply --reason \"Commercial GA O-11 Cosign enforce\"",
			"verify images are signed before enabling on additional namespaces",
		},
	}
}

func (h *Handler) handleOpsPolicyCosignEnableApply(ctx context.Context, operation string, req operatorOperationRequest) (operatorOperationResponse, int) {
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

	plan, err := h.planCosignEnable(ctx, req)
	if err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "failed",
			DryRun:      false,
			Summary:     "failed to plan cosign namespace labels",
			Warnings:    []string{err.Error()},
		}, http.StatusInternalServerError
	}

	labeled := make([]string, 0)
	skipped := make([]string, 0)
	warnings := make([]string, 0)

	for _, item := range plan {
		if item.Action != "label" {
			skipped = append(skipped, item.Namespace)
			continue
		}
		patch := []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"true"}}}`, cosignVerifySignaturesLabel))
		_, err := client.CoreV1().Namespaces().Patch(ctx, item.Namespace, types.MergePatchType, patch, metav1.PatchOptions{})
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: patch failed: %v", item.Namespace, err))
			skipped = append(skipped, item.Namespace)
			continue
		}
		labeled = append(labeled, item.Namespace)
	}

	status := "submitted"
	summary := fmt.Sprintf("labeled %d namespace(s) for cosign enforce through Enclii", len(labeled))
	code := http.StatusAccepted
	if len(labeled) == 0 && len(warnings) > 0 {
		status = "failed"
		summary = "no namespaces were labeled"
		code = http.StatusInternalServerError
	} else if len(labeled) == 0 {
		status = "succeeded"
		summary = "cosign enforce labels already present"
		code = http.StatusOK
	}

	return operatorOperationResponse{
		OperationID: operationID,
		Operation:   operation,
		Status:      status,
		DryRun:      false,
		Summary:     summary,
		Data: map[string]any{
			"labeled": labeled,
			"skipped": skipped,
		},
		Warnings: warnings,
	}, code
}
