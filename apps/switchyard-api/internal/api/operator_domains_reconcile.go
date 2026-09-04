package api

// ops.domains.reconcile — the explicit operator verb for "provision the
// hostnames this service's enclii.yaml declares, now".
//
// Every credential this needs (the GitHub token that reads the manifest, the
// Cloudflare token that writes the DNS record and the tunnel route) is already
// held by the control plane. None of them is a parameter, and none of them is
// ever returned. The operator supplies a service name and a reason; that is the
// whole input surface. That is the point of the verb: an operator should not
// have to know a project id, a zone id, a tunnel id, or a Cloudflare token to
// make a declared hostname real.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/manifest"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// declaredDomainSource resolves the manifest to reconcile against. Split out so
// the dry run and the apply read the same manifest the same way, and so a test
// can supply one without GitHub.
type declaredDomainSource struct {
	Service  *types.Service
	Manifest *manifest.EncliiYAML
	// Ref names where the manifest came from, for the audit trail.
	Ref string
	Err error
}

// resolveDeclaredDomainSource loads the service and its enclii.yaml.
//
// `target` is the service NAME, deliberately: the operator knows "nauta-web",
// not a UUID. A git ref may be supplied to reconcile against a specific commit;
// the default branch head is the normal case.
func (h *Handler) resolveDeclaredDomainSource(ctx context.Context, req operatorOperationRequest) declaredDomainSource {
	name := operationTarget(req)
	if name == "" && req.Scope != nil {
		name = strings.TrimSpace(req.Scope["service"])
	}
	if name == "" {
		return declaredDomainSource{Err: fmt.Errorf("missing args.target or scope.service (the service name, e.g. nauta-web)")}
	}

	if h.repos == nil || h.repos.Services == nil {
		return declaredDomainSource{Err: fmt.Errorf("service registry is unavailable")}
	}

	service, err := h.repos.Services.GetByName(name)
	if err != nil {
		return declaredDomainSource{Err: fmt.Errorf("service %q not found: %w", name, err)}
	}

	repoFullName := githubRepoFullName(service.GitRepo)
	if repoFullName == "" {
		return declaredDomainSource{
			Service: service,
			Err:     fmt.Errorf("service %q has no GitHub repository on record, so its enclii.yaml cannot be read", name),
		}
	}

	ref := operationArg(req, "ref", "git_ref", "git-ref")
	token := ""
	if h.config != nil {
		token = h.config.GitHubToken
	}

	parsed := manifest.FetchAndParse(ctx, h.logger, token, repoFullName, ref)
	if parsed == nil {
		return declaredDomainSource{
			Service: service,
			Ref:     ref,
			Err: fmt.Errorf(
				"could not read a usable enclii.yaml from %s: the file is missing, unreadable, or failed to parse", repoFullName),
		}
	}

	return declaredDomainSource{Service: service, Manifest: parsed, Ref: ref}
}

