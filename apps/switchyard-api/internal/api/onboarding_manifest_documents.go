package api

// Onboarding a manifest that declares more than one surface.
//
// A multi-document enclii.yaml declares one `kind: Service` document per
// deployed surface, and each document declares its OWN domains, network entries
// and status entries. Onboarding read one document and provisioned everything
// it found against `services[0]` of an unordered SQL result, so on 2026-09-12
// telesia's three web hostnames (telesia.quest, www.telesia.quest,
// app.telesia.quest) were created as junctions bound to `telesia-api` —
// the service whose document did not declare a single one of them
// (enclii#546). nauta's manifest documents the same "services[0]" fallback as a
// PLATFORM-GAP.
//
// The rule here is that a hostname binds to the service whose document declares
// it, and to nothing else. If that service has no record yet it is registered
// first (the same get-then-create reconcile `enclii services-sync` performs);
// if registering it fails, the hostname is SKIPPED with a warning that names
// the service. Falling back to another service is never an option: a junction
// on the wrong service routes a surface's traffic into a different pod.

import (
	"context"
	"fmt"
	"strings"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/manifest"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// serviceResolver returns the service record a manifest document's
// metadata.name identifies, registering it when it does not exist yet.
type serviceResolver func(ctx context.Context, name string) (*types.Service, error)

// manifestDomainBinding is one Service document paired with the service that
// document names — the service every hostname in it will be provisioned
// against.
type manifestDomainBinding struct {
	// ServiceName is the document's metadata.name, kept separately so a
	// binding can be reported even when the service could not be resolved.
	ServiceName string
	Service     *types.Service
	Document    *manifest.EncliiYAML
}

// Hostnames lists the domains this document declares, canonicalised the same
// way the provisioner canonicalises them.
func (b manifestDomainBinding) Hostnames() []string {
	if b.Document == nil {
		return nil
	}
	hosts := make([]string, 0, len(b.Document.Spec.Domains))
	for _, domain := range b.Document.Spec.Domains {
		hosts = append(hosts, canonicalDomain(domain.Name))
	}
	return hosts
}

// bindManifestDomainDocuments pairs every Service document that declares
// domains with the service its metadata.name identifies.
//
// `allow` is the per-document capture gate (the dead-name guard); a nil allow
// admits every document. `resolve` finds or registers the service. A document
// whose service cannot be resolved produces a skip message naming BOTH the
// service and the hostnames that were not provisioned, and never borrows
// another document's service.
func bindManifestDomainDocuments(
	ctx context.Context,
	documents []*manifest.EncliiYAML,
	allow func(ctx context.Context, doc *manifest.EncliiYAML) bool,
	resolve serviceResolver,
) (bindings []manifestDomainBinding, skipped []string) {
	for _, doc := range manifest.ServiceDocuments(documents) {
		if len(doc.Spec.Domains) == 0 {
			continue
		}

		name := strings.TrimSpace(doc.Metadata.Name)
		binding := manifestDomainBinding{ServiceName: name, Document: doc}

		if name == "" {
			skipped = append(skipped, fmt.Sprintf(
				"a Service document declares %d domain(s) (%s) but no metadata.name, so there is no service to bind them to",
				len(doc.Spec.Domains), strings.Join(binding.Hostnames(), ", ")))
			continue
		}

		if allow != nil && !allow(ctx, doc) {
			continue
		}

		if resolve == nil {
			skipped = append(skipped, fmt.Sprintf(
				"%s: no service registry available to bind %s to", name, strings.Join(binding.Hostnames(), ", ")))
			continue
		}

		service, err := resolve(ctx, name)
		if err != nil || service == nil {
			reason := "the service is not registered and could not be registered"
			if err != nil {
				reason = err.Error()
			}
			skipped = append(skipped, fmt.Sprintf(
				"%s: %s — %s NOT provisioned (they are declared by %s's document and must not be bound to another service)",
				name, reason, strings.Join(binding.Hostnames(), ", "), name))
			continue
		}

		binding.Service = service
		bindings = append(bindings, binding)
	}

	return bindings, skipped
}

// ensureServiceForManifestDocument returns the service a manifest document
// names, registering it under the project when it has no record yet.
//
// Get-then-create keyed on the service NAME, which is the same reconcile
// `enclii services-sync` performs (it lists the project's services, keys them by
// metadata.name, and creates only the specs that are missing). Idempotent by
// construction: a second onboarding run finds the record and creates nothing.
func (h *Handler) ensureServiceForManifestDocument(
	ctx context.Context,
	project *types.Project,
	repoFullName string,
	name string,
) (service *types.Service, created bool, err error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, false, fmt.Errorf("manifest document declares no metadata.name")
	}
	if h.repos == nil || h.repos.Services == nil {
		return nil, false, fmt.Errorf("service registry is unavailable")
	}

	if existing, lookupErr := h.repos.Services.GetByName(name); lookupErr == nil && existing != nil {
		return existing, false, nil
	}

	if project == nil {
		return nil, false, fmt.Errorf("service %q has no record and no project is available to register it under", name)
	}

	service = &types.Service{
		ProjectID:  project.ID,
		Name:       name,
		GitRepo:    "https://github.com/" + repoFullName,
		AutoDeploy: true,
	}
	if err := h.repos.Services.Create(service); err != nil {
		return nil, false, fmt.Errorf("could not register service %q declared by enclii.yaml: %w", name, err)
	}

	h.logger.Info(ctx, "Registered service declared by enclii.yaml",
		logging.String("service", name),
		logging.String("project", project.Slug),
		logging.String("repo", repoFullName))

	return service, true, nil
}

