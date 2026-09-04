package api

// ops.secrets.provision-kalya-feed — the operator verb for the kalya standing
// feed credential.
//
// Mirrors ops.secrets.vault-backfill in shape (dry run plans, apply mutates,
// reason required on apply) and provision-oidc in intent (a credential the
// ecosystem needs is obtained and filed by the control plane, not carried by a
// human). The operator supplies a tenant, a consumer list, and a reason. No
// token of any kind is an input, and none is an output.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
)

// kalyaFeedRequestFromOperation reads the operator contract's args into the
// provisioner's inputs.
func (h *Handler) kalyaFeedRequestFromOperation(req operatorOperationRequest) (kalyaFeedProvisionRequest, error) {
	tenant := operationArg(req, "tenant", "tenant_slug", "tenant-slug")
	if tenant == "" && req.Scope != nil {
		tenant = strings.TrimSpace(req.Scope["tenant"])
	}

	consumersRaw := operationArg(req, "consumers", "consumer")
	consumers := strings.Split(consumersRaw, ",")

	origin := operationArg(req, "kalya_origin", "kalya-origin", "origin")
	if origin == "" {
		origin = h.resolveKalyaOrigin()
	}

	rotate := strings.EqualFold(operationArg(req, "rotate"), "true")

	return resolveKalyaFeedRequest(tenant, consumers, origin, rotate)
}

// resolveKalyaOrigin prefers kalya's own service record over the compiled-in
// default, so a staging control plane provisioning against a staging kalya does
// not need the origin spelled out on every invocation.
func (h *Handler) resolveKalyaOrigin() string {
	if h == nil || h.repos == nil || h.repos.CustomDomains == nil || h.repos.Services == nil {
		return defaultKalyaOrigin
	}
	service, err := h.repos.Services.GetByName("kalya")
	if err != nil || service == nil {
		return defaultKalyaOrigin
	}
	domains, err := h.repos.CustomDomains.GetByServiceID(context.Background(), service.ID.String())
	if err != nil {
		return defaultKalyaOrigin
	}
	// Verified production domains only. An unverified row is an aspiration, and
	// sending a mint request bearing kalya's internal API key at a hostname that
	// is not yet confirmed to be kalya is exactly the mistake worth not making.
	for i := range domains {
		if domains[i].Verified && strings.TrimSpace(domains[i].Domain) != "" {
			return "https://" + strings.TrimSpace(domains[i].Domain)
		}
	}
	return defaultKalyaOrigin
}

func (h *Handler) handleOpsSecretsProvisionKalyaFeedDryRun(ctx context.Context, operation string, req operatorOperationRequest) operatorOperationResponse {
	operationID := fmt.Sprintf("op_%d", time.Now().UTC().UnixNano())

	resolved, err := h.kalyaFeedRequestFromOperation(req)
	if err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "invalid_request",
			DryRun:      true,
			Summary:     "ops.secrets.provision-kalya-feed could not read its inputs",
			Warnings:    []string{err.Error()},
		}
	}

	steps := []operatorOperationStep{
		{Name: "authorize", Status: "planned", Detail: "verify admin RBAC and audit reason on apply"},
		{Name: "load-state", Status: "planned", Detail: "read the consumer Vault paths to decide what is already provisioned"},
		{Name: "diff", Status: "planned", Detail: "per-consumer write/skip decision; skip when the properties already exist and --rotate was not given"},
		{Name: "mint", Status: "planned", Detail: "ask kalya to mint a feed token using the internal API key read from Vault server-side"},
		{Name: "vault-write", Status: "planned", Detail: "write the consumer properties to Vault; the token is never returned"},
		{Name: "audit", Status: "planned", Detail: "record operation_id and reason without any secret value"},
	}

	if h.vaultClient == nil || !h.vaultClient.IsEnabled() {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "adapter_unconfigured",
			DryRun:      true,
			Summary:     "ops.secrets.provision-kalya-feed cannot run until Vault is configured",
			Data:        kalyaFeedPlanData(resolved, nil),
			Steps:       steps,
			Warnings:    []string{"Vault is not configured for this build"},
		}
	}

	planned, _, planErr := planKalyaFeedProvision(ctx, h.vaultClient, resolved)
	if planErr != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "provider_read_failed",
			DryRun:      true,
			Summary:     "ops.secrets.provision-kalya-feed could not read the consumer Vault paths",
			Data:        kalyaFeedPlanData(resolved, nil),
			Steps:       steps,
			Warnings:    []string{planErr.Error()},
		}
	}

	writes, skips, failures := summarizeKalyaFeedPlan(planned)
	warnings := []string{}
	for _, entry := range planned {
		if entry.Error != "" {
			warnings = append(warnings, entry.Error)
		}
	}
	if writes == 0 && failures == 0 {
		warnings = append(warnings,
			"every consumer already carries this tenant's feed properties; an apply would mint nothing — pass rotate=true to replace the token deliberately")
	}

	return operatorOperationResponse{
		OperationID: operationID,
		Operation:   operation,
		Status:      "ready_to_apply",
		DryRun:      true,
		Summary: fmt.Sprintf(
			"ops.secrets.provision-kalya-feed dry-run for tenant %s: %d consumer(s) to write, %d already provisioned, %d unreadable",
			resolved.Tenant, writes, skips, failures),
		Data:     kalyaFeedPlanData(resolved, planned),
		Steps:    steps,
		Warnings: warnings,
		Next:     []string{"rerun with dry_run=false and a reason to mint and file the feed token"},
	}
}