// githubRepoFullName reduces a stored git_repo URL to `owner/name`, which is
// the form the GitHub contents API wants. Returns "" for anything that is not
// recognisably a GitHub URL rather than guessing.
func githubRepoFullName(gitRepo string) string {
	trimmed := strings.TrimSpace(gitRepo)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.TrimSuffix(trimmed, "/")
	trimmed = strings.TrimSuffix(trimmed, ".git")

	for _, prefix := range []string{
		"https://github.com/",
		"http://github.com/",
		"git@github.com:",
		"ssh://git@github.com/",
	} {
		if strings.HasPrefix(trimmed, prefix) {
			trimmed = strings.TrimPrefix(trimmed, prefix)
			break
		}
	}

	parts := strings.Split(trimmed, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

// hostnameScopedDomain returns the single hostname an operator scoped this
// reconcile to, if any.
//
// Hostname scoping matters, and is not a convenience. `cloudflare
// tunnels-apply --project X` reconciles EVERY junction in the project and
// rewrites each one's backend, which on 2026-08-30 clobbered unrelated
// hostnames whose pods live in another namespace. A reconcile aimed at one
// newly declared hostname must touch exactly that hostname.
func hostnameScopedDomain(req operatorOperationRequest) string {
	return canonicalDomain(operationArg(req, "domain", "hostname"))
}

// filterPlanToHostname narrows a plan to one hostname. An empty hostname means
// no narrowing. A hostname the manifest does not declare yields an EMPTY plan,
// never the full one: silently widening a scoped request to everything is the
// exact shape of the clobber this guards against.
func filterPlanToHostname(plan declaredDomainPlan, hostname string) declaredDomainPlan {
	if hostname == "" {
		return plan
	}
	filtered := declaredDomainPlan{Service: plan.Service, Entries: []declaredDomainPlanEntry{}}
	for _, entry := range plan.Entries {
		if entry.Domain == hostname {
			filtered.Entries = append(filtered.Entries, entry)
		}
	}
	return filtered
}

func (h *Handler) handleOpsDomainsReconcileDryRun(ctx context.Context, operation string, req operatorOperationRequest) operatorOperationResponse {
	operationID := fmt.Sprintf("op_%d", time.Now().UTC().UnixNano())
	source := h.resolveDeclaredDomainSource(ctx, req)
	if source.Err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "invalid_request",
			DryRun:      true,
			Summary:     "ops.domains.reconcile could not resolve the service and its enclii.yaml",
			Warnings:    []string{source.Err.Error()},
		}
	}

	host := selectDomainHostService([]*types.Service{source.Service}, source.Manifest)
	scoped := hostnameScopedDomain(req)
	plan := filterPlanToHostname(
		planDeclaredDomains(ctx, h.customDomainChecker(), serviceNameOf(host), source.Manifest), scoped)
	create, ensure, skip := plan.Counts()

	warnings := []string{}
	if scoped != "" && len(plan.Entries) == 0 {
		warnings = append(warnings, fmt.Sprintf(
			"%s is not declared in this service's enclii.yaml; nothing is planned rather than falling back to every declared hostname", scoped))
	}
	if !h.domainEdgeConfigured() {
		warnings = append(warnings,
			"Cloudflare is not wired into this build, so an apply would create no DNS record; the plan is still accurate about what is declared")
	}
	if len(plan.Entries) == 0 {
		warnings = append(warnings, "enclii.yaml declares no domains for this service")
	}

	return operatorOperationResponse{
		OperationID: operationID,
		Operation:   operation,
		Status:      "ready_to_apply",
		DryRun:      true,
		Summary: fmt.Sprintf(
			"ops.domains.reconcile dry-run for %s: %d to create, %d to re-assert, %d skipped",
			serviceNameOf(host), create, ensure, skip),
		Data: map[string]any{
			"service":  serviceNameOf(host),
			"scope":    domainReconcileScope(scoped),
			"plan":     plan.Entries,
			"create":   create,
			"ensure":   ensure,
			"skipped":  skip,
			"mutating": plan.Mutating(),
		},
		Steps: []operatorOperationStep{
			{Name: "authorize", Status: "planned", Detail: "verify admin RBAC and audit reason on apply"},
			{Name: "load-state", Status: "completed", Detail: "read enclii.yaml and the existing domain records"},
			{Name: "diff", Status: "completed", Detail: "compared declared hostnames against registered ones"},
			{Name: "provision", Status: "planned", Detail: "create the DNS record, tunnel route, and TLS for each declared hostname using control-plane credentials"},
			{Name: "audit", Status: "planned", Detail: "record operation_id and reason against each domain"},
		},
		Warnings: warnings,
		Next:     []string{"rerun with dry_run=false and a reason to provision the declared hostnames"},
	}
}

