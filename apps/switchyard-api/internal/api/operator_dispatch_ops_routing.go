package api

import "context"

// Routing operator-operation dispatch: the ops verbs that decide where a
// hostname's traffic goes (domains.reconcile, junctions.rebind).
//
// Kept out of operator_contract_handlers.go, which is past the repo's
// 800-line ceiling; the same reason operator_dispatch_cloudflare.go exists.
// Both functions return ok=false for anything they do not own, so the caller
// falls through to the generic contract response.

func (h *Handler) opsRoutingDryRunDispatch(ctx context.Context, prefix, domain, action, operation string, req operatorOperationRequest) (operatorOperationResponse, bool) {
	if prefix != "ops" {
		return operatorOperationResponse{}, false
	}
	switch domain + "." + action {
	case "domains.reconcile":
		return h.handleOpsDomainsReconcileDryRun(ctx, operation, req), true
	case "junctions.rebind":
		return h.handleOpsJunctionsRebindDryRun(ctx, operation, req), true
	}
	return operatorOperationResponse{}, false
}

func (h *Handler) opsRoutingApplyDispatch(ctx context.Context, prefix, domain, action, operation string, req operatorOperationRequest) (operatorOperationResponse, int, bool) {
	if prefix != "ops" {
		return operatorOperationResponse{}, 0, false
	}
	switch domain + "." + action {
	case "domains.reconcile":
		resp, statusCode := h.handleOpsDomainsReconcileApply(ctx, operation, req)
		return resp, statusCode, true
	case "junctions.rebind":
		resp, statusCode := h.handleOpsJunctionsRebindApply(ctx, operation, req)
		return resp, statusCode, true
	}
	return operatorOperationResponse{}, 0, false
}
