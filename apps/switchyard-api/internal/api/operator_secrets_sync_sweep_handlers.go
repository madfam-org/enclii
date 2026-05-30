package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var gaExternalSecretNamespaces = []string{"enclii", "data", "monitoring", "cloudflare-tunnel"}

type externalSecretSyncTarget struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Ready     bool   `json:"ready"`
	Reason    string `json:"reason,omitempty"`
}

func externalSecretReady(obj *unstructured.Unstructured) bool {
	if obj == nil {
		return false
	}
	conditions, ok, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if !ok {
		return false
	}
	for _, raw := range conditions {
		cond, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if cond["type"] == "Ready" && cond["status"] == "True" {
			return true
		}
	}
	return false
}

func syncSweepNamespaces(req operatorOperationRequest) []string {
	if ns := strings.TrimSpace(operationNamespace(req, "")); ns != "" {
		return []string{ns}
	}
	return gaExternalSecretNamespaces
}

func planExternalSecretSyncTargets(items []externalSecretSyncTarget) []externalSecretSyncTarget {
	targets := make([]externalSecretSyncTarget, 0, len(items))
	for _, item := range items {
		if item.Ready {
			continue
		}
		targets = append(targets, item)
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Namespace == targets[j].Namespace {
			return targets[i].Name < targets[j].Name
		}
		return targets[i].Namespace < targets[j].Namespace
	})
	return targets
}

func (h *Handler) collectExternalSecretSyncTargets(ctx context.Context, req operatorOperationRequest) ([]externalSecretSyncTarget, error) {
	if h == nil || h.k8sClient == nil || h.k8sClient.DynamicClient == nil {
		return nil, fmt.Errorf("kubernetes dynamic client is not configured")
	}

	targetFilter := strings.TrimSpace(operationTarget(req))
	namespaces := syncSweepNamespaces(req)
	items := make([]externalSecretSyncTarget, 0)

	for _, namespace := range namespaces {
		list, err := h.k8sClient.DynamicClient.Resource(externalSecretGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list ExternalSecrets in %s: %w", namespace, err)
		}
		for i := range list.Items {
			item := &list.Items[i]
			name := item.GetName()
			if targetFilter != "" && name != targetFilter {
				continue
			}
			ready := externalSecretReady(item)
			reason := "Ready=True"
			if !ready {
				reason = externalSecretNotReadyReason(item)
			}
			items = append(items, externalSecretSyncTarget{
				Namespace: namespace,
				Name:      name,
				Ready:     ready,
				Reason:    reason,
			})
		}
	}

	if targetFilter != "" && len(items) == 0 {
		return nil, fmt.Errorf("ExternalSecret %q not found in sweep namespaces", targetFilter)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Namespace == items[j].Namespace {
			return items[i].Name < items[j].Name
		}
		return items[i].Namespace < items[j].Namespace
	})
	return items, nil
}

func externalSecretNotReadyReason(obj *unstructured.Unstructured) string {
	conditions, ok, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if !ok || len(conditions) == 0 {
		return "no Ready condition yet"
	}
	for _, raw := range conditions {
		cond, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if cond["type"] == "Ready" {
			reason := strings.TrimSpace(fmt.Sprint(cond["reason"]))
			message := strings.TrimSpace(fmt.Sprint(cond["message"]))
			if reason != "" && reason != "<nil>" {
				if message != "" && message != "<nil>" {
					return reason + ": " + message
				}
				return reason
			}
		}
	}
	return "Ready=False"
}

