package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/ecosystem"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/resend"
)

func (h *Handler) resendClient() *resend.Client {
	if h == nil {
		return nil
	}
	if h.emailService != nil {
		if c := h.emailService.ResendClient(); c != nil && c.Configured() {
			return c
		}
	}
	if h.config == nil {
		return nil
	}
	return resend.NewClient(resend.Config{APIKey: h.config.EmailAPIKey})
}

func (h *Handler) handleResendReadOperation(ctx context.Context, provider, action, operation string, req operatorOperationRequest) operatorOperationResponse {
	client := h.resendClient()
	switch action {
	case "credentials":
		return h.handleResendCredentialsReadOperation(provider, action, operation)
	case "domains":
		if client == nil || !client.Configured() {
			return operatorReadUnavailable(operation, provider, action, "ENCLII_RESEND_API_KEY is not configured")
		}
		return h.handleResendDomainsRead(ctx, operation, provider, action, req, client)
	case "domain":
		if client == nil || !client.Configured() {
			return operatorReadUnavailable(operation, provider, action, "ENCLII_RESEND_API_KEY is not configured")
		}
		return h.handleResendDomainRead(ctx, operation, provider, action, req, client)
	case "emails":
		if client == nil || !client.Configured() {
			return operatorReadUnavailable(operation, provider, action, "ENCLII_RESEND_API_KEY is not configured")
		}
		return h.handleResendEmailsRead(ctx, operation, provider, action, req, client)
	default:
		return operatorReadUnavailable(operation, provider, action, "resend read adapter is not wired for this operation")
	}
}

func (h *Handler) handleResendCredentialsReadOperation(provider, action, operation string) operatorOperationResponse {
	keyPresent := h != nil && h.config != nil && strings.TrimSpace(h.config.EmailAPIKey) != ""
	fromAddress := ""
	fromName := ""
	enabled := false
	if h != nil && h.emailService != nil {
		fromAddress = h.emailService.FromEmail()
		fromName = h.emailService.FromName()
		enabled = h.emailService.IsEnabled()
	}
	data := gin.H{
		"configured":          keyPresent && enabled,
		"apiKeyPresent":       keyPresent,
		"fromAddress":         fromAddress,
		"fromName":            fromName,
		"secretValuesExposed": false,
	}
	if !keyPresent {
		return operatorOperationResponse{
			OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
			Operation:   operation,
			Status:      "adapter_unconfigured",
			DryRun:      true,
			Summary:     "resend.credentials readiness is incomplete",
			Data:        data,
			Warnings:    []string{"missing ENCLII_RESEND_API_KEY"},
			Next:        []string{"backfill secret/enclii/resend_api_key in Vault", "sync enclii-resend-api-key ExternalSecret", "rerun providers.resend.credentials"},
		}
	}
	return operatorReadSuccess(operation, provider, action, data)
}

func (h *Handler) handleResendDomainsRead(ctx context.Context, operation, provider, action string, req operatorOperationRequest, client *resend.Client) operatorOperationResponse {
	domains, err := client.ListDomains(ctx)
	if err != nil {
		return operatorReadFailed(operation, provider, action, err)
	}
	tenantFilter := strings.TrimSpace(req.Scope["tenant"])
	target := strings.ToLower(strings.TrimSpace(operationTarget(req)))
	filtered := make([]gin.H, 0, len(domains))
	for _, d := range domains {
		if target != "" && !strings.EqualFold(d.Name, target) {
			continue
		}
		tid := ecosystem.TenantFromDomain(d.Name)
		if tenantFilter != "" && string(tid) != tenantFilter {
			continue
		}
		filtered = append(filtered, gin.H{
			"id":     d.ID,
			"name":   d.Name,
			"status": d.Status,
			"region": d.Region,
			"tenant": tid,
			"sender": ecosystem.DefaultSenderForTenant(tid),
		})
	}
	return operatorReadSuccess(operation, provider, action, gin.H{
		"domains": filtered,
		"count":   len(filtered),
	})
}

