package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/cloudflare"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/ecosystem"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/resend"
)

type resendDNSPlanItem struct {
	FQDN       string `json:"fqdn"`
	RecordType string `json:"type"`
	Content    string `json:"content"`
	Priority   int    `json:"priority,omitempty"`
	Proxied    bool   `json:"proxied"`
	Mutation   string `json:"mutation"`
}

func (h *Handler) handleResendDomainDNSApplyDryRun(ctx context.Context, operation string, req operatorOperationRequest) operatorOperationResponse {
	target := resendDomainTarget(req)
	data := gin.H{"target": target}
	steps := resendApplySteps()
	if target == "" {
		return operatorOperationResponse{
			OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
			Operation:   operation,
			Status:      "invalid_request",
			DryRun:      true,
			Summary:     "resend.domain-dns-apply requires args.target",
			Data:        data,
			Warnings:    []string{"missing args.target"},
		}
	}
	client := h.resendClient()
	cfClient := h.cloudflareDNSApplyClient()
	if client == nil || !client.Configured() {
		return operatorOperationResponse{
			OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
			Operation:   operation,
			Status:      "adapter_unconfigured",
			DryRun:      true,
			Summary:     "resend.domain-dns-apply requires ENCLII_RESEND_API_KEY",
			Data:        data,
			Steps:       steps,
		}
	}
	if cfClient == nil {
		return operatorOperationResponse{
			OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
			Operation:   operation,
			Status:      "adapter_unconfigured",
			DryRun:      true,
			Summary:     "resend.domain-dns-apply requires Cloudflare domain sync client",
			Data:        data,
			Steps:       steps,
			Warnings:    []string{"configure ENCLII_CLOUDFLARE_* on switchyard-api"},
		}
	}
	plan, err := h.planResendDNSRecords(ctx, client, cfClient, target)
	if err != nil {
		return operatorOperationResponse{
			OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
			Operation:   operation,
			Status:      "provider_read_failed",
			DryRun:      true,
			Summary:     fmt.Sprintf("failed to plan DNS for %s", target),
			Data:        data,
			Warnings:    []string{err.Error()},
		}
	}
	data["records"] = plan
	data["can_apply"] = len(plan) > 0
	return operatorOperationResponse{
		OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
		Operation:   operation,
		Status:      "ready_to_apply",
		DryRun:      true,
		Summary:     fmt.Sprintf("resend.domain-dns-apply dry-run completed for %s (%d records)", target, len(plan)),
		Data:        data,
		Steps:       steps,
		Next:        []string{"rerun with --apply and a reason to push DNS records to Cloudflare"},
	}
}

