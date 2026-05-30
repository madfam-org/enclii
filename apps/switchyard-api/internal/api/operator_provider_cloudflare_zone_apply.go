package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func (h *Handler) handleProviderCloudflareZoneAddApplyDryRun(ctx context.Context, operation string, req operatorOperationRequest) operatorOperationResponse {
	target := strings.TrimSpace(operationTarget(req))
	data := gin.H{"target": target}
	steps := []operatorOperationStep{
		{Name: "authorize", Status: "planned", Detail: "verify admin RBAC and audit reason on apply"},
		{Name: "load-state", Status: "planned", Detail: "list Cloudflare zones for existing match"},
		{Name: "diff", Status: "planned", Detail: "create zone when absent"},
		{Name: "audit", Status: "planned", Detail: "record operation_id and reason"},
	}
	if target == "" {
		return operatorOperationResponse{
			OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
			Operation:   operation,
			Status:      "invalid_request",
			DryRun:      true,
			Summary:     "cloudflare.zone-add-apply requires args.target (apex domain)",
			Data:        data,
			Warnings:    []string{"missing args.target"},
		}
	}
	client := h.cloudflareDNSApplyClient()
	if client == nil {
		return operatorOperationResponse{
			OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
			Operation:   operation,
			Status:      "adapter_unconfigured",
			DryRun:      true,
			Summary:     "cloudflare.zone-add-apply cannot run until Cloudflare credentials are configured",
			Data:        data,
			Steps:       steps,
			Warnings:    []string{"Cloudflare domain sync client is not configured"},
		}
	}
	existing, err := client.FindZoneForDomain(ctx, target)
	if err != nil && !strings.Contains(err.Error(), "no Cloudflare zone found") {
		return operatorOperationResponse{
			OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
			Operation:   operation,
			Status:      "provider_read_failed",
			DryRun:      true,
			Summary:     fmt.Sprintf("failed to read Cloudflare zone state for %s", target),
			Data:        data,
			Steps:       steps,
			Warnings:    []string{err.Error()},
		}
	}
	mutation := "create"
	if existing != nil {
		mutation = "noop"
		data["existingZone"] = existing
	}
	data["mutation"] = mutation
	data["can_apply"] = mutation == "create"
	return operatorOperationResponse{
		OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
		Operation:   operation,
		Status:      "ready_to_apply",
		DryRun:      true,
		Summary:     fmt.Sprintf("cloudflare.zone-add-apply dry-run completed for %s", target),
		Data:        data,
		Steps:       steps,
		Next:        []string{"rerun with dry_run=false and a reason to create the Cloudflare zone"},
	}
}

func (h *Handler) handleProviderCloudflareZoneAddApply(ctx context.Context, operation string, req operatorOperationRequest) (operatorOperationResponse, int) {
	dry := h.handleProviderCloudflareZoneAddApplyDryRun(ctx, operation, req)
	if dry.Status == "invalid_request" {
		return dry, http.StatusBadRequest
	}
	if dry.Status == "adapter_unconfigured" {
		return dry, http.StatusServiceUnavailable
	}
	if dry.Status == "provider_read_failed" {
		return dry, http.StatusBadGateway
	}
	if data, ok := dry.Data.(gin.H); ok {
		if mutation, _ := data["mutation"].(string); mutation == "noop" {
			return operatorOperationResponse{
				OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
				Operation:   operation,
				Status:      "noop",
				DryRun:      false,
				Summary:     fmt.Sprintf("Cloudflare zone for %s already exists", operationTarget(req)),
				Data:        data,
			}, http.StatusOK
		}
	}
	target := operationTarget(req)
	zone, err := h.cloudflareDNSApplyClient().CreateZone(ctx, target)
	if err != nil {
		return operatorOperationResponse{
			OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
			Operation:   operation,
			Status:      "provider_apply_failed",
			DryRun:      false,
			Summary:     fmt.Sprintf("failed to create Cloudflare zone for %s", target),
			Warnings:    []string{err.Error()},
		}, http.StatusBadGateway
	}
	return operatorOperationResponse{
		OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
		Operation:   operation,
		Status:      "succeeded",
		DryRun:      false,
		Summary:     fmt.Sprintf("created Cloudflare zone %s through Enclii", target),
		Data: gin.H{
			"target":      target,
			"zone":        zone,
			"nameservers": zone.NameServers,
		},
		Next: []string{"update registrar nameservers", "poll providers.cloudflare.zones until status is active"},
	}, http.StatusAccepted
}
