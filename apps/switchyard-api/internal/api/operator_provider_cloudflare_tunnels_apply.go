package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

type tunnelRoutePlanItem struct {
	Hostname       string `json:"hostname"`
	Action         string `json:"action"`
	CurrentService string `json:"current_service,omitempty"`
	DesiredService string `json:"desired_service"`
	ServiceName    string `json:"service_name"`
	Namespace      string `json:"namespace"`
	Port           int    `json:"port"`
	Reason         string `json:"reason,omitempty"`
}

func planTunnelRouteDrifts(live []services.IngressRule, specs []*services.RouteSpec) []tunnelRoutePlanItem {
	items := make([]tunnelRoutePlanItem, 0, len(specs))
	for _, spec := range specs {
		if spec == nil || strings.TrimSpace(spec.Hostname) == "" {
			continue
		}
		want := tunnelRouteServiceURL(spec)
		current := currentTunnelService(live, spec.Hostname)
		item := tunnelRoutePlanItem{
			Hostname:       spec.Hostname,
			DesiredService: want,
			ServiceName:    spec.ServiceName,
			Namespace:      spec.ServiceNamespace,
			Port:           spec.ServicePort,
			CurrentService: current,
		}
		switch {
		case current == "":
			item.Action = "create"
			item.Reason = "tunnel route missing"
		case current == want:
			item.Action = "skip"
			item.Reason = "already targets desired service"
		default:
			item.Action = "update"
			item.Reason = "live route targets different backend"
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Hostname < items[j].Hostname })
	return items
}

func currentTunnelService(live []services.IngressRule, hostname string) string {
	for _, route := range live {
		if route.Hostname == hostname {
			return route.Service
		}
	}
	return ""
}

func (h *Handler) planJunctionTunnelRoutes(ctx context.Context, projectSlug, target string) ([]tunnelRoutePlanItem, *types.Project, error) {
	if h == nil || h.repos == nil || h.repos.Projects == nil || h.repos.Junctions == nil || h.repos.Services == nil {
		return nil, nil, fmt.Errorf("project repositories are not configured")
	}
	if h.tunnelRoutesService == nil {
		return nil, nil, fmt.Errorf("cloudflare tunnel routes service is not configured")
	}

	projectSlug = strings.TrimSpace(projectSlug)
	if projectSlug == "" {
		return nil, nil, fmt.Errorf("project slug is required")
	}

	project, err := h.repos.Projects.GetBySlug(projectSlug)
	if err != nil {
		return nil, nil, fmt.Errorf("project lookup failed: %w", err)
	}
	if project == nil {
		return nil, nil, fmt.Errorf("project %q not found", projectSlug)
	}

	if _, err := h.ensureDefaultProductionEnvironment(ctx, project); err != nil {
		h.logger.Warn(ctx, "Failed to ensure default environment before tunnel route planning",
			logging.String("project", project.Slug),
			logging.Error("error", err))
	}

	junctions, err := h.repos.Junctions.ListByProject(ctx, project.ID)
	if err != nil {
		return nil, project, fmt.Errorf("list junctions: %w", err)
	}

	target = strings.TrimSpace(target)
	specs := make([]*services.RouteSpec, 0, len(junctions))
	for _, junction := range junctions {
		if junction == nil || junction.Domain == "" {
			continue
		}
		if target != "" && junction.Domain != target {
			continue
		}

		service, err := h.repos.Services.GetByID(junction.ServiceID)
		if err != nil {
			return nil, project, fmt.Errorf("service lookup for %s: %w", junction.Domain, err)
		}
		namespace := h.resolveServiceNamespace(ctx, service, defaultProductionEnvironmentName)
		specs = append(specs, &services.RouteSpec{
			Hostname:         junction.Domain,
			ServiceName:      service.Name,
			ServiceNamespace: namespace,
			ServicePort:      80,
		})
	}

	live, err := h.tunnelRoutesService.ListRoutes(ctx)
	if err != nil {
		return nil, project, fmt.Errorf("list tunnel routes: %w", err)
	}

	return planTunnelRouteDrifts(live, specs), project, nil
}

