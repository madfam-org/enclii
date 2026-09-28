package api

// Which registered service owns each hostname a push's enclii.yaml declares.
//
// The push reconcile (reconcileDeclaredDomainsFromPush) re-asserts every
// declared hostname's tunnel ingress rule on every push to the default branch.
// For a manifest with one Service document per surface, the answer to "which
// service does this hostname route to" is the document's own metadata.name
// (enclii#546). For a SINGLE-document manifest it used to be
// selectDomainHostService: the first `network.services` entry with a non-zero
// port. That is a tie-break for "web service vs headless worker", and it was
// being used as an ownership rule. A single document that lists hostnames for a
// web app, an API and an admin console sent all of them to whichever of the
// three the network block happened to list first, so on every push the API and
// admin hostnames were rewritten onto the web app. The post-write canary
// usually put them back within seconds; once it did not, and the API hostname
// kept serving the web app's login redirect.
//
// The rule now is the one bindManifestDomainDocuments already applies to
// multi-document manifests: a hostname is routed only to a service the
// manifest unambiguously ties it to, and otherwise it is SKIPPED, loudly.
// Skipping is safe: the reconcile only ever re-asserts or creates rules, so a
// skipped hostname keeps whatever rule it has today. Guessing is not: a wrong
// guess repoints live production traffic.
//
// A hostname is tied to a service when:
//
//  1. the manifest attributes it explicitly — a `kind: Project` manifest
//     declares each hostname under spec.services[].domains[], and the parser
//     records that service on the domain; or
//  2. exactly ONE service could possibly be serving HTTP for this manifest.
//     The candidates are every `network.services` entry with a non-zero port,
//     plus every registered service the network block does not mention at all
//     (nothing says it is headless, so nothing rules it out). A service
//     declared with `port: 0` is headless and never a candidate. This keeps
//     the case selectDomainHostService was written for — a web service and a
//     headless worker — working exactly as before.
//
// metadata.name is deliberately NOT an ownership signal for a single document.
// Legacy single-document manifests name the project (or one of its services)
// while declaring hostnames for several services; binding every hostname to
// the document's name is the same wrong answer with a different tie-break.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/manifest"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// singleDocumentDomainOwner returns the one service every UNATTRIBUTED hostname
// in a single-document manifest may be routed to, or nil and the reason no
// such service exists.
//
// registered is keyed by normalizeServiceKey(name).
func singleDocumentDomainOwner(
	registered map[string]*types.Service,
	doc *manifest.EncliiYAML,
) (*types.Service, string) {
	ported := map[string]struct{}{}
	headless := map[string]struct{}{}
	if doc != nil && doc.Spec.Network != nil {
		for _, declared := range doc.Spec.Network.Services {
			key := normalizeServiceKey(declared.Name)
			if key == "" {
				continue
			}
			if declared.Port > 0 {
				ported[key] = struct{}{}
			} else {
				headless[key] = struct{}{}
			}
		}
	}
	// A name listed both with and without a port publishes something.
	for key := range ported {
		delete(headless, key)
	}

	candidates := map[string]struct{}{}
	for key := range ported {
		candidates[key] = struct{}{}
	}
	for key := range registered {
		_, isPorted := ported[key]
		_, isHeadless := headless[key]
		if !isPorted && !isHeadless {
			candidates[key] = struct{}{}
		}
	}

	names := make([]string, 0, len(candidates))
	for key := range candidates {
		names = append(names, key)
	}
	sort.Strings(names)

	switch len(names) {
	case 0:
		return nil, "no service this manifest describes can serve HTTP (every registered service is declared headless with `port: 0`)"
	case 1:
		owner, ok := registered[names[0]]
		if !ok {
			return nil, fmt.Sprintf(
				"the only service that could serve these hostnames, %q, is not registered for this repository (run `enclii onboard ensure` to register it)",
				names[0])
		}
		return owner, ""
	default:
		return nil, fmt.Sprintf(
			"this single-document enclii.yaml does not say which service each hostname belongs to, and %d services could serve them (%s); "+
				"routing them all to one of those would repoint the others' traffic. Declare one `kind: Service` document per service "+
				"(each document's hostnames bind to the service it names), or reconcile a hostname explicitly with "+
				"`enclii ops domains reconcile <service> --domain <host>`",
			len(names), strings.Join(names, ", "))
	}
}

// attributeSingleDocumentDomains pairs each hostname a single-document
// manifest declares with the service it is unambiguously tied to, grouping
// hostnames that share a service into one narrowed copy of the document.
//
// Hostnames that cannot be attributed produce a skip message naming them and
// the reason; they are never bound to a fallback.
func attributeSingleDocumentDomains(
	services []*types.Service,
	doc *manifest.EncliiYAML,
) (bindings []manifestDomainBinding, skipped []string) {
	if doc == nil || len(doc.Spec.Domains) == 0 {
		return nil, nil
	}

	registered := make(map[string]*types.Service, len(services))
	for _, service := range services {
		if service == nil {
			continue
		}
		if key := normalizeServiceKey(service.Name); key != "" {
			registered[key] = service
		}
	}

	// Computed lazily: a manifest whose every hostname is attributed never
	// needs a document-level owner, and must not log a reason for lacking one.
	var (
		owner         *types.Service
		ownerReason   string
		ownerResolved bool
	)
	documentOwner := func() (*types.Service, string) {
		if !ownerResolved {
			owner, ownerReason = singleDocumentDomainOwner(registered, doc)
			ownerResolved = true
		}
		return owner, ownerReason
	}

	grouped := map[string][]manifest.EncliiYAMLDomain{}
	order := []*types.Service{}
	assign := func(service *types.Service, domain manifest.EncliiYAMLDomain) {
		key := normalizeServiceKey(service.Name)
		if _, seen := grouped[key]; !seen {
			order = append(order, service)
		}
		grouped[key] = append(grouped[key], domain)
	}

	var unattributed []string
	for _, domain := range doc.Spec.Domains {
		if declaredFor := strings.TrimSpace(domain.Service); declaredFor != "" {
			service, ok := registered[normalizeServiceKey(declaredFor)]
			if !ok {
				skipped = append(skipped, fmt.Sprintf(
					"%s: declared for service %q, which is not registered for this repository, so a push cannot provision it (run `enclii onboard ensure` to register it); it is never routed to another service",
					canonicalDomain(domain.Name), declaredFor))
				continue
			}
			assign(service, domain)
			continue
		}

		service, _ := documentOwner()
		if service == nil {
			unattributed = append(unattributed, canonicalDomain(domain.Name))
			continue
		}
		assign(service, domain)
	}

	if len(unattributed) > 0 {
		_, reason := documentOwner()
		skipped = append(skipped, fmt.Sprintf("%s NOT provisioned by this push: %s",
			strings.Join(unattributed, ", "), reason))
	}

	for _, service := range order {
		narrowed := *doc
		narrowed.Spec.Domains = append([]manifest.EncliiYAMLDomain(nil), grouped[normalizeServiceKey(service.Name)]...)
		bindings = append(bindings, manifestDomainBinding{
			ServiceName: service.Name,
			Service:     service,
			Document:    &narrowed,
		})
	}

	return bindings, skipped
}

// normalizeServiceKey is the comparison form of a service name, matching the
// case- and whitespace-insensitive lookups the per-document path uses.
func normalizeServiceKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
