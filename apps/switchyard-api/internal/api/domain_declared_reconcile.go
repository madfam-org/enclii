package api

// Reconciling the hostnames a service DECLARES in its enclii.yaml against the
// hostnames that actually exist.
//
// The gap this closes: a domain added to an already-onboarded service's
// enclii.yaml was not provisioned. Two independent defects produced that, and
// both are addressed here rather than in the provisioner itself, because the
// provisioner was never the thing that was wrong — it was simply never called.
//
//  1. The webhook reached provisionDomainsFromYAML only AFTER the
//     `github-webhook-builds-enabled` gate, which defaults to false. A service
//     deployed through ArgoCD digest pinning (which is most of them) never
//     turns that flag on, so its manifest's domains were never reconciled.
//     webhook_push.go now calls reconcileDeclaredDomainsFromPush BEFORE that
//     gate.
//
//  2. Domains were sent to `services[0]` of an unordered SQL result. For a
//     monorepo with a web service and a worker, which row came back first was
//     whatever Postgres felt like, so a hostname could be routed at a headless
//     worker. selectDomainHostService picks deterministically instead.
//
// The plan type exists so the same diff can be shown to an operator (dry run)
// and executed (apply) without two implementations disagreeing about what
// "already provisioned" means.

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/manifest"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// declaredDomainAction is what reconciliation intends to do about one declared
// hostname.
type declaredDomainAction string

const (
	// declaredDomainCreate: the hostname has no CustomDomain row. DNS, tunnel
	// route and TLS all have to be established.
	declaredDomainCreate declaredDomainAction = "create"
	// declaredDomainEnsure: a row exists, so this pass re-asserts the edge
	// (DNS record + tunnel route) without creating anything. This is the
	// steady-state action and is what makes reconciliation idempotent: running
	// it twice produces the same plan and the same live state.
	declaredDomainEnsure declaredDomainAction = "ensure"
	// declaredDomainSkip: the declaration cannot be acted on at all (an
	// unparseable `external:` value, a hostname that fails validation). Skips
	// carry a reason so the operator sees why, rather than the hostname simply
	// being absent from the plan.
	declaredDomainSkip declaredDomainAction = "skip"
)

// declaredDomainPlanEntry is one hostname's decided-but-not-applied outcome.
type declaredDomainPlanEntry struct {
	Domain      string               `json:"domain"`
	Environment string               `json:"environment"`
	Action      declaredDomainAction `json:"action"`
	Reason      string               `json:"reason,omitempty"`
	// TLSEnabled mirrors the manifest declaration; it is reported so a dry run
	// shows what TLS posture is being asked for.
	TLSEnabled bool `json:"tls_enabled"`
}

// declaredDomainPlan is the full diff of declared-vs-existing for one service.
type declaredDomainPlan struct {
	Service string                    `json:"service"`
	Entries []declaredDomainPlanEntry `json:"entries"`
}

// Counts summarises the plan for an operator-facing summary line.
func (p declaredDomainPlan) Counts() (create, ensure, skip int) {
	for _, entry := range p.Entries {
		switch entry.Action {
		case declaredDomainCreate:
			create++
		case declaredDomainEnsure:
			ensure++
		case declaredDomainSkip:
			skip++
		}
	}
	return create, ensure, skip
}

// Mutating reports whether applying this plan would create anything new. An
// all-`ensure` plan is still worth applying (it re-asserts the edge), but an
// operator asking "is anything missing?" wants this answer.
func (p declaredDomainPlan) Mutating() bool {
	create, _, _ := p.Counts()
	return create > 0
}

// domainExistenceChecker is the narrow slice of the CustomDomains repository
// the plan builder needs. An interface, not the concrete repository, so the
// plan can be unit-tested without a database.
type domainExistenceChecker interface {
	Exists(ctx context.Context, domain string) (bool, error)
}

