package api

import (
	"context"
	"fmt"
	"time"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/cloudflare"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/manifest"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

type junctionProvisioningSummary struct {
	Domain           string   `json:"domain"`
	TunnelRouteReady bool     `json:"tunnel_route_ready"`
	DNSRequested     bool     `json:"dns_requested"`
	Warnings         []string `json:"warnings,omitempty"`

	// Mechanism records whether the domain was provisioned as a zone+CNAME
	// (we control the nameservers) or as a Cloudflare for SaaS custom
	// hostname (the client does). PendingClientDNSRecords is what the domain
	// owner still has to create; while it is non-empty the junction is
	// waiting on them, not on us.
	Mechanism              string                   `json:"mechanism,omitempty"`
	PendingClientDNSAction string                   `json:"pending_client_dns_action,omitempty"`
	PendingClientDNSRecord []types.PendingDNSRecord `json:"pending_client_dns_records,omitempty"`
}

type junctionRouteReconcileSummary struct {
	Total  int      `json:"total"`
	Ready  int      `json:"ready"`
	Failed []string `json:"failed,omitempty"`
}

// provisionDomainsFromYAML auto-provisions custom domains declared in enclii.yaml.
// For each domain it:
//  1. Creates a CustomDomain record in the database (if not exists)
//  2. Adds a tunnel route via TunnelRoutesManager
//  3. Creates a DNS CNAME record in Cloudflare
//
// This is called from triggerAutoDeploy after a successful build.
// Errors are logged but don't block the deployment.
func (h *Handler) provisionDomainsFromYAML(
	ctx context.Context,
	service *types.Service,
	envConfig *manifest.EncliiYAML,
) {
	if envConfig == nil || len(envConfig.Spec.Domains) == 0 {
		return
	}

	runtimePort := envConfig.Spec.Runtime.Port
	for _, domainCfg := range envConfig.Spec.Domains {
		h.provisionSingleDomain(ctx, service, domainCfg, runtimePort)
	}
}

// provisionSingleDomain provisions a single domain: DB record + tunnel route + DNS CNAME
func (h *Handler) provisionSingleDomain(
	ctx context.Context,
	service *types.Service,
	domainCfg manifest.EncliiYAMLDomain,
	runtimePort int,
) {
	// Canonical form before anything else reads it. The manifest parser already
	// lowercases, but this path is also reached with manifests assembled in
	// memory, and one uppercase byte surviving to here is enough to make the
	// zone match miss and reroute a MADFAM domain onto the custom-hostname path.
	domainName := canonicalDomain(domainCfg.Name)
	envName := domainCfg.Environment
	servicePort := domainCfg.GetPort(runtimePort)
	external, externalDeclared := domainCfg.ExternalOverride()

	// A malformed `external:` value fails this one domain rather than the
	// whole manifest — the parser deliberately keeps the rest of the file.
	if raw, malformed := domainCfg.ExternalParseFailure(); malformed {
		h.logger.Error(ctx, "Skipping domain with an unreadable `external` value in enclii.yaml",
			logging.String("domain", domainName),
			logging.String("service", service.Name),
			logging.String("field", "spec.domains[].external"),
			logging.String("value", raw),
			logging.String("expected", "true or false"))
		return
	}

	// Validate domain format. Nested subdomains are allowed only on the
	// Cloudflare for SaaS path, which issues a certificate for the exact
	// hostname; Universal SSL on the zone path covers one level only.
	//
	// On the DEPLOY path a nested host is a loud warning, not a skip: hosts
	// like api.pravara.madfam.io are already declared in shipped manifests and
	// already reconciled, and silently dropping them here would stop
	// reconciling a live domain. Declaration-time entry points (AddCustomDomain,
	// CreateJunction) still reject them, where a human sees the error.
	if err := validateDomain(domainName, true); err != nil {
		h.logger.Warn(ctx, "Skipping invalid domain from enclii.yaml",
			logging.String("domain", domainName),
			logging.String("service", service.Name),
			logging.Error("error", err))
		return
	}
	if !(externalDeclared && external) && isNestedSubdomain(domainName) {
		h.logger.Warn(ctx, "Declared domain is nested more than one level below its apex; Cloudflare Universal SSL covers a single level, so TLS will fail at the edge unless the domain is declared `external: true`. Continuing to reconcile it because it is already declared.",
			logging.String("domain", domainName),
			logging.String("service", service.Name),
			logging.String("remedy", "declare `external: true` in enclii.yaml, or use a single-level host"))
	}

	var externalOverride *bool
	if externalDeclared {
		externalOverride = &external
	}

	// Check if domain already exists in the database
	exists, err := h.repos.CustomDomains.Exists(ctx, domainName)
	if err != nil {
		h.logger.Warn(ctx, "Failed to check domain existence",
			logging.String("domain", domainName),
			logging.Error("error", err))
		return
	}

	if exists {
		h.logger.Debug(ctx, "Domain already registered, skipping creation",
			logging.String("domain", domainName))
		// Even if already registered, ensure tunnel route + DNS exist
		h.provisionDomainEdge(ctx, domainName, service, envName, servicePort, externalOverride)
		return
	}

	// Find the environment
	env, err := h.repos.Environments.GetByProjectAndName(service.ProjectID, envName)
	if err != nil {
		h.logger.Warn(ctx, "Environment not found for domain provisioning, will provision on next deploy",
			logging.String("domain", domainName),
			logging.String("environment", envName),
			logging.Error("error", err))
		return
	}

	// Determine TLS issuer
	tlsIssuer := "letsencrypt-prod"
	if envName != "production" {
		tlsIssuer = "letsencrypt-staging"
	}

	// Create custom domain in database
	domain := &types.CustomDomain{
		ServiceID:     service.ID,
		EnvironmentID: env.ID,
		Domain:        domainName,
		Verified:      false,
		TLSEnabled:    domainCfg.IsTLSEnabled(),
		TLSIssuer:     tlsIssuer,
	}

	// Checked and claimed in one transaction, under the cross-project hostname
	// lock. This path reached Create with NO ownership check at all: a manifest
	// is a claim a project makes about a hostname, not proof of one, so a
	// declaration naming another project's live hostname minted a competing row
	// on every build. The claim fails closed, which on the deploy path means
	// the domain is simply not created this pass and is retried on the next —
	// the same shape as the existence check above.
	if err := h.claimHostname(ctx, domain, ownerFromService(service)); err != nil {
		h.logger.Warn(ctx, "Failed to create custom domain from enclii.yaml",
			logging.String("domain", domainName),
			logging.String("service", service.Name),
			logging.Error("error", err))
		return
	}

	h.logger.Info(ctx, "Custom domain created from enclii.yaml",
		logging.String("domain", domainName),
		logging.String("service", service.Name),
		logging.String("environment", envName))

	// Provision edge routing (zone + CNAME for domains whose nameservers we
	// control, Cloudflare for SaaS custom hostname for client-owned domains)
	// and the tunnel route, in whichever order the mechanism demands.
	h.provisionDomainEdge(ctx, domainName, service, envName, servicePort, externalOverride)
}

// provisionDomainEdge establishes both halves of a domain's routing — the
// Cloudflare edge and the tunnel ingress rule — in the order the mechanism
// demands.
//
// AddRoute OVERWRITES any existing ingress rule for the same hostname, and the
// ingress config is keyed on hostname across every tenant. It is therefore the
// mutation that has to be ordered against ownership, on every branch:
//
//   - custom hostname — the edge step runs FIRST because it carries the
//     ownership check. Establishing the rule before we know the hostname is
//     ours hands another project's traffic to this service, and would do so
//     even when the custom hostname is then refused.
//   - UNDETERMINED (or a mechanism that cannot be run at all) — nothing is
//     mutated. An undecidable zone lookup means we do not know whether this
//     hostname is ours or a client's, and "we could not find out" must not
//     buy the same ingress write that a decided mechanism does. Only the
//     diagnosis is recorded.
//   - zone + CNAME — keeps the pre-existing route-first order, but not
//     unconditionally: a hostname another project positively holds is refused
//     before the route is touched. The zone path is reached for domains whose
//     apex is a zone in our own account, and for anything a manifest pinned
//     with `external: false` — which is a claim the declaring project makes
//     about a hostname, not proof of one.
func (h *Handler) provisionDomainEdge(
	ctx context.Context,
	domain string,
	service *types.Service,
	envName string,
	servicePort int,
	external *bool,
) domainProvisioningResult {
	owner := ownerFromService(service)
	plan := h.planDomainRouting(ctx, domain, external)

	if plan.err != nil || plan.mechanism == mechanismUndetermined {
		result := h.applyDomainRouting(ctx, plan, owner)
		reason := "the mechanism this domain asked for cannot be run"
		if plan.mechanism == mechanismUndetermined {
			reason = "how this domain reaches the edge could not be decided"
		}
		h.logger.Warn(ctx, "Leaving the tunnel ingress rule untouched: "+reason,
			logging.String("domain", domain),
			logging.String("service", serviceNameOf(service)),
			logging.String("mechanism", string(plan.mechanism)),
			logging.Error("error", plan.err))
		return result
	}

	if plan.mechanism == mechanismCustomHostname {
		result := h.applyDomainRouting(ctx, plan, owner)
		if result.Err != nil {
			h.logger.Warn(ctx, "Leaving the tunnel ingress rule untouched: this hostname is not confirmed to belong to this project",
				logging.String("domain", domain),
				logging.String("service", serviceNameOf(service)),
				logging.Error("error", result.Err))
			return result
		}
		h.ensureTunnelRoute(ctx, domain, service, envName, servicePort)
		return result
	}

	if err := h.zonePathHostnameConflict(ctx, domain, owner); err != nil {
		h.logger.Warn(ctx, "Leaving the tunnel ingress rule untouched: another project holds this hostname",
			logging.String("domain", domain),
			logging.String("service", serviceNameOf(service)),
			logging.Error("error", err))
		result := domainProvisioningResult{Domain: domain, Mechanism: plan.mechanism}
		result.setErr(err)
		// Recorded against the REFUSED project's own row, never the holder's.
		// This branch has just established that somebody else holds the
		// hostname; writing the refusal to the row found by hostname marked the
		// victim's live domain `error` and stamped the attacker's project id
		// into the victim's operator-facing provisioning_error.
		h.persistDomainProvisioningResult(ctx, result, owner)
		return result
	}

	h.ensureTunnelRoute(ctx, domain, service, envName, servicePort)
	return h.applyDomainRouting(ctx, plan, owner)
}

// domainRoutingPlan is a decided-but-not-yet-applied provisioning of one
// domain. The decision is separated from the application so a caller can order
// the tunnel ingress rule against it (see provisionDomainEdge) without paying
// for a second zone listing.
type domainRoutingPlan struct {
	domain    string
	mechanism domainProvisioningMechanism
	client    *cloudflare.Client
	// err is set when the mechanism could not be decided, or when a mechanism
	// the manifest demanded cannot be run at all.
	err error
	// skip means Cloudflare is not wired up and the domain did not ask for
	// anything we now cannot deliver: nothing to do, nothing to record.
	skip bool
}

// planDomainRouting picks the provisioning mechanism for a domain.
//
// Two mechanisms exist and the choice is per-domain:
//   - zone + CNAME  — for domains whose nameservers point at our Cloudflare
//     account. Unchanged from before.
//   - custom hostname — Cloudflare for SaaS, for client-owned domains that
//     keep their own registrar and nameservers.
//
// external forces the mechanism when non-nil (enclii.yaml `external:`);
// nil means auto-detect.
func (h *Handler) planDomainRouting(ctx context.Context, domain string, external *bool) domainRoutingPlan {
	plan := domainRoutingPlan{domain: domain, mechanism: mechanismZoneCNAME}

	var cfClient *cloudflare.Client
	if h.domainSyncService != nil {
		cfClient = h.domainSyncService.GetCloudflareClient()
	}

	if cfClient == nil {
		// A domain declared `external: true` asked for a mechanism we cannot
		// run. Falling back to the zone path would point a client-owned domain
		// at DNS we do not control, so the manifest's request is refused
		// loudly and recorded on the domain instead.
		if external != nil && *external {
			plan.mechanism = mechanismCustomHostname
			plan.err = fmt.Errorf(
				"cannot provision client-owned domain %s: the Cloudflare client is unavailable, so no custom hostname can be registered and no DNS record will be created for it", domain)
			return plan
		}
		if h.domainSyncService != nil {
			h.logger.Warn(ctx, "Cloudflare client not available for domain provisioning",
				logging.String("domain", domain))
		}
		plan.skip = true
		return plan
	}

	plan.client = cfClient
	plan.mechanism, plan.err = h.resolveDomainMechanism(ctx, cfClient, domain, external)
	return plan
}

// applyDomainRouting executes a plan and persists the typed outcome on the
// domain record.
//
// Failures are logged and do NOT abort the deploy — that is deliberate — but
// they are also written to the domain record so the failure stays legible on
// the read path.
func (h *Handler) applyDomainRouting(ctx context.Context, plan domainRoutingPlan, owner *domainOwner) domainProvisioningResult {
	if plan.skip {
		return domainProvisioningResult{Domain: plan.domain, Mechanism: mechanismZoneCNAME}
	}

	var result domainProvisioningResult
	switch {
	case plan.err != nil:
		result = domainProvisioningResult{Domain: plan.domain, Mechanism: plan.mechanism}
		result.setErr(plan.err)
	case plan.mechanism == mechanismCustomHostname:
		result = h.ensureCustomHostname(ctx, plan.domain, owner)
	default:
		result = h.ensureZoneDNSRecord(ctx, plan.client, plan.domain)
	}

	if result.Err != nil {
		h.logger.Warn(ctx, "Domain provisioning failed",
			logging.String("domain", plan.domain),
			logging.String("mechanism", string(result.Mechanism)),
			logging.Error("error", result.Err))
	} else if result.WaitingOnClient() {
		// Names only: describePendingClientAction renders the ownership token
		// and the DCV challenge value, which must not reach a log.
		h.logger.Info(ctx, "Domain provisioning is waiting on the domain owner",
			logging.String("domain", plan.domain),
			logging.String("mechanism", string(result.Mechanism)),
			logging.Int("pending_client_dns_records", len(result.PendingDNSRecords)),
			logging.String("pending_client_dns_record_names", describePendingClientRecordNames(result)))
	}

	h.persistDomainProvisioningResult(ctx, result, owner)
	return result
}

// ensureZoneDNSRecord is the pre-existing zone + proxied CNAME path, used for
// domains whose nameservers are delegated to our Cloudflare account.
func (h *Handler) ensureZoneDNSRecord(ctx context.Context, cfClient *cloudflare.Client, domain string) domainProvisioningResult {
	result := domainProvisioningResult{Domain: domain, Mechanism: mechanismZoneCNAME}

	// Ensure the Cloudflare zone exists for this domain (creates if missing)
	if _, err := cfClient.EnsureZoneForDomain(ctx, domain); err != nil {
		h.logger.Warn(ctx, "Failed to ensure Cloudflare zone for domain",
			logging.String("domain", domain),
			logging.Error("error", err))
		// Continue — EnsureDNSRecord will fail with a clearer error if zone is truly missing
	}

	// The tunnel CNAME target — domains are CNAME'd to the tunnel endpoint.
	tunnelCNAME := h.domainSyncService.TunnelCNAME()

	record, created, err := cfClient.EnsureDNSRecord(ctx, domain, tunnelCNAME)
	if err != nil {
		h.logger.Warn(ctx, "Failed to create DNS record for domain",
			logging.String("domain", domain),
			logging.String("cname_target", tunnelCNAME),
			logging.Error("error", err))
		result.setErr(fmt.Errorf("failed to create DNS record for %s: %w", domain, err))
		return result
	}

	if created {
		h.logger.Info(ctx, "DNS CNAME record created in Cloudflare",
			logging.String("domain", domain),
			logging.String("cname_target", tunnelCNAME),
			logging.String("record_id", record.ID))
	} else {
		h.logger.Debug(ctx, "DNS record already exists",
			logging.String("domain", domain),
			logging.String("existing_content", record.Content))
	}

	return result
}

func (h *Handler) ensureJunctionInfrastructure(ctx context.Context, domain string, service *types.Service) junctionProvisioningSummary {
	summary := junctionProvisioningSummary{
		Domain:       domain,
		DNSRequested: h != nil && h.domainSyncService != nil,
	}
	if h == nil || service == nil {
		summary.Warnings = append(summary.Warnings, "service unavailable")
		return summary
	}

	result := h.provisionDomainEdge(ctx, domain, service, defaultProductionEnvironmentName, 80, nil)
	if h.tunnelRoutesService != nil {
		ready, err := h.tunnelRoutesService.RouteExists(ctx, domain)
		if err != nil {
			summary.Warnings = append(summary.Warnings, "tunnel route readback failed: "+err.Error())
		}
		summary.TunnelRouteReady = ready
	}

	summary.Mechanism = string(result.Mechanism)
	summary.PendingClientDNSRecord = result.PendingDNSRecords
	if action := describePendingClientAction(result); action != "" {
		summary.PendingClientDNSAction = action
	}
	if result.Err != nil {
		summary.Warnings = append(summary.Warnings, result.ErrorMessage)
	}
	return summary
}

func (h *Handler) reconcileJunctionTunnelRoutesForProject(ctx context.Context, project *types.Project) junctionRouteReconcileSummary {
	summary := junctionRouteReconcileSummary{}
	if h == nil || h.repos == nil || h.repos.Junctions == nil || h.repos.Services == nil || project == nil {
		return summary
	}

	if _, err := h.ensureDefaultProductionEnvironment(ctx, project); err != nil {
		h.logger.Warn(ctx, "Failed to ensure default environment before junction route reconciliation",
			logging.String("project", project.Slug),
			logging.Error("error", err))
	}

	junctions, err := h.repos.Junctions.ListByProject(ctx, project.ID)
	if err != nil {
		h.logger.Warn(ctx, "Failed to list junctions during route reconciliation",
			logging.String("project", project.Slug),
			logging.Error("error", err))
		return summary
	}

	summary.Total = len(junctions)
	for _, junction := range junctions {
		if junction == nil || junction.Domain == "" {
			continue
		}

		service, err := h.repos.Services.GetByID(junction.ServiceID)
		if err != nil {
			summary.Failed = append(summary.Failed, junction.Domain)
			h.logger.Warn(ctx, "Skipping junction route reconciliation because service lookup failed",
				logging.String("domain", junction.Domain),
				logging.String("service_id", junction.ServiceID.String()),
				logging.Error("error", err))
			continue
		}

		h.provisionDomainEdge(ctx, junction.Domain, service, defaultProductionEnvironmentName, 80, nil)

		if h.tunnelRoutesService == nil {
			continue
		}
		ready, err := h.tunnelRoutesService.RouteExists(ctx, junction.Domain)
		if err != nil || !ready {
			summary.Failed = append(summary.Failed, junction.Domain)
			if err != nil {
				h.logger.Warn(ctx, "Junction tunnel route readback failed after reconciliation",
					logging.String("domain", junction.Domain),
					logging.Error("error", err))
			}
			continue
		}
		summary.Ready++
	}

	return summary
}

func (h *Handler) scheduleJunctionTunnelRouteReconcile(project *types.Project) {
	if h == nil || project == nil {
		return
	}

	go func(projectCopy types.Project) {
		for _, delay := range []time.Duration{2 * time.Second, 15 * time.Second} {
			time.Sleep(delay)

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			summary := h.reconcileJunctionTunnelRoutesForProject(ctx, &projectCopy)
			cancel()

			h.logger.Info(context.Background(), "Delayed junction route reconciliation completed",
				logging.String("project", projectCopy.Slug),
				logging.Int("total", summary.Total),
				logging.Int("ready", summary.Ready))
		}
	}(*project)
}