// manifestServiceRegistration summarises what registering a manifest's Service
// documents did, in the shape the onboarding responses already report.
type manifestServiceRegistration struct {
	// Names carries "<service> (existing|created|failed)" per document.
	Names []string
	// Attempted counts documents whose service had no record yet.
	Attempted int
	// Err is the first registration failure, for the onboarding step.
	Err error
}

// registerManifestServices ensures a service record exists for every Service
// document in the manifest.
//
// Previously only `encliiConfig.Metadata.Name` — one document — was registered,
// so a manifest declaring a web surface and an API surface produced one service
// record and the other surface's document had nothing to bind its hostnames to.
func (h *Handler) registerManifestServices(
	ctx context.Context,
	project *types.Project,
	repoFullName string,
	documents []*manifest.EncliiYAML,
) manifestServiceRegistration {
	var result manifestServiceRegistration
	if h.repos == nil || h.repos.Services == nil {
		return result
	}

	seen := make(map[string]bool)
	for _, doc := range manifestServiceNameDocuments(documents) {
		name := strings.TrimSpace(doc.Metadata.Name)
		if name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true

		service, created, err := h.ensureServiceForManifestDocument(ctx, project, repoFullName, name)
		switch {
		case err != nil:
			result.Attempted++
			if result.Err == nil {
				result.Err = err
			}
			result.Names = append(result.Names, name+" (failed)")
		case created:
			result.Attempted++
			result.Names = append(result.Names, service.Name+" (created)")
		default:
			result.Names = append(result.Names, service.Name+" (existing)")
		}
	}

	return result
}

// manifestServiceNameDocuments lists the documents that name a service to
// register: every `kind: Service` document, plus — for a manifest that declares
// no Service document at all — the primary document, which is the pre-existing
// single-document behaviour.
func manifestServiceNameDocuments(documents []*manifest.EncliiYAML) []*manifest.EncliiYAML {
	if services := manifest.ServiceDocuments(documents); len(services) > 0 {
		return services
	}
	if primary := manifest.FirstServiceDocument(documents); primary != nil {
		return []*manifest.EncliiYAML{primary}
	}
	return nil
}

