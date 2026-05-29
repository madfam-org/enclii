package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var defaultSyncSweepExcludedApps = []string{"network-policies"}

type argoSyncSweepTarget struct {
	Name         string `json:"name"`
	SyncStatus   string `json:"syncStatus"`
	HealthStatus string `json:"healthStatus"`
}

func syncSweepExcludedApps(req operatorOperationRequest) map[string]struct{} {
	excluded := make(map[string]struct{}, len(defaultSyncSweepExcludedApps)+2)
	for _, name := range defaultSyncSweepExcludedApps {
		excluded[name] = struct{}{}
	}
	if extra := strings.TrimSpace(req.Args["exclude"]); extra != "" {
		for _, name := range splitCSV(extra) {
			if trimmed := strings.TrimSpace(name); trimmed != "" {
				excluded[trimmed] = struct{}{}
			}
		}
	}
	return excluded
}

func (h *Handler) collectArgoSyncSweepTargets(ctx context.Context, req operatorOperationRequest) (namespace string, targets []argoSyncSweepTarget, excluded []string, err error) {
	if h.k8sClient == nil || h.k8sClient.DynamicClient == nil {
		return "", nil, nil, fmt.Errorf("kubernetes dynamic client is not configured")
	}
	namespace = operationNamespace(req, "argocd")
	drift, err := h.readArgoApplicationDrift(ctx, req)
	if err != nil {
		return namespace, nil, nil, err
	}

	exclusionSet := syncSweepExcludedApps(req)
	excluded = make([]string, 0)
	targets = make([]argoSyncSweepTarget, 0)

	rawApps, _ := drift["applications"].([]gin.H)
	for _, raw := range rawApps {
		name := fmt.Sprint(raw["name"])
		if name == "" || name == "<nil>" {
			continue
		}
		if _, skip := exclusionSet[name]; skip {
			excluded = append(excluded, name)
			continue
		}
		drifted, _ := raw["drifted"].(bool)
		syncStatus := fmt.Sprint(raw["syncStatus"])
		if !drifted && strings.EqualFold(syncStatus, "Synced") {
			continue
		}
		targets = append(targets, argoSyncSweepTarget{
			Name:         name,
			SyncStatus:   syncStatus,
			HealthStatus: fmt.Sprint(raw["healthStatus"]),
		})
	}

	sort.Slice(excluded, func(i, j int) bool { return excluded[i] < excluded[j] })
	sort.Slice(targets, func(i, j int) bool { return targets[i].Name < targets[j].Name })
	return namespace, targets, excluded, nil
}

func (h *Handler) handleOpsAppsSyncSweepDryRun(ctx context.Context, operation string, req operatorOperationRequest) operatorOperationResponse {
	operationID := fmt.Sprintf("op_%d", time.Now().UTC().UnixNano())
	namespace, targets, excluded, err := h.collectArgoSyncSweepTargets(ctx, req)
	if err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "failed",
			DryRun:      true,
			Summary:     "failed to list Argo application drift",
			Warnings:    []string{err.Error()},
		}
	}

	status := "ready_to_apply"
	summary := fmt.Sprintf("found %d Argo application(s) eligible for sync-sweep", len(targets))
	if len(targets) == 0 {
		status = "succeeded"
		summary = "all non-excluded Argo applications are Synced"
	}

	return operatorOperationResponse{
		OperationID: operationID,
		Operation:   operation,
		Status:      status,
		DryRun:      true,
		Summary:     summary,
		Data: map[string]any{
			"namespace": namespace,
			"targets":   targets,
			"excluded":  excluded,
			"count":     len(targets),
		},
		Steps: []operatorOperationStep{
			{Name: "authorize", Status: "planned", Detail: "admin role required on apply"},
			{Name: "load-state", Status: "completed", Detail: "listed Argo Application drift in namespace"},
			{Name: "diff", Status: "completed", Detail: summary},
			{Name: "audit", Status: "planned", Detail: "write operation reason before sync"},
		},
		Next: []string{
			"enclii ops apps sync-sweep --apply --reason \"Commercial GA O-8 Argo sweep\"",
			"document permanently excluded apps with args.exclude",
		},
	}
}

func (h *Handler) handleOpsAppsSyncSweepApply(ctx context.Context, operation string, req operatorOperationRequest) (operatorOperationResponse, int) {
	operationID := fmt.Sprintf("op_%d", time.Now().UTC().UnixNano())
	namespace, targets, excluded, err := h.collectArgoSyncSweepTargets(ctx, req)
	if err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "failed",
			DryRun:      false,
			Summary:     "failed to list Argo application drift",
			Warnings:    []string{err.Error()},
		}, http.StatusInternalServerError
	}

	if len(targets) == 0 {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "succeeded",
			DryRun:      false,
			Summary:     "no Argo applications require sync-sweep",
			Data: map[string]any{
				"namespace": namespace,
				"excluded":  excluded,
				"synced":    []string{},
				"skipped":   []string{},
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
		subReq.Scope["namespace"] = namespace

		resp, code := h.handleOpsAppsSyncApply(ctx, operation+".sync", subReq)
		switch resp.Status {
		case "submitted":
			synced = append(synced, target.Name)
		case "already_running":
			skipped = append(skipped, target.Name)
			warnings = append(warnings, fmt.Sprintf("%s: %s", target.Name, resp.Summary))
		default:
			skipped = append(skipped, target.Name)
			warnings = append(warnings, fmt.Sprintf("%s: HTTP %d %s", target.Name, code, resp.Summary))
		}
	}

	status := "submitted"
	summary := fmt.Sprintf("submitted sync for %d Argo application(s) through Enclii", len(synced))
	code := http.StatusAccepted
	if len(synced) == 0 {
		status = "failed"
		summary = "no Argo applications were synced"
		code = http.StatusInternalServerError
	}

	return operatorOperationResponse{
		OperationID: operationID,
		Operation:   operation,
		Status:      status,
		DryRun:      false,
		Summary:     summary,
		Data: map[string]any{
			"namespace": namespace,
			"excluded":  excluded,
			"synced":    synced,
			"skipped":   skipped,
		},
		Steps: []operatorOperationStep{
			{Name: "authorize", Status: "completed", Detail: "reason supplied and caller passed endpoint authorization"},
			{Name: "load-state", Status: "completed", Detail: "listed drifted Argo Applications"},
			{Name: "diff", Status: "completed", Detail: summary},
			{Name: "audit", Status: "completed", Detail: "patched operation.sync on each eligible application"},
		},
		Warnings: warnings,
		Next: []string{
			"poll enclii ops apps diff -n argocd until driftedCount is 0",
			"document any apps that remain OutOfSync by design",
		},
	}, code
}