func (h *Handler) handleProviderCloudflareTunnelsApplyDryRun(ctx context.Context, operation string, req operatorOperationRequest) operatorOperationResponse {
	operationID := fmt.Sprintf("op_%d", time.Now().UTC().UnixNano())
	projectSlug := strings.TrimSpace(req.Scope["project"])
	if projectSlug == "" {
		projectSlug = strings.TrimSpace(req.Args["project"])
	}
	target := operationTarget(req)
	data := map[string]any{
		"project": projectSlug,
		"target":  target,
	}
	steps := []operatorOperationStep{
		{Name: "authorize", Status: "planned", Detail: "check caller RBAC and require reason on apply"},
		{Name: "load-state", Status: "planned", Detail: "load project junctions and live Cloudflare tunnel routes"},
		{Name: "diff", Status: "planned", Detail: "compare desired K8s service backends with live tunnel ingress"},
		{Name: "audit", Status: "planned", Detail: "record operation reason and idempotency key before mutation"},
	}

	if projectSlug == "" {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "invalid_request",
			DryRun:      true,
			Summary:     "cloudflare.tunnels-apply requires scope.project",
			Data:        data,
			Steps:       steps,
			Warnings:    []string{"missing scope.project or --project"},
		}
	}

	if h.tunnelRoutesService == nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "adapter_unconfigured",
			DryRun:      true,
			Summary:     "cloudflare.tunnels-apply cannot run until the Cloudflare tunnel routes service is configured",
			Data:        data,
			Steps:       steps,
			Warnings:    []string{"cloudflare tunnel routes service is not configured"},
			Next:        []string{"set ENCLII_CLOUDFLARE_TUNNEL_ID and tunnel route wiring on switchyard-api"},
		}
	}

	plan, project, err := h.planJunctionTunnelRoutes(ctx, projectSlug, target)
	if err != nil {
		status := "failed"
		if strings.Contains(err.Error(), "not found") {
			status = "invalid_request"
		}
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      status,
			DryRun:      true,
			Summary:     fmt.Sprintf("cloudflare.tunnels-apply planning failed for project %s", projectSlug),
			Data:        data,
			Steps:       steps,
			Warnings:    []string{err.Error()},
		}
	}

	mutateCount := 0
	for _, item := range plan {
		if item.Action == "create" || item.Action == "update" {
			mutateCount++
		}
	}

	status := "ready_to_apply"
	summary := fmt.Sprintf("plan to reconcile %d tunnel route(s) for project %s", mutateCount, project.Slug)
	if mutateCount == 0 {
		status = "succeeded"
		if len(plan) == 0 {
			summary = fmt.Sprintf("no junction domains found for project %s", project.Slug)
		} else {
			summary = fmt.Sprintf("tunnel routes already target desired services for project %s", project.Slug)
		}
	}

	return operatorOperationResponse{
		OperationID: operationID,
		Operation:   operation,
		Status:      status,
		DryRun:      true,
		Summary:     summary,
		Data: map[string]any{
			"project": project.Slug,
			"target":  target,
			"plan":    plan,
			"count":   mutateCount,
		},
		Steps: steps,
		Next: []string{
			fmt.Sprintf("enclii providers cloudflare tunnels-apply --project %s --apply --reason \"reconcile junction tunnel routes\"", project.Slug),
		},
	}
}

func (h *Handler) handleProviderCloudflareTunnelsApply(ctx context.Context, operation string, req operatorOperationRequest) (operatorOperationResponse, int) {
	operationID := fmt.Sprintf("op_%d", time.Now().UTC().UnixNano())
	projectSlug := strings.TrimSpace(req.Scope["project"])
	if projectSlug == "" {
		projectSlug = strings.TrimSpace(req.Args["project"])
	}
	target := operationTarget(req)

	if projectSlug == "" {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "invalid_request",
			DryRun:      false,
			Summary:     "cloudflare.tunnels-apply requires scope.project",
			Warnings:    []string{"missing scope.project or --project"},
		}, http.StatusBadRequest
	}

	if h.tunnelRoutesService == nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "adapter_unconfigured",
			DryRun:      false,
			Summary:     "cloudflare.tunnels-apply cannot run until the Cloudflare tunnel routes service is configured",
			Warnings:    []string{"cloudflare tunnel routes service is not configured"},
		}, http.StatusServiceUnavailable
	}

	plan, project, err := h.planJunctionTunnelRoutes(ctx, projectSlug, target)
	if err != nil {
		code := http.StatusInternalServerError
		status := "failed"
		if strings.Contains(err.Error(), "not found") {
			code = http.StatusBadRequest
			status = "invalid_request"
		}
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      status,
			DryRun:      false,
			Summary:     fmt.Sprintf("cloudflare.tunnels-apply failed for project %s", projectSlug),
			Warnings:    []string{err.Error()},
		}, code
	}

	updated := make([]string, 0)
	created := make([]string, 0)
	skipped := make([]string, 0)
	warnings := make([]string, 0)

	for _, item := range plan {
		switch item.Action {
		case "create", "update":
			spec := &services.RouteSpec{
				Hostname:         item.Hostname,
				ServiceName:      item.ServiceName,
				ServiceNamespace: item.Namespace,
				ServicePort:      item.Port,
			}
			if err := h.tunnelRoutesService.AddRoute(ctx, spec); err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: %v", item.Hostname, err))
				continue
			}
			if item.Action == "create" {
				created = append(created, item.Hostname)
			} else {
				updated = append(updated, item.Hostname)
			}
		default:
			skipped = append(skipped, item.Hostname)
		}
	}

	mutated := len(created) + len(updated)
	status := "submitted"
	summary := fmt.Sprintf("reconciled %d tunnel route(s) for project %s through Enclii", mutated, project.Slug)
	code := http.StatusAccepted
	if mutated == 0 && len(warnings) > 0 {
		status = "failed"
		summary = "no tunnel routes were reconciled"
		code = http.StatusInternalServerError
	} else if mutated == 0 {
		status = "succeeded"
		summary = fmt.Sprintf("tunnel routes already target desired services for project %s", project.Slug)
		code = http.StatusOK
	}

	return operatorOperationResponse{
		OperationID: operationID,
		Operation:   operation,
		Status:      status,
		DryRun:      false,
		Summary:     summary,
		Data: map[string]any{
			"project": project.Slug,
			"created": created,
			"updated": updated,
			"skipped": skipped,
		},
		Warnings: warnings,
	}, code
}