func (h *Handler) handleResendDomainDNSApply(ctx context.Context, operation string, req operatorOperationRequest) (operatorOperationResponse, int) {
	target := resendDomainTarget(req)
	client := h.resendClient()
	cfClient := h.cloudflareDNSApplyClient()
	if client == nil || cfClient == nil {
		return operatorOperationResponse{
			OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
			Operation:   operation,
			Status:      "adapter_unconfigured",
			DryRun:      false,
			Summary:     "resend.domain-dns-apply adapters not configured",
		}, http.StatusServiceUnavailable
	}
	plan, err := h.planResendDNSRecords(ctx, client, cfClient, target)
	if err != nil {
		return operatorOperationResponse{
			OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
			Operation:   operation,
			Status:      "provider_read_failed",
			DryRun:      false,
			Summary:     fmt.Sprintf("failed to plan DNS for %s", target),
			Warnings:    []string{err.Error()},
		}, http.StatusBadGateway
	}
	zone, err := cfClient.FindZoneForDomain(ctx, target)
	if err != nil {
		return operatorOperationResponse{
			OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
			Operation:   operation,
			Status:      "blocked_by_dns_authority",
			DryRun:      false,
			Summary:     fmt.Sprintf("Cloudflare zone not available for %s", target),
			Warnings:    []string{err.Error()},
		}, http.StatusFailedDependency
	}
	applied := make([]resendDNSPlanItem, 0, len(plan))
	for _, item := range plan {
		if item.Mutation == "noop" {
			applied = append(applied, item)
			continue
		}
		existing, err := cfClient.GetDNSRecordByType(ctx, item.FQDN, item.RecordType)
		if err != nil {
			return operatorOperationResponse{
				OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
				Operation:   operation,
				Status:      "provider_apply_failed",
				DryRun:      false,
				Summary:     fmt.Sprintf("failed reading Cloudflare DNS for %s", item.FQDN),
				Warnings:    []string{err.Error()},
			}, http.StatusBadGateway
		}
		if existing != nil {
			_, err = cfClient.UpdateDNSRecordInZone(ctx, zone.ID, *existing, item.Content, item.Proxied)
		} else {
			_, err = cfClient.CreateDNSRecordInZoneWithPriority(ctx, zone.ID, item.FQDN, item.RecordType, item.Content, item.Proxied, item.Priority)
		}
		if err != nil {
			return operatorOperationResponse{
				OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
				Operation:   operation,
				Status:      "provider_apply_failed",
				DryRun:      false,
				Summary:     fmt.Sprintf("failed applying Cloudflare DNS for %s", item.FQDN),
				Warnings:    []string{err.Error()},
			}, http.StatusBadGateway
		}
		applied = append(applied, item)
	}
	return operatorOperationResponse{
		OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
		Operation:   operation,
		Status:      "succeeded",
		DryRun:      false,
		Summary:     fmt.Sprintf("applied %d Resend DNS records for %s via Cloudflare", len(applied), target),
		Data:        gin.H{"target": target, "records": applied},
		Steps:       resendApplyStepsCompleted(),
		Next:        []string{"run providers.resend.domain-verify-apply", "poll public DNS until verified"},
	}, http.StatusAccepted
}

func (h *Handler) planResendDNSRecords(ctx context.Context, client *resend.Client, cfClient *cloudflare.Client, apex string) ([]resendDNSPlanItem, error) {
	domain, err := client.GetDomainByName(ctx, apex)
	if err != nil {
		return nil, err
	}
	if domain == nil {
		return nil, fmt.Errorf("domain %s not found in Resend; run domain-add-apply first", apex)
	}
	full, err := client.GetDomain(ctx, domain.ID)
	if err != nil {
		return nil, err
	}
	records := full.Records
	if len(records) == 0 {
		records = domain.Records
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("no DNS records returned from Resend for %s", apex)
	}
	plan := make([]resendDNSPlanItem, 0, len(records))
	for _, rec := range records {
		fqdn := resendRecordFQDN(apex, rec.Name)
		content := strings.Trim(rec.Value, `"`)
		proxied := false
		item := resendDNSPlanItem{
			FQDN:       fqdn,
			RecordType: strings.ToUpper(rec.Type),
			Content:    content,
			Priority:   rec.Priority,
			Proxied:    proxied,
			Mutation:   "create",
		}
		existing, err := cfClient.GetDNSRecordByType(ctx, fqdn, item.RecordType)
		if err != nil {
			return nil, err
		}
		if existing != nil && existing.Content == content {
			item.Mutation = "noop"
		} else if existing != nil {
			item.Mutation = "update"
		}
		plan = append(plan, item)
	}
	return plan, nil
}

func resendRecordFQDN(apex, name string) string {
	name = strings.TrimSpace(strings.TrimSuffix(name, "."))
	if name == "" || name == "@" {
		return apex
	}
	lower := strings.ToLower(name)
	apexLower := strings.ToLower(apex)
	if lower == apexLower || strings.HasSuffix(lower, "."+apexLower) {
		return name
	}
	return name + "." + apex
}