func (h *Handler) handleResendDomainRead(ctx context.Context, operation, provider, action string, req operatorOperationRequest, client *resend.Client) operatorOperationResponse {
	target := resendDomainTarget(req)
	if target == "" {
		return operatorOperationResponse{
			OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
			Operation:   operation,
			Status:      "invalid_request",
			DryRun:      true,
			Summary:     "resend.domain requires args.target (apex domain)",
			Warnings:    []string{"missing args.target"},
		}
	}
	domain, err := client.GetDomainByName(ctx, target)
	if err != nil {
		return operatorReadFailed(operation, provider, action, err)
	}
	if domain == nil {
		return operatorReadSuccess(operation, provider, action, gin.H{
			"target": target,
			"domain": nil,
			"tenant": ecosystem.TenantFromDomain(target),
		})
	}
	full, err := client.GetDomain(ctx, domain.ID)
	if err != nil {
		return operatorReadFailed(operation, provider, action, err)
	}
	return operatorReadSuccess(operation, provider, action, gin.H{
		"target": target,
		"domain": full,
		"tenant": ecosystem.TenantFromDomain(target),
		"sender": ecosystem.DefaultSenderForTenant(ecosystem.TenantFromDomain(target)),
	})
}

func (h *Handler) handleResendEmailsRead(ctx context.Context, operation, provider, action string, req operatorOperationRequest, client *resend.Client) operatorOperationResponse {
	domain := resendDomainTarget(req)
	if domain == "" {
		domain = strings.TrimSpace(req.Args["domain"])
	}
	emails, err := client.ListEmails(ctx, domain)
	if err != nil {
		return operatorReadFailed(operation, provider, action, err)
	}
	return operatorReadSuccess(operation, provider, action, gin.H{
		"domain": domain,
		"emails": emails,
		"count":  len(emails),
	})
}

func resendDomainTarget(req operatorOperationRequest) string {
	return strings.ToLower(strings.TrimSpace(operationTarget(req)))
}

func (h *Handler) handleResendDomainAddApplyDryRun(ctx context.Context, operation string, req operatorOperationRequest) operatorOperationResponse {
	target := resendDomainTarget(req)
	data := gin.H{"target": target, "region": ecosystem.ResendRegionForDomain(target), "tenant": ecosystem.TenantFromDomain(target)}
	steps := resendApplySteps()
	if target == "" {
		return operatorOperationResponse{
			OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
			Operation:   operation,
			Status:      "invalid_request",
			DryRun:      true,
			Summary:     "resend.domain-add-apply requires args.target",
			Data:        data,
			Steps:       steps,
			Warnings:    []string{"missing args.target"},
		}
	}
	client := h.resendClient()
	if client == nil || !client.Configured() {
		return operatorOperationResponse{
			OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
			Operation:   operation,
			Status:      "adapter_unconfigured",
			DryRun:      true,
			Summary:     "resend.domain-add-apply cannot run until ENCLII_RESEND_API_KEY is configured",
			Data:        data,
			Steps:       steps,
			Warnings:    []string{"ENCLII_RESEND_API_KEY missing"},
		}
	}
	existing, err := client.GetDomainByName(ctx, target)
	if err != nil {
		return operatorOperationResponse{
			OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
			Operation:   operation,
			Status:      "provider_read_failed",
			DryRun:      true,
			Summary:     fmt.Sprintf("failed to read Resend domain state for %s", target),
			Data:        data,
			Steps:       steps,
			Warnings:    []string{err.Error()},
		}
	}
	mutation := "create"
	if existing != nil {
		mutation = "noop"
		data["existingDomain"] = existing
	}
	data["mutation"] = mutation
	data["can_apply"] = mutation == "create"
	return operatorOperationResponse{
		OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
		Operation:   operation,
		Status:      "ready_to_apply",
		DryRun:      true,
		Summary:     fmt.Sprintf("resend.domain-add-apply dry-run completed for %s", target),
		Data:        data,
		Steps:       steps,
		Next:        []string{"rerun with --apply and a reason to create the Resend domain"},
	}
}

func (h *Handler) handleResendDomainAddApply(ctx context.Context, operation string, req operatorOperationRequest) (operatorOperationResponse, int) {
	dry := h.handleResendDomainAddApplyDryRun(ctx, operation, req)
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
				Summary:     fmt.Sprintf("Resend domain %s already exists", resendDomainTarget(req)),
				Data:        data,
				Steps:       resendApplyStepsCompleted(),
			}, http.StatusOK
		}
	}
	target := resendDomainTarget(req)
	region := ecosystem.ResendRegionForDomain(target)
	created, err := h.resendClient().CreateDomain(ctx, target, region)
	if err != nil {
		return operatorOperationResponse{
			OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
			Operation:   operation,
			Status:      "provider_apply_failed",
			DryRun:      false,
			Summary:     fmt.Sprintf("failed to create Resend domain %s", target),
			Warnings:    []string{err.Error()},
		}, http.StatusBadGateway
	}
	return operatorOperationResponse{
		OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
		Operation:   operation,
		Status:      "succeeded",
		DryRun:      false,
		Summary:     fmt.Sprintf("created Resend domain %s through Enclii", target),
		Data:        gin.H{"domain": created, "target": target},
		Steps:       resendApplyStepsCompleted(),
		Next:        []string{"run providers.resend.domain-dns-apply", "run providers.resend.domain-verify-apply"},
	}, http.StatusAccepted
}

