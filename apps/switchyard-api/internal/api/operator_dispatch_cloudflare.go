package api

import "context"

// Cloudflare operator-operation dispatch.
//
// Extracted from operator_contract_handlers.go, which had reached the 800-line
// ceiling the repo's pre-commit hook enforces — adding zone-settings-apply
// tipped it over. Grouping one provider's routes together is the change that
// file wanted anyway: the dispatch was a flat run of near-identical
// `prefix == "providers" && domain == "cloudflare" && action == "..."` lines
// repeated for every provider, and the shared prefix is now tested once.
//
// Both functions return ok=false for an unrecognised action so the caller can
// fall through to the generic contract response. They must NOT return a
// zero-valued response with ok=true; an unknown action would then read as a
// successful no-op.

func (h *Handler) cloudflareDryRunDispatch(ctx context.Context, action, operation string, req operatorOperationRequest) (operatorOperationResponse, bool) {
	switch action {
	case "dns-apply":
		return h.handleProviderCloudflareDNSApplyDryRun(ctx, operation, req), true
	case "zone-add-apply":
		return h.handleProviderCloudflareZoneAddApplyDryRun(ctx, operation, req), true
	case "zone-settings-apply":
		return h.handleProviderCloudflareZoneSettingsApplyDryRun(ctx, operation, req), true
	case "tunnels-apply":
		return h.handleProviderCloudflareTunnelsApplyDryRun(ctx, operation, req), true
	}
	return operatorOperationResponse{}, false
}

func (h *Handler) cloudflareApplyDispatch(ctx context.Context, action, operation string, req operatorOperationRequest) (operatorOperationResponse, int, bool) {
	switch action {
	case "dns-apply":
		resp, statusCode := h.handleProviderCloudflareDNSApply(ctx, operation, req)
		return resp, statusCode, true
	case "zone-add-apply":
		resp, statusCode := h.handleProviderCloudflareZoneAddApply(ctx, operation, req)
		return resp, statusCode, true
	case "zone-settings-apply":
		resp, statusCode := h.handleProviderCloudflareZoneSettingsApply(ctx, operation, req)
		return resp, statusCode, true
	case "tunnels-apply":
		// Preserves the original guard: the tunnels apply path is only wired
		// when the routes service is configured. Without this it would return
		// ok=true and dispatch into a nil service.
		if h.tunnelRoutesService != nil {
			resp, statusCode := h.handleProviderCloudflareTunnelsApply(ctx, operation, req)
			return resp, statusCode, true
		}
	}
	return operatorOperationResponse{}, 0, false
}