// provisionDomainsFromManifest captures the hostnames a manifest declares.
//
// Two paths, deliberately:
//
//   - a manifest with MORE THAN ONE Service document binds each document's
//     hostnames to the service that document names (enclii#546);
//   - anything else keeps the historical capture-service selection, unchanged.
//     A single-document manifest carries no per-service attribution to use:
//     janua's document is named `janua` while its services are janua-api /
//     janua-admin / …, and tezca's service `tezca-api` reads a document named
//     `tezca`. Binding those by document name would mint phantom service
//     records and re-point live domains, so the single-document case is left
//     exactly as it was.
//
// Returns the per-hostname result lines the onboarding response reports.
func (h *Handler) provisionDomainsFromManifest(
	ctx context.Context,
	steps *[]stepResult,
	project *types.Project,
	namespace string,
	repoFullName string,
	documents []*manifest.EncliiYAML,
) []string {
	if h == nil || project == nil || len(documents) == 0 {
		return nil
	}

	if len(manifest.ServiceDocuments(documents)) > 1 {
		return h.provisionDomainsPerManifestDocument(ctx, steps, project, namespace, repoFullName, documents)
	}

	primary := manifest.FirstServiceDocument(documents)
	if primary == nil || len(primary.Spec.Domains) == 0 {
		return nil
	}

	svcList, _ := h.repos.Services.ListByProject(project.ID)
	var captureService *types.Service
	if len(svcList) > 0 {
		captureService = svcList[0]
	}

	// Capture-time dead-name guard (2026-08-27 janua outage). A manifest doc
	// whose metadata.name serves nothing gets a loud step failure here and its
	// domains are never captured. See onboarding_manifest_workload_guard.go.
	if !h.guardManifestDomainCapture(ctx, steps, namespace, captureService, primary) {
		return nil
	}

	var domainResults []string
	if captureService != nil {
		go h.provisionDomainsFromYAML(context.Background(), captureService, primary)
		for _, d := range primary.Spec.Domains {
			domainResults = append(domainResults, d.Name+" (provisioning)")
		}
	}
	h.recordStep(ctx, steps, "domain_provisioning", false, nil)
	return domainResults
}

// provisionDomainsPerManifestDocument provisions each Service document's
// domains against the service that document declares them for.
func (h *Handler) provisionDomainsPerManifestDocument(
	ctx context.Context,
	steps *[]stepResult,
	project *types.Project,
	namespace string,
	repoFullName string,
	documents []*manifest.EncliiYAML,
) []string {
	allow := func(ctx context.Context, doc *manifest.EncliiYAML) bool {
		// The guard's existing-record lookup only enriches its message, so a
		// service with no record yet is fine here; it is registered below.
		var existing *types.Service
		if h.repos != nil && h.repos.Services != nil {
			if found, err := h.repos.Services.GetByName(doc.Metadata.Name); err == nil {
				existing = found
			}
		}
		return h.guardManifestDomainCapture(ctx, steps, namespace, existing, doc)
	}

	resolve := func(ctx context.Context, name string) (*types.Service, error) {
		service, _, err := h.ensureServiceForManifestDocument(ctx, project, repoFullName, name)
		return service, err
	}

	bindings, skipped := bindManifestDomainDocuments(ctx, documents, allow, resolve)

	var domainResults []string
	for _, binding := range bindings {
		h.logger.Info(ctx, "Provisioning domains declared by an enclii.yaml Service document",
			logging.String("service", binding.Service.Name),
			logging.String("namespace", namespace),
			logging.Int("domain_count", len(binding.Document.Spec.Domains)))

		go h.provisionDomainsFromYAML(context.Background(), binding.Service, binding.Document)
		for _, host := range binding.Hostnames() {
			domainResults = append(domainResults, fmt.Sprintf("%s → %s (provisioning)", host, binding.Service.Name))
		}
	}

	for _, message := range skipped {
		h.logger.Warn(ctx, "Skipping hostnames declared by an enclii.yaml Service document",
			logging.String("detail", message),
			logging.String("namespace", namespace))
	}

	if len(skipped) > 0 {
		h.recordStep(ctx, steps, "domain_provisioning", false, fmt.Errorf(
			"some declared hostnames were not provisioned because the service that declares them could not be registered: %s",
			strings.Join(skipped, "; ")))
	} else if len(bindings) > 0 {
		h.recordStep(ctx, steps, "domain_provisioning", false, nil)
	}

	return domainResults
}

// registerStatusEntriesForDocuments projects the status entries of EVERY
// document into the onboarding snapshot.
//
// Telesia merged all three of its status entries into one document because only
// the first was read. Entries are de-duplicated on name+url so a hostname
// declared by two documents during a migration registers once.
func (h *Handler) registerStatusEntriesForDocuments(
	ctx context.Context,
	projectName string,
	documents []*manifest.EncliiYAML,
) ([]statusServiceEntry, error) {
	var entries []statusServiceEntry
	var firstErr error
	seen := make(map[string]bool)

	for _, doc := range documents {
		docEntries, err := h.registerStatusEntries(ctx, projectName, doc)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		for _, entry := range docEntries {
			key := strings.ToLower(entry.Name) + "\x00" + strings.ToLower(entry.URL)
			if seen[key] {
				continue
			}
			seen[key] = true
			entries = append(entries, entry)
		}
	}

	return entries, firstErr
}