func (h *Handler) handleResendSendTestApplyDryRun(ctx context.Context, operation string, req operatorOperationRequest) operatorOperationResponse {
	to := strings.TrimSpace(req.Args["to"])
	target := resendDomainTarget(req)
	data := gin.H{"to": to, "target": target}
	if to == "" {
		return operatorOperationResponse{
			OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
			Operation:   operation,
			Status:      "invalid_request",
			DryRun:      true,
			Summary:     "resend.send-test-apply requires args.to",
			Data:        data,
			Warnings:    []string{"missing args.to recipient"},
		}
	}
	client := h.resendClient()
	if client == nil || !client.Configured() {
		return operatorOperationResponse{
			OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
			Operation:   operation,
			Status:      "adapter_unconfigured",
			DryRun:      true,
			Summary:     "resend.send-test-apply requires ENCLII_RESEND_API_KEY",
			Data:        data,
		}
	}
	if h.emailService == nil {
		return operatorOperationResponse{
			OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
			Operation:   operation,
			Status:      "adapter_unconfigured",
			DryRun:      true,
			Summary:     "email service is not wired",
			Data:        data,
		}
	}
	fromName, fromEmail := h.resendSendTestSender(target)
	data["from"] = fmt.Sprintf("%s <%s>", fromName, fromEmail)
	data["from_email"] = fromEmail
	data["can_apply"] = true
	return operatorOperationResponse{
		OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
		Operation:   operation,
		Status:      "ready_to_apply",
		DryRun:      true,
		Summary:     fmt.Sprintf("resend.send-test-apply dry-run ready for %s", to),
		Data:        data,
		Steps:       resendApplySteps(),
	}
}

// resendSendTestSender is the sender of a send-test, shared by the dry-run
// and the real send so the preview is what is sent: the default sender
// address of the tenant that owns args.target (ecosystem tenants.json, e.g.
// noreply@creatumundo.mx for creatumundo.mx), falling back to the email
// service's configured sender when there is no target or the domain belongs
// to no tenant with a default sender. The display name is always the email
// service's. Callers must have checked h.emailService != nil.
func (h *Handler) resendSendTestSender(target string) (fromName, fromEmail string) {
	fromName = h.emailService.FromName()
	fromEmail = h.emailService.FromEmail()
	if target != "" {
		if sender := ecosystem.DefaultSenderForTenant(ecosystem.TenantFromDomain(target)); sender != "" {
			fromEmail = sender
		}
	}
	return fromName, fromEmail
}

// handleResendSendTestApply sends the test email. It runs the dry-run first
// and stops on the same conditions (missing args.to is a 400; no Resend API
// key or no email service is a 503 adapter_unconfigured), so it never calls
// an unconfigured client, and it sends from the sender the dry-run showed.
func (h *Handler) handleResendSendTestApply(ctx context.Context, operation string, req operatorOperationRequest) (operatorOperationResponse, int) {
	dry := h.handleResendSendTestApplyDryRun(ctx, operation, req)
	switch dry.Status {
	case "ready_to_apply":
	case "invalid_request":
		dry.DryRun = false
		return dry, http.StatusBadRequest
	case "adapter_unconfigured":
		dry.DryRun = false
		return dry, http.StatusServiceUnavailable
	default:
		dry.DryRun = false
		return dry, http.StatusInternalServerError
	}
	to := strings.TrimSpace(req.Args["to"])
	fromName, fromEmail := h.resendSendTestSender(resendDomainTarget(req))
	subject := "Enclii Resend send-test"
	body := fmt.Sprintf("This is a send-test from Enclii Provider Hub at %s.", time.Now().UTC().Format(time.RFC3339))
	_, err := h.resendClient().SendEmail(ctx, resend.SendEmailRequest{
		From:    fmt.Sprintf("%s <%s>", fromName, fromEmail),
		To:      []string{to},
		Subject: subject,
		Text:    body,
	})
	if err != nil {
		return operatorOperationResponse{
			OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
			Operation:   operation,
			Status:      "provider_apply_failed",
			DryRun:      false,
			Summary:     fmt.Sprintf("failed to send test email to %s", to),
			Warnings:    []string{err.Error()},
		}, http.StatusBadGateway
	}
	return operatorOperationResponse{
		OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
		Operation:   operation,
		Status:      "succeeded",
		DryRun:      false,
		Summary:     fmt.Sprintf("sent Resend test email to %s", to),
		Data:        gin.H{"to": to, "from": fromEmail},
		Steps:       resendApplyStepsCompleted(),
	}, http.StatusAccepted
}
