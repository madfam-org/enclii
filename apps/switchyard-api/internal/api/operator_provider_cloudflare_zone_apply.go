package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/cloudflare"
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
	// "Zone absent" is the ONLY state in which this operation has work to do, so
	// it must fall through to mutation = "create" rather than being reported as a
	// read failure. This previously matched on the literal "no Cloudflare zone
	// found", which FindZoneForDomain has not returned since it was reworded to
	// cloudflare.ErrZoneNotFound ("cloudflare: no zone found for domain") — see
	// the HIGH-1 note in internal/cloudflare/zone_lookup_test.go. The strings do
	// not overlap, so every absent zone was misclassified as provider_read_failed
	// and zone-add-apply could never create a zone. The apply path returns 502 on
	// that status, so both paths were dead.
	// errors.Is is what the rest of this package already uses for this sentinel
	// (domain_provisioner_custom_hostname.go, domain_teardown.go).
	if err != nil && !errors.Is(err, cloudflare.ErrZoneNotFound) {
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
