package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/cloudflare"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
)

type cloudflareDNSApplyIntent struct {
	Target     string
	RecordType string
	Content    string
	Proxied    bool
}

func (h *Handler) handleProviderCloudflareDNSApplyDryRun(ctx context.Context, operation string, req operatorOperationRequest) operatorOperationResponse {
	operationID := fmt.Sprintf("op_%d", time.Now().UTC().UnixNano())
	intent := cloudflareDNSApplyIntentFromRequest(req, cloudflareDNSDefaultContent(h))
	data := map[string]any{
		"target":     intent.Target,
		"type":       intent.RecordType,
		"content":    intent.Content,
		"proxied":    intent.Proxied,
		"project":    strings.TrimSpace(req.Scope["project"]),
		"service":    strings.TrimSpace(req.Scope["service"]),
		"can_apply":  false,
		"zone_owned": false,
	}
	steps := []operatorOperationStep{
		{Name: "authorize", Status: "planned", Detail: "check caller RBAC and require reason on apply"},
		{Name: "load-state", Status: "planned", Detail: "load Cloudflare zone and DNS record through Enclii"},
		{Name: "diff", Status: "planned", Detail: "compare desired DNS record with live Cloudflare state"},
		{Name: "audit", Status: "planned", Detail: "record operation reason and idempotency key before mutation"},
	}
	if intent.Target == "" {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "invalid_request",
			DryRun:      true,
			Summary:     "cloudflare.dns-apply requires a target domain",
			Data:        data,
			Steps:       steps,
			Warnings:    []string{"missing args.target or scope.target"},
		}
	}

	cfClient := h.cloudflareDNSApplyClient()
	if cfClient == nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "adapter_unconfigured",
			DryRun:      true,
			Summary:     "cloudflare.dns-apply cannot run until the Cloudflare domain sync client is configured",
			Data:        data,
			Steps:       steps,
			Warnings:    []string{"cloudflare domain sync service is not configured"},
			Next:        []string{"set ENCLII_CLOUDFLARE_API_TOKEN, ENCLII_CLOUDFLARE_ACCOUNT_ID, ENCLII_CLOUDFLARE_ZONE_ID, and ENCLII_CLOUDFLARE_TUNNEL_ID on switchyard-api"},
		}
	}

	zone, err := cfClient.FindZoneForDomain(ctx, intent.Target)
	if err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "blocked_by_dns_authority",
			DryRun:      true,
			Summary:     fmt.Sprintf("Cloudflare zone authority is not available for %s", intent.Target),
			Data:        data,
			Steps:       steps,
			Warnings:    []string{err.Error()},
			Next: []string{
				"delegate or import the apex zone into the Enclii-managed Cloudflare account",
				"configure the Enclii Porkbun adapter when registrar nameserver changes are required",
				"rerun this dry-run before applying DNS",
			},
		}
	}

	data["zoneID"] = zone.ID
	data["zoneName"] = zone.Name
	data["zone_owned"] = true
	record, err := cfClient.GetDNSRecordByType(ctx, intent.Target, intent.RecordType)
	if err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "provider_read_failed",
			DryRun:      true,
			Summary:     fmt.Sprintf("failed to read Cloudflare DNS record for %s", intent.Target),
			Data:        data,
			Steps:       steps,
			Warnings:    []string{err.Error()},
		}
	}

	mutation := "create"
	if record != nil {
		data["existingRecord"] = record
		if record.Content == intent.Content && record.Proxied == intent.Proxied {
			mutation = "noop"
		} else {
			mutation = "update"
		}
	}
	data["mutation"] = mutation
	data["can_apply"] = true
	return operatorOperationResponse{
		OperationID: operationID,
		Operation:   operation,
		Status:      "ready_to_apply",
		DryRun:      true,
		Summary:     fmt.Sprintf("cloudflare.dns-apply dry-run completed for %s", intent.Target),
		Data:        data,
		Steps:       steps,
		Next: []string{
			"rerun with --apply and a reason to execute the DNS mutation through Enclii",
			"poll providers.cloudflare.dns and the public DNS resolver until the record converges",
		},
	}
}