// planDeclaredDomains diffs the hostnames a manifest declares against the
// hostnames that already have a CustomDomain row.
//
// It is deliberately pure with respect to the edge: it does not call
// Cloudflare and does not touch the tunnel. Deciding what to do and doing it
// are separated so `--dry-run` and `--apply` cannot disagree.
//
// A lookup failure is NOT collapsed into "does not exist". An unknown answer
// becomes an `ensure`, which re-asserts the edge without minting a competing
// CustomDomain row — the safe direction, matching how the provisioner treats
// every other inconclusive read.
func planDeclaredDomains(
	ctx context.Context,
	existing domainExistenceChecker,
	serviceName string,
	envConfig *manifest.EncliiYAML,
) declaredDomainPlan {
	plan := declaredDomainPlan{Service: serviceName, Entries: []declaredDomainPlanEntry{}}
	if envConfig == nil {
		return plan
	}

	seen := make(map[string]struct{}, len(envConfig.Spec.Domains))
	for _, domainCfg := range envConfig.Spec.Domains {
		name := canonicalDomain(domainCfg.Name)
		entry := declaredDomainPlanEntry{
			Domain:      name,
			Environment: domainCfg.Environment,
			TLSEnabled:  domainCfg.IsTLSEnabled(),
		}

		if name == "" {
			entry.Action = declaredDomainSkip
			entry.Reason = "manifest declared an empty hostname"
			plan.Entries = append(plan.Entries, entry)
			continue
		}

		// A hostname declared twice in one manifest is planned once. Without
		// this, an idempotency assertion over the plan is meaningless: the
		// second copy would always read as an extra unit of work.
		if _, duplicate := seen[name]; duplicate {
			continue
		}
		seen[name] = struct{}{}

		if raw, malformed := domainCfg.ExternalParseFailure(); malformed {
			entry.Action = declaredDomainSkip
			entry.Reason = fmt.Sprintf("`external: %s` is not readable as true or false", raw)
			plan.Entries = append(plan.Entries, entry)
			continue
		}

		// Same validation the deploy path applies, and for the same reason:
		// nested hostnames are permitted (they are already declared in shipped
		// manifests) but a structurally invalid hostname is refused.
		if err := validateDomain(name, true); err != nil {
			entry.Action = declaredDomainSkip
			entry.Reason = err.Error()
			plan.Entries = append(plan.Entries, entry)
			continue
		}

		if existing == nil {
			entry.Action = declaredDomainEnsure
			entry.Reason = "domain registry unavailable; re-asserting the edge without creating a record"
			plan.Entries = append(plan.Entries, entry)
			continue
		}

		exists, err := existing.Exists(ctx, name)
		switch {
		case err != nil:
			entry.Action = declaredDomainEnsure
			entry.Reason = fmt.Sprintf("could not determine whether %s is already registered: %v", name, err)
		case exists:
			entry.Action = declaredDomainEnsure
			entry.Reason = "already registered; re-asserting DNS and tunnel route"
		default:
			entry.Action = declaredDomainCreate
			entry.Reason = "declared in enclii.yaml with no domain record yet"
		}
		plan.Entries = append(plan.Entries, entry)
	}

	sort.SliceStable(plan.Entries, func(i, j int) bool {
		return plan.Entries[i].Domain < plan.Entries[j].Domain
	})
	return plan
}

// selectDomainHostService picks which of a repo's services a declared hostname
// should be routed to.
//
// `services[0]` of an unordered query was the previous answer, and in a
// monorepo that is a coin flip. The preference order below is deterministic and
// prefers a service that can actually serve HTTP:
//
//  1. the service the manifest's `network.services` block lists first with a
//     non-zero port (a headless worker declares `port: 0` precisely to say "do
//     not publish me");
//  2. failing that, the first service by name with a non-zero declared port;
//  3. failing that, the first service by name.
//
// Sorting by name is what makes 2 and 3 deterministic; the previous code
// depended on SQL row order, which is not a guarantee Postgres makes.
func selectDomainHostService(services []*types.Service, envConfig *manifest.EncliiYAML) *types.Service {
	if len(services) == 0 {
		return nil
	}

	ordered := make([]*types.Service, 0, len(services))
	for _, service := range services {
		if service != nil {
			ordered = append(ordered, service)
		}
	}
	if len(ordered) == 0 {
		return nil
	}
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })

	byName := make(map[string]*types.Service, len(ordered))
	for _, service := range ordered {
		byName[strings.ToLower(strings.TrimSpace(service.Name))] = service
	}

	// Network is a pointer and is absent from most manifests; dereferencing it
	// unguarded would panic on every push from a service that declares domains
	// but no network block.
	if envConfig != nil && envConfig.Spec.Network != nil {
		for _, declared := range envConfig.Spec.Network.Services {
			if declared.Port == 0 {
				continue
			}
			if match, ok := byName[strings.ToLower(strings.TrimSpace(declared.Name))]; ok {
				return match
			}
		}
	}

	return ordered[0]
}

// reconcileDeclaredDomainsFromPush is the webhook entry point. It runs on every
// push to the default branch, independently of whether this platform also
// builds the image.
//
// Non-blocking and best-effort, matching the call it replaces: a Cloudflare
// hiccup must not fail a webhook GitHub will retry anyway.
func (h *Handler) reconcileDeclaredDomainsFromPush(
	ctx context.Context,
	services []*types.Service,
	envConfig *manifest.EncliiYAML,
) {
	if h == nil || envConfig == nil || len(envConfig.Spec.Domains) == 0 || len(services) == 0 {
		return
	}

	host := selectDomainHostService(services, envConfig)
	if host == nil {
		return
	}

	h.logger.Info(ctx, "Reconciling domains declared in enclii.yaml",
		logging.String("service", host.Name),
		logging.Int("declared_domains", len(envConfig.Spec.Domains)))

	go h.provisionDomainsFromYAML(context.Background(), host, envConfig)
}