func (h *Handler) handleOpsSecretsSyncSweepDryRun(ctx context.Context, operation string, req operatorOperationRequest) operatorOperationResponse {
	operationID := fmt.Sprintf("op_%d", time.Now().UTC().UnixNano())
	items, err := h.collectExternalSecretSyncTargets(ctx, req)
	if err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "failed",
			DryRun:      true,
			Summary:     "failed to list ExternalSecret readiness",
			Warnings:    []string{err.Error()},
		}
	}

	targets := planExternalSecretSyncTargets(items)
	status := "ready_to_apply"
	summary := fmt.Sprintf("found %d ExternalSecret(s) eligible for sync-sweep", len(targets))
	if len(targets) == 0 {
		status = "succeeded"
		if len(items) == 0 {
			summary = "no ExternalSecrets found in GA sweep namespaces"
		} else {
			summary = "all ExternalSecrets in sweep scope are Ready"
		}
	}

	return operatorOperationResponse{
		OperationID: operationID,
		Operation:   operation,
		Status:      status,
		DryRun:      true,
		Summary:     summary,
		Data: map[string]any{
			"namespaces": syncSweepNamespaces(req),
			"inventory":  items,
			"targets":    targets,
			"count":      len(targets),
		},
		Steps: []operatorOperationStep{
			{Name: "authorize", Status: "planned", Detail: "admin role required on apply"},
			{Name: "load-state", Status: "completed", Detail: "listed ExternalSecret Ready conditions in GA namespaces"},
			{Name: "diff", Status: "completed", Detail: summary},
			{Name: "audit", Status: "planned", Detail: "write operation reason before force-sync"},
		},
		Next: []string{
			"enclii ops secrets sync-sweep --apply --reason \"Commercial GA ESO reconcile\"",
			"populate missing Vault paths before retrying if SecretSyncedError persists",
		},
	}
}

func (h *Handler) handleOpsSecretsSyncSweepApply(ctx context.Context, operation string, req operatorOperationRequest) (operatorOperationResponse, int) {
	operationID := fmt.Sprintf("op_%d", time.Now().UTC().UnixNano())
	items, err := h.collectExternalSecretSyncTargets(ctx, req)
	if err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "failed",
			DryRun:      false,
			Summary:     "failed to list ExternalSecret readiness",
			Warnings:    []string{err.Error()},
		}, http.StatusInternalServerError
	}

	targets := planExternalSecretSyncTargets(items)
	if len(targets) == 0 {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "succeeded",
			DryRun:      false,
			Summary:     "no ExternalSecrets require sync-sweep",
			Data: map[string]any{
				"synced":  []string{},
				"skipped": []string{},
			},
		}, http.StatusOK
	}

	synced := make([]string, 0, len(targets))
	skipped := make([]string, 0)
	warnings := make([]string, 0)

	for _, target := range targets {
		subReq := req
		if subReq.Args == nil {
			subReq.Args = map[string]string{}
		}
		subReq.Args["target"] = target.Name
		if subReq.Scope == nil {
			subReq.Scope = map[string]string{}
		}
		subReq.Scope["namespace"] = target.Namespace

		resp, code := h.handleOpsSecretsRefreshApply(ctx, operation+".sync", subReq)
		if resp.Status == "submitted" {
			synced = append(synced, fmt.Sprintf("%s/%s", target.Namespace, target.Name))
			continue
		}
		skipped = append(skipped, fmt.Sprintf("%s/%s", target.Namespace, target.Name))
		warnings = append(warnings, fmt.Sprintf("%s/%s: HTTP %d %s", target.Namespace, target.Name, code, resp.Summary))
	}

	status := "submitted"
	summary := fmt.Sprintf("requested sync for %d ExternalSecret(s) through Enclii", len(synced))
	code := http.StatusAccepted
	if len(synced) == 0 {
		status = "failed"
		summary = "no ExternalSecrets were synced"
		code = http.StatusInternalServerError
	}

	return operatorOperationResponse{
		OperationID: operationID,
		Operation:   operation,
		Status:      status,
		DryRun:      false,
		Summary:     summary,
		Data: map[string]any{
			"synced":  synced,
			"skipped": skipped,
		},
		Warnings: warnings,
		Next: []string{
			"poll enclii ops secrets external -n <ns> until Ready=True",
			"run enclii admin ga-verify --stability after Vault paths are populated",
		},
	}, code
}