func (h *Handler) handleProviderCloudflareDNSApply(ctx context.Context, operation string, req operatorOperationRequest) (operatorOperationResponse, int) {
	operationID := fmt.Sprintf("op_%d", time.Now().UTC().UnixNano())
	intent := cloudflareDNSApplyIntentFromRequest(req, cloudflareDNSDefaultContent(h))
	data := map[string]any{
		"target":  intent.Target,
		"type":    intent.RecordType,
		"content": intent.Content,
		"proxied": intent.Proxied,
		"project": strings.TrimSpace(req.Scope["project"]),
		"service": strings.TrimSpace(req.Scope["service"]),
	}
	steps := []operatorOperationStep{
		{Name: "authorize", Status: "completed", Detail: "reason supplied and caller passed endpoint authorization"},
		{Name: "load-state", Status: "planned", Detail: "load Cloudflare zone and DNS record through Enclii"},
		{Name: "diff", Status: "planned", Detail: "compare desired DNS record with live Cloudflare state"},
		{Name: "audit", Status: "planned", Detail: "record operation reason and idempotency key before mutation"},
	}
	if intent.Target == "" {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "invalid_request",
			DryRun:      false,
			Summary:     "cloudflare.dns-apply requires a target domain",
			Data:        data,
			Steps:       steps,
			Warnings:    []string{"missing args.target or scope.target"},
		}, http.StatusBadRequest
	}

	cfClient := h.cloudflareDNSApplyClient()
	if cfClient == nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "adapter_unconfigured",
			DryRun:      false,
			Summary:     "cloudflare.dns-apply cannot run until the Cloudflare domain sync client is configured",
			Data:        data,
			Steps:       steps,
			Warnings:    []string{"cloudflare domain sync service is not configured"},
			Next:        []string{"configure the Cloudflare provider environment on switchyard-api, then retry through Enclii"},
		}, http.StatusServiceUnavailable
	}

	zone, err := cfClient.FindZoneForDomain(ctx, intent.Target)
	if err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "blocked_by_dns_authority",
			DryRun:      false,
			Summary:     fmt.Sprintf("Cloudflare zone authority is not available for %s", intent.Target),
			Data:        data,
			Steps:       steps,
			Warnings:    []string{err.Error()},
			Next: []string{
				"delegate or import the apex zone into the Enclii-managed Cloudflare account",
				"configure and apply the Enclii Porkbun adapter if registrar nameservers must change",
			},
		}, http.StatusFailedDependency
	}

	data["zoneID"] = zone.ID
	data["zoneName"] = zone.Name
	steps[1].Status = "completed"
	record, err := cfClient.GetDNSRecordByType(ctx, intent.Target, intent.RecordType)
	if err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "provider_read_failed",
			DryRun:      false,
			Summary:     fmt.Sprintf("failed to read Cloudflare DNS record for %s", intent.Target),
			Data:        data,
			Steps:       steps,
			Warnings:    []string{err.Error()},
		}, http.StatusBadGateway
	}

	if record != nil && record.Content == intent.Content && record.Proxied == intent.Proxied {
		data["mutation"] = "noop"
		data["record"] = record
		steps[2].Status = "completed"
		steps[2].Detail = "live Cloudflare DNS already matches desired state"
		steps[3].Status = "completed"
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "noop",
			DryRun:      false,
			Summary:     fmt.Sprintf("Cloudflare DNS for %s already matches desired Enclii state", intent.Target),
			Data:        data,
			Steps:       steps,
			Next:        []string{"poll public DNS and the service health check until status converges"},
		}, http.StatusOK
	}

	var changed any
	mutation := "create"
	if record != nil {
		mutation = "update"
		changed, err = cfClient.UpdateDNSRecordInZone(ctx, zone.ID, *record, intent.Content, intent.Proxied)
	} else {
		changed, err = cfClient.CreateDNSRecordInZone(ctx, zone.ID, intent.Target, intent.RecordType, intent.Content, intent.Proxied)
	}
	if err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "provider_apply_failed",
			DryRun:      false,
			Summary:     fmt.Sprintf("failed to %s Cloudflare DNS record for %s", mutation, intent.Target),
			Data:        data,
			Steps:       steps,
			Warnings:    []string{err.Error()},
		}, http.StatusBadGateway
	}

	data["mutation"] = mutation
	data["record"] = changed
	steps[2].Status = "completed"
	steps[2].Detail = fmt.Sprintf("%s %s record through Cloudflare", mutation, intent.RecordType)
	steps[3].Status = "completed"
	return operatorOperationResponse{
		OperationID: operationID,
		Operation:   operation,
		Status:      "succeeded",
		DryRun:      false,
		Summary:     fmt.Sprintf("%sd Cloudflare DNS record for %s through Enclii", mutation, intent.Target),
		Data:        data,
		Steps:       steps,
		Next: []string{
			"poll providers.cloudflare.dns until the record is visible",
			"poll the public service endpoint until status.madfam.io converges",
		},
	}, http.StatusAccepted
}

func (h *Handler) cloudflareDNSApplyClient() *cloudflare.Client {
	if h == nil || h.domainSyncService == nil {
		return nil
	}
	return h.domainSyncService.GetCloudflareClient()
}

func cloudflareDNSDefaultContent(h *Handler) string {
	if h != nil && h.domainSyncService != nil {
		return h.domainSyncService.TunnelCNAME()
	}
	return services.DefaultTunnelCNAME
}

func cloudflareDNSApplyIntentFromRequest(req operatorOperationRequest, defaultContent string) cloudflareDNSApplyIntent {
	recordType := strings.ToUpper(strings.TrimSpace(req.Args["type"]))
	if recordType == "" {
		recordType = "CNAME"
	}
	content := strings.TrimSpace(req.Args["content"])
	if content == "" {
		content = strings.TrimSpace(req.Args["cname"])
	}
	if content == "" {
		content = strings.TrimSpace(req.Args["record_content"])
	}
	if content == "" {
		content = defaultContent
	}
	return cloudflareDNSApplyIntent{
		Target:     operationTarget(req),
		RecordType: recordType,
		Content:    content,
		Proxied:    cloudflareDNSApplyProxied(req, recordType),
	}
}

func cloudflareDNSApplyProxied(req operatorOperationRequest, recordType string) bool {
	defaultProxied := recordType == "A" || recordType == "AAAA" || recordType == "CNAME"
	value := strings.ToLower(strings.TrimSpace(req.Args["proxied"]))
	if value == "" {
		value = strings.ToLower(strings.TrimSpace(req.Args["proxy"]))
	}
	switch value {
	case "true", "1", "yes", "y", "on":
		return true
	case "false", "0", "no", "n", "off":
		return false
	default:
		return defaultProxied
	}
}