func (h *Handler) handleResendDomainVerifyApplyDryRun(ctx context.Context, operation string, req operatorOperationRequest) operatorOperationResponse {
	target := resendDomainTarget(req)
	data := gin.H{"target": target}
	steps := resendApplySteps()
	if target == "" {
		return operatorOperationResponse{
			OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
			Operation:   operation,
			Status:      "invalid_request",
			DryRun:      true,
			Summary:     "resend.domain-verify-apply requires args.target",
			Data:        data,
			Warnings:    []string{"missing args.target"},
		}
	}
	client := h.resendClient()
	if client == nil || !client.Configured() {
		return operatorOperationResponse{
			OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
			Operation:   operation,
			Status:      "adapter_unconfigured",
			DryRun:      true,
			Summary:     "resend.domain-verify-apply requires ENCLII_RESEND_API_KEY",
			Data:        data,
			Steps:       steps,
		}
	}
	domain, err := client.GetDomainByName(ctx, target)
	if err != nil || domain == nil {
		msg := "domain not found in Resend"
		if err != nil {
			msg = err.Error()
		}
		return operatorOperationResponse{
			OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
			Operation:   operation,
			Status:      "provider_read_failed",
			DryRun:      true,
			Summary:     fmt.Sprintf("cannot verify %s in Resend", target),
			Data:        data,
			Warnings:    []string{msg},
		}
	}
	data["domainId"] = domain.ID
	data["status"] = domain.Status
	data["can_apply"] = true
	data["mutation"] = "verify"
	return operatorOperationResponse{
		OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
		Operation:   operation,
		Status:      "ready_to_apply",
		DryRun:      true,
		Summary:     fmt.Sprintf("resend.domain-verify-apply dry-run completed for %s", target),
		Data:        data,
		Steps:       steps,
	}
}

func (h *Handler) handleResendDomainVerifyApply(ctx context.Context, operation string, req operatorOperationRequest) (operatorOperationResponse, int) {
	target := resendDomainTarget(req)
	domain, err := h.resendClient().GetDomainByName(ctx, target)
	if err != nil || domain == nil {
		return operatorOperationResponse{
			OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
			Operation:   operation,
			Status:      "provider_read_failed",
			DryRun:      false,
			Summary:     fmt.Sprintf("Resend domain %s not found", target),
		}, http.StatusBadGateway
	}
	verified, err := h.resendClient().VerifyDomain(ctx, domain.ID)
	if err != nil {
		return operatorOperationResponse{
			OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
			Operation:   operation,
			Status:      "provider_apply_failed",
			DryRun:      false,
			Summary:     fmt.Sprintf("failed to verify Resend domain %s", target),
			Warnings:    []string{err.Error()},
		}, http.StatusBadGateway
	}
	return operatorOperationResponse{
		OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
		Operation:   operation,
		Status:      "succeeded",
		DryRun:      false,
		Summary:     fmt.Sprintf("triggered Resend verification for %s", target),
		Data:        gin.H{"domain": verified},
		Steps:       resendApplyStepsCompleted(),
	}, http.StatusAccepted
}

func resendApplySteps() []operatorOperationStep {
	return []operatorOperationStep{
		{Name: "authorize", Status: "planned", Detail: "check caller RBAC and require reason on apply"},
		{Name: "load-state", Status: "planned", Detail: "load Resend domain and DNS requirements"},
		{Name: "diff", Status: "planned", Detail: "compare desired state with live Resend/Cloudflare"},
		{Name: "audit", Status: "planned", Detail: "record operation reason before mutation"},
	}
}

func resendApplyStepsCompleted() []operatorOperationStep {
	return []operatorOperationStep{
		{Name: "authorize", Status: "completed"},
		{Name: "load-state", Status: "completed"},
		{Name: "diff", Status: "completed"},
		{Name: "audit", Status: "completed"},
	}
}