func (h *Handler) handleOpsSecretsProvisionKalyaFeedApply(ctx context.Context, operation string, req operatorOperationRequest) (operatorOperationResponse, int) {
	operationID := fmt.Sprintf("op_%d", time.Now().UTC().UnixNano())

	resolved, err := h.kalyaFeedRequestFromOperation(req)
	if err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "invalid_request",
			DryRun:      false,
			Summary:     "ops.secrets.provision-kalya-feed could not read its inputs",
			Warnings:    []string{err.Error()},
		}, http.StatusBadRequest
	}

	if h.vaultClient == nil || !h.vaultClient.IsEnabled() {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "adapter_unconfigured",
			DryRun:      false,
			Summary:     "ops.secrets.provision-kalya-feed cannot run until Vault is configured",
			Warnings:    []string{"Vault is not configured for this build"},
		}, http.StatusServiceUnavailable
	}

	outcome, provisionErr := provisionKalyaFeedToken(ctx, h.vaultClient, h.kalyaMinter(), resolved)
	if provisionErr != nil {
		// Logged without any secret value: provisionKalyaFeedToken's errors are
		// constructed to name paths and status codes, never bodies or tokens.
		h.logger.Error(ctx, "kalya feed-token provisioning failed",
			logging.String("tenant", resolved.Tenant),
			logging.String("kalya_origin", resolved.Origin),
			logging.Error("error", provisionErr))
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "failed",
			DryRun:      false,
			Summary:     fmt.Sprintf("failed to provision the kalya feed token for tenant %s", resolved.Tenant),
			Data:        kalyaFeedPlanData(resolved, outcome.Consumers),
			Warnings:    []string{provisionErr.Error()},
		}, http.StatusBadGateway
	}

	writes, skips, failures := summarizeKalyaFeedPlan(outcome.Consumers)
	warnings := []string{
		"the feed token was minted by kalya and written directly to Vault; it is not returned by this API, not logged, and not recorded in the audit trail",
		"kalya's internal API key was read server-side from " + kalyaVaultPath + " and never left the control plane",
	}
	for _, entry := range outcome.Consumers {
		if entry.Error != "" {
			warnings = append(warnings, fmt.Sprintf("%s: %s", entry.Consumer, entry.Error))
		}
	}

	status := "submitted"
	statusCode := http.StatusAccepted
	if failures > 0 {
		status = "partial"
	}

	h.logger.Info(ctx, "kalya feed-token provisioning completed",
		logging.String("tenant", resolved.Tenant),
		logging.String("kalya_origin", resolved.Origin),
		logging.Bool("minted", outcome.Minted),
		logging.Int("consumers_written", writes),
		logging.Int("consumers_skipped", skips),
		logging.Int("consumers_failed", failures))

	return operatorOperationResponse{
		OperationID: operationID,
		Operation:   operation,
		Status:      status,
		DryRun:      false,
		Summary: fmt.Sprintf(
			"provisioned the kalya standing feed for tenant %s: %d consumer(s) written, %d already provisioned, %d failed",
			resolved.Tenant, writes, skips, failures),
		Data: kalyaFeedPlanData(resolved, outcome.Consumers),
		Steps: []operatorOperationStep{
			{Name: "authorize", Status: "completed", Detail: "reason supplied and caller passed endpoint authorization"},
			{Name: "load-state", Status: "completed", Detail: "read the consumer Vault paths"},
			{Name: "diff", Status: "completed", Detail: "decided per consumer whether the properties were already present"},
			{Name: "mint", Status: stepStatus(!outcome.Minted, outcome.Minted), Detail: "kalya minted a feed token, authorized by the internal API key read from Vault"},
			{Name: "vault-write", Status: stepStatus(writes == 0, writes > 0), Detail: "consumer properties merged into Vault without exposing values"},
			{Name: "audit", Status: "completed", Detail: "operation reason recorded; no secret value in the audit record"},
		},
		Warnings: warnings,
		Next: []string{
			"refresh the consuming ExternalSecrets: `enclii ops secrets refresh <name> --namespace <ns> --apply --reason ...`",
			"re-run this operation freely; it is idempotent and mints nothing when the consumers are already provisioned",
		},
	}, statusCode
}

// kalyaMinter returns the live kalya client. A seam, so a test can substitute
// a fake endpoint without a network.
func (h *Handler) kalyaMinter() kalyaFeedTokenMinter {
	if h != nil && h.kalyaFeedMinter != nil {
		return h.kalyaFeedMinter
	}
	return newHTTPKalyaMinter()
}

// kalyaFeedPlanData renders the operator-facing data block. Every field here is
// a path, a property NAME, or a decision — never a property value, because the
// values are the token.
func kalyaFeedPlanData(req kalyaFeedProvisionRequest, outcomes []kalyaFeedConsumerOutcome) map[string]any {
	data := map[string]any{
		"tenant":      req.Tenant,
		"kalyaOrigin": req.Origin,
		"label":       req.Label,
		"consumers":   req.Consumers,
		"rotate":      req.Rotate,
	}
	if outcomes != nil {
		data["plan"] = outcomes
	}
	return data
}

func summarizeKalyaFeedPlan(outcomes []kalyaFeedConsumerOutcome) (writes, skips, failures int) {
	for _, entry := range outcomes {
		switch entry.Action {
		case "write", "rotate":
			writes++
		case "skip":
			skips++
		case "error":
			failures++
		}
	}
	return writes, skips, failures
}