func (h *Handler) handleOpsDomainsReconcileApply(ctx context.Context, operation string, req operatorOperationRequest) (operatorOperationResponse, int) {
	operationID := fmt.Sprintf("op_%d", time.Now().UTC().UnixNano())
	source := h.resolveDeclaredDomainSource(ctx, req)
	if source.Err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "invalid_request",
			DryRun:      false,
			Summary:     "ops.domains.reconcile could not resolve the service and its enclii.yaml",
			Warnings:    []string{source.Err.Error()},
		}, http.StatusBadRequest
	}

	host := selectDomainHostService([]*types.Service{source.Service}, source.Manifest)
	if host == nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "invalid_request",
			DryRun:      false,
			Summary:     "ops.domains.reconcile found no service to route the declared hostnames to",
		}, http.StatusBadRequest
	}

	scoped := hostnameScopedDomain(req)

	// The plan is taken BEFORE the mutation so the response reports what this
	// run set out to do. Taking it afterwards would report an all-`ensure`
	// plan every time and make a first provisioning indistinguishable from a
	// no-op re-run.
	plan := filterPlanToHostname(
		planDeclaredDomains(ctx, h.customDomainChecker(), host.Name, source.Manifest), scoped)
	create, ensure, skip := plan.Counts()

	if scoped != "" && len(plan.Entries) == 0 {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "invalid_request",
			DryRun:      false,
			Summary:     fmt.Sprintf("%s is not declared in %s's enclii.yaml", scoped, host.Name),
			Warnings: []string{
				"nothing was provisioned; a scoped reconcile never widens to the other declared hostnames",
			},
		}, http.StatusBadRequest
	}

	// The manifest handed to the provisioner is narrowed to the scope, so a
	// hostname-scoped reconcile touches exactly one hostname's DNS record and
	// exactly one tunnel ingress rule. Passing the whole manifest and relying on
	// idempotency to make the rest no-ops is NOT equivalent: every other
	// declared hostname would have its ingress rule rewritten, which is the
	// project-wide clobber shape.
	target := narrowManifestToHostname(source.Manifest, scoped)

	// Synchronous, unlike the webhook path: an operator who typed --apply is
	// waiting for the answer, and a backgrounded goroutine would return
	// "submitted" before anything had been attempted.
	h.provisionDomainsFromYAML(ctx, host, target)

	// Read back so the response reflects reality rather than intent. A hostname
	// still absent here failed, and the per-domain provisioning error is on its
	// record for `enclii domains status`.
	after := filterPlanToHostname(
		planDeclaredDomains(ctx, h.customDomainChecker(), host.Name, source.Manifest), scoped)
	stillMissing := []string{}
	for _, entry := range after.Entries {
		if entry.Action == declaredDomainCreate {
			stillMissing = append(stillMissing, entry.Domain)
		}
	}

	warnings := []string{
		"secrets and provider credentials were used server-side only; none is returned in this response",
	}
	if !h.domainEdgeConfigured() {
		warnings = append(warnings, "Cloudflare is not wired into this build, so no DNS record was created")
	}
	if len(stillMissing) > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"still unregistered after this pass: %s — see `enclii domains status` for the per-domain provisioning error",
			strings.Join(stillMissing, ", ")))
	}

	status := "submitted"
	statusCode := http.StatusAccepted
	if len(stillMissing) > 0 {
		status = "partial"
	}

	return operatorOperationResponse{
		OperationID: operationID,
		Operation:   operation,
		Status:      status,
		DryRun:      false,
		Summary: fmt.Sprintf(
			"reconciled %d declared hostname(s) for %s (%d created, %d re-asserted, %d skipped)",
			len(plan.Entries), host.Name, create, ensure, skip),
		Data: map[string]any{
			"service":      host.Name,
			"scope":        domainReconcileScope(scoped),
			"planned":      plan.Entries,
			"observed":     after.Entries,
			"created":      create,
			"reasserted":   ensure,
			"skipped":      skip,
			"stillMissing": stillMissing,
		},
		Steps: []operatorOperationStep{
			{Name: "authorize", Status: "completed", Detail: "reason supplied and caller passed endpoint authorization"},
			{Name: "load-state", Status: "completed", Detail: "read enclii.yaml and the existing domain records"},
			{Name: "diff", Status: "completed", Detail: "compared declared hostnames against registered ones"},
			{Name: "provision", Status: "completed", Detail: "ran the domain provisioner with control-plane Cloudflare credentials"},
			{Name: "audit", Status: "completed", Detail: "per-domain provisioning outcome persisted on each domain record"},
		},
		Warnings: warnings,
		Next: []string{
			"poll `enclii domains status <hostname>` for DNS, tunnel route, and TLS readiness",
			"re-run this operation freely; it is idempotent and re-asserts rather than duplicates",
		},
	}, statusCode
}

// customDomainChecker adapts the repository to the narrow interface the plan
// builder needs, and yields nil when the registry is unavailable so the plan
// degrades to `ensure` rather than panicking.
func (h *Handler) customDomainChecker() domainExistenceChecker {
	if h == nil || h.repos == nil || h.repos.CustomDomains == nil {
		return nil
	}
	return h.repos.CustomDomains
}

// domainEdgeConfigured reports whether a Cloudflare client is actually
// available to this build, so the operator is told plainly when a reconcile
// cannot reach the edge instead of reading a green summary over a no-op.
func (h *Handler) domainEdgeConfigured() bool {
	if h == nil || h.domainSyncService == nil {
		return false
	}
	return h.domainSyncService.GetCloudflareClient() != nil
}

// domainReconcileScope renders what this run was aimed at, so the response says
// plainly whether one hostname or the whole manifest was in scope.
func domainReconcileScope(hostname string) string {
	if hostname == "" {
		return "all declared hostnames"
	}
	return hostname
}

// narrowManifestToHostname returns a copy of the manifest declaring only the
// named hostname, leaving everything else (runtime port, network block) intact
// so the provisioner resolves the same backend it otherwise would.
//
// A copy, not a mutation: the caller's manifest is read again afterwards for
// the read-back plan, and quietly emptying it would make that read-back lie.
func narrowManifestToHostname(config *manifest.EncliiYAML, hostname string) *manifest.EncliiYAML {
	if config == nil || hostname == "" {
		return config
	}
	narrowed := *config
	narrowed.Spec.Domains = nil
	for _, domain := range config.Spec.Domains {
		if canonicalDomain(domain.Name) == hostname {
			narrowed.Spec.Domains = append(narrowed.Spec.Domains, domain)
		}
	}
	return &narrowed
}
