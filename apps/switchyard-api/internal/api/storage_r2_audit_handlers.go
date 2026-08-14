package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/provisioning"
)

// R2 credential drift audit, exposed as the read-only operator action
// `ops.storage.r2-audit` and surfaced by `enclii ops storage r2-audit` and
// `enclii admin ga-verify`.
//
// It exists because of a specific production failure: a service was found
// holding a *copy of another service's* R2 token — pointed at that other
// service's bucket — because provisioning had told it STORAGE_BACKEND=r2
// without ever minting it credentials, and somebody made storage work by hand.
// Every signal below is independent, so no single missing annotation hides it.

// readR2CredentialDrift scans Secrets for R2 configuration and reports drift.
// No secret material is read into the response: access key IDs are reduced to
// fingerprints, which is enough to prove two namespaces share one credential.
func (h *Handler) readR2CredentialDrift(ctx context.Context, req operatorOperationRequest) (map[string]any, error) {
	kube := h.opsKubeClient()
	if kube == nil {
		return nil, fmt.Errorf("kubernetes typed client is not configured on switchyard-api")
	}

	var namespaces []string
	if ns := strings.TrimSpace(operationNamespace(req, "")); ns != "" {
		namespaces = []string{ns}
	}

	bindings, err := provisioning.NewR2Auditor(kube).Scan(ctx, namespaces)
	if err != nil {
		return nil, err
	}

	if target := strings.TrimSpace(operationTarget(req)); target != "" {
		filtered := make([]provisioning.R2SecretBinding, 0, len(bindings))
		for _, b := range bindings {
			if b.Namespace == target || b.SecretName == target || b.Bucket == target {
				filtered = append(filtered, b)
			}
		}
		bindings = filtered
	}

	findings := provisioning.AuditR2Bindings(bindings)
	critical := provisioning.CountCritical(findings)

	scope := "all namespaces"
	if len(namespaces) > 0 {
		scope = strings.Join(namespaces, ", ")
	}

	return map[string]any{
		"scope":          scope,
		"bindings":       bindings,
		"findings":       findings,
		"binding_count":  len(bindings),
		"finding_count":  len(findings),
		"critical_count": critical,
	}, nil
}

// handleOpsStorageR2Audit renders the drift scan as an operator response. A
// critical finding downgrades the operation status so `admin ga-verify` and
// any Selva-driven gate treat it as a failure rather than a passing read.
func (h *Handler) handleOpsStorageR2Audit(ctx context.Context, operation, domain, action string, req operatorOperationRequest) operatorOperationResponse {
	data, err := h.readR2CredentialDrift(ctx, req)
	if err != nil {
		return operatorReadFailed(operation, domain, action, err)
	}

	resp := operatorReadSuccess(operation, domain, action, data)

	critical, _ := data["critical_count"].(int)
	findingCount, _ := data["finding_count"].(int)
	bindingCount, _ := data["binding_count"].(int)

	switch {
	case critical > 0:
		resp.Status = "failed"
		resp.Summary = fmt.Sprintf("%d critical R2 credential finding(s) across %d binding(s)", critical, bindingCount)
	case findingCount > 0:
		resp.Summary = fmt.Sprintf("%d non-critical R2 credential finding(s) across %d binding(s)", findingCount, bindingCount)
	default:
		resp.Summary = fmt.Sprintf("%d R2 binding(s) checked; every service holds its own complete credentials", bindingCount)
	}

	if findings, ok := data["findings"].([]provisioning.R2Finding); ok {
		for _, f := range findings {
			resp.Warnings = append(resp.Warnings,
				fmt.Sprintf("[%s] %s/%s %s: %s", f.Severity, f.Namespace, f.Secret, f.Kind, f.Message))
		}
	}

	if critical > 0 {
		resp.Next = []string{
			"enclii buckets ls --project <project> to inspect one project's bindings",
			"enclii buckets create <bucket> --project <project> --rotate to mint that service its own scoped credentials",
			"revoke any shared Cloudflare R2 token once every holder has been re-provisioned",
		}
	}
	return resp
}
