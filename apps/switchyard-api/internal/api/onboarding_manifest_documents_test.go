package api

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/manifest"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// The manifest telesia onboarded with on 2026-09-12: two surfaces, each
// declaring its own hostnames, network entries and status entries.
const twoSurfaceManifest = `
apiVersion: enclii.dev/v1
kind: Project
metadata:
  name: telesia
spec:
  promotion:
    pattern: auto
---
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: telesia-web
  project: telesia
spec:
  runtime:
    port: 3000
  domains:
    - name: telesia.quest
    - name: www.telesia.quest
    - name: app.telesia.quest
  network:
    services:
      - name: telesia-web
        port: 3000
        ingress: [cloudflare-tunnel]
        egress: [dns, https, janua]
  status:
    enabled: true
    entries:
      - name: Telesia
        url: https://telesia.quest/health
        group: Telesia
---
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: telesia-api
  project: telesia
spec:
  runtime:
    port: 8080
  domains:
    - name: api.telesia.quest
  network:
    services:
      - name: telesia-api
        port: 8080
        ingress: [cloudflare-tunnel]
        egress: [dns, https, postgres]
      - name: telesia-worker
        port: 0
        egress: [dns, https, postgres]
  status:
    enabled: true
    entries:
      - name: Telesia API
        url: https://api.telesia.quest/health
        group: Telesia
`

func mustParseDocuments(t *testing.T, content string) []*manifest.EncliiYAML {
	t.Helper()
	docs, err := manifest.ParseEncliiYAMLDocuments([]byte(content))
	if err != nil {
		t.Fatalf("ParseEncliiYAMLDocuments() error = %v", err)
	}
	return docs
}

// registryResolver is a serviceResolver over an in-memory registry, recording
// which names it was asked to register.
type registryResolver struct {
	services   map[string]*types.Service
	registered []string
	refuse     map[string]bool
}

func newRegistryResolver(names ...string) *registryResolver {
	r := &registryResolver{services: map[string]*types.Service{}, refuse: map[string]bool{}}
	for _, name := range names {
		r.services[name] = &types.Service{ID: uuid.New(), ProjectID: uuid.New(), Name: name}
	}
	return r
}

func (r *registryResolver) resolve(_ context.Context, name string) (*types.Service, error) {
	if service, ok := r.services[name]; ok {
		return service, nil
	}
	if r.refuse[name] {
		return nil, errRegistrationRefused
	}
	service := &types.Service{ID: uuid.New(), ProjectID: uuid.New(), Name: name}
	r.services[name] = service
	r.registered = append(r.registered, name)
	return service, nil
}

var errRegistrationRefused = &registrationError{}

type registrationError struct{}

func (*registrationError) Error() string { return "service registry refused the write" }

// junctionServiceIDs maps each declared hostname to the id of the service its
// junction will be created against. provisionDomainsFromYAML is called with
// (binding.Service, binding.Document) and provisionSingleDomain writes
// custom_domains.service_id = service.ID, so this IS the junction's service.
func junctionServiceIDs(bindings []manifestDomainBinding) map[string]uuid.UUID {
	ids := map[string]uuid.UUID{}
	for _, binding := range bindings {
		for _, host := range binding.Hostnames() {
			ids[host] = binding.Service.ID
		}
	}
	return ids
}

// DEFECT 2 (enclii#546): during `onboard ensure` the three telesia web
// hostnames were created as junctions bound to telesia-api, because host
// selection only ever saw the parsed (first) document's service. Each hostname
// must bind to the service whose document DECLARES it.
func TestManifestDomainsBindToTheServiceWhoseDocumentDeclaresThem(t *testing.T) {
	docs := mustParseDocuments(t, twoSurfaceManifest)
	resolver := newRegistryResolver("telesia-web", "telesia-api")

	bindings, skipped := bindManifestDomainDocuments(context.Background(), docs, nil, resolver.resolve)
	if len(skipped) != 0 {
		t.Fatalf("unexpected skips: %v", skipped)
	}
	if len(bindings) != 2 {
		t.Fatalf("bound %d documents, want 2 (one per Service document)", len(bindings))
	}

	web := resolver.services["telesia-web"]
	api := resolver.services["telesia-api"]
	want := map[string]uuid.UUID{
		"telesia.quest":     web.ID,
		"www.telesia.quest": web.ID,
		"app.telesia.quest": web.ID,
		"api.telesia.quest": api.ID,
	}

	got := junctionServiceIDs(bindings)
	if len(got) != len(want) {
		t.Fatalf("bound %d hostnames, want %d: %v", len(got), len(want), got)
	}
	for host, wantID := range want {
		gotID, ok := got[host]
		if !ok {
			t.Fatalf("%s was not bound to any service; its junction would never be created", host)
		}
		if gotID != wantID {
			owner := "telesia-web"
			if wantID == api.ID {
				owner = "telesia-api"
			}
			t.Errorf("%s bound to service %s, want %s (%s declares it)", host, gotID, wantID, owner)
		}
	}

	// The Project document declares no domains and must bind nothing.
	for _, binding := range bindings {
		if binding.Document.Kind != manifest.KindService {
			t.Errorf("bound a %s document; only Service documents declare a surface", binding.Document.Kind)
		}
	}
}

// The service a document names has no record yet: register it, do not borrow
// another surface's service.
func TestManifestDomainBindingRegistersAMissingService(t *testing.T) {
	docs := mustParseDocuments(t, twoSurfaceManifest)
	resolver := newRegistryResolver("telesia-web") // telesia-api is not registered

	bindings, skipped := bindManifestDomainDocuments(context.Background(), docs, nil, resolver.resolve)
	if len(skipped) != 0 {
		t.Fatalf("unexpected skips: %v", skipped)
	}
	if len(resolver.registered) != 1 || resolver.registered[0] != "telesia-api" {
		t.Fatalf("registered %v, want exactly [telesia-api]", resolver.registered)
	}
	if got := junctionServiceIDs(bindings)["api.telesia.quest"]; got != resolver.services["telesia-api"].ID {
		t.Fatalf("api.telesia.quest bound to %s, want the newly registered telesia-api", got)
	}
}

// Registration failed: that document's hostnames are SKIPPED with a warning
// naming the service. Binding them to telesia-web instead is the defect.
func TestManifestDomainBindingSkipsRatherThanBorrowingAnotherService(t *testing.T) {
	docs := mustParseDocuments(t, twoSurfaceManifest)
	resolver := newRegistryResolver("telesia-web")
	resolver.refuse["telesia-api"] = true

	bindings, skipped := bindManifestDomainDocuments(context.Background(), docs, nil, resolver.resolve)

	if len(bindings) != 1 || bindings[0].ServiceName != "telesia-web" {
		t.Fatalf("bindings = %+v, want only the telesia-web document", bindings)
	}
	if _, bound := junctionServiceIDs(bindings)["api.telesia.quest"]; bound {
		t.Fatal("api.telesia.quest was bound to telesia-web; a hostname must never be routed at a service that did not declare it")
	}
	if len(skipped) != 1 {
		t.Fatalf("skipped = %v, want one message", skipped)
	}
	if !strings.Contains(skipped[0], "telesia-api") || !strings.Contains(skipped[0], "api.telesia.quest") {
		t.Fatalf("skip message %q must name both the service and the hostname it left unprovisioned", skipped[0])
	}
}

// The dead-name guard decides per document: refusing one surface must not
// refuse the other's hostnames.
func TestManifestDomainBindingHonoursThePerDocumentCaptureGuard(t *testing.T) {
	docs := mustParseDocuments(t, twoSurfaceManifest)
	resolver := newRegistryResolver("telesia-web", "telesia-api")

	allow := func(_ context.Context, doc *manifest.EncliiYAML) bool {
		return doc.Metadata.Name != "telesia-api"
	}

	bindings, skipped := bindManifestDomainDocuments(context.Background(), docs, allow, resolver.resolve)
	if len(skipped) != 0 {
		t.Fatalf("a guarded document is refused by the guard's own step, not as a binding skip: %v", skipped)
	}
	if len(bindings) != 1 || bindings[0].ServiceName != "telesia-web" {
		t.Fatalf("bindings = %+v, want only telesia-web", bindings)
	}
}

// A single-surface manifest has nothing to disambiguate and must keep the
// historical capture-service selection: bindManifestDomainDocuments is not
// even reached for it (provisionDomainsFromManifest branches on the Service
// document count), and binding it directly is still well-defined.
func TestManifestDomainBindingIgnoresDocumentsWithoutDomains(t *testing.T) {
	docs := mustParseDocuments(t, `
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: worker
spec:
  runtime:
    port: 0
---
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: web
spec:
  domains:
    - name: web.madfam.io
`)
	resolver := newRegistryResolver("worker", "web")

	bindings, skipped := bindManifestDomainDocuments(context.Background(), docs, nil, resolver.resolve)
	if len(skipped) != 0 {
		t.Fatalf("unexpected skips: %v", skipped)
	}
	if len(bindings) != 1 || bindings[0].ServiceName != "web" {
		t.Fatalf("bindings = %+v, want only the document that declares a hostname", bindings)
	}
}

// Every document's network entries are generated. Telesia had to merge both
// blocks into one document to get either of them applied.
func TestConvertNetworkSpecCoversEveryDocument(t *testing.T) {
	docs := mustParseDocuments(t, twoSurfaceManifest)

	merged := manifest.MergedNetwork(docs)
	if merged == nil {
		t.Fatal("MergedNetwork() = nil; onboarding would generate no NetworkPolicy at all")
	}
	spec := convertNetworkSpec(merged)

	var names []string
	for _, svc := range spec.Services {
		names = append(names, svc.Name)
	}
	want := []string{"telesia-web", "telesia-api", "telesia-worker"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("network services = %v, want %v", names, want)
	}
	if spec.Services[2].Port != 0 {
		t.Errorf("telesia-worker port = %d, want 0 (headless)", spec.Services[2].Port)
	}
}

// Status entries are registered per document, and de-duplicated across them.
func TestRegisterStatusEntriesForDocumentsCollectsEveryDocument(t *testing.T) {
	h := &Handler{logger: newNopLogger()}
	docs := mustParseDocuments(t, twoSurfaceManifest)

	entries, err := h.registerStatusEntriesForDocuments(context.Background(), "telesia", docs)
	if err != nil {
		t.Fatalf("registerStatusEntriesForDocuments() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("registered %d entries, want 2: %+v", len(entries), entries)
	}
	if entries[0].Name != "Telesia" || entries[1].Name != "Telesia API" {
		t.Fatalf("entries = %+v, want both documents' entries in declaration order", entries)
	}

	// The same entry declared twice registers once.
	duplicated := append(docs, docs[1])
	entries, err = h.registerStatusEntriesForDocuments(context.Background(), "telesia", duplicated)
	if err != nil {
		t.Fatalf("registerStatusEntriesForDocuments() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("registered %d entries for a duplicated document, want 2", len(entries))
	}
}

// manifestServiceNameDocuments decides which names onboarding registers: every
// Service document, or — with no Service document at all — the primary one,
// which is the pre-existing single-document behaviour.
func TestManifestServiceNameDocuments(t *testing.T) {
	docs := mustParseDocuments(t, twoSurfaceManifest)
	names := []string{}
	for _, doc := range manifestServiceNameDocuments(docs) {
		names = append(names, doc.Metadata.Name)
	}
	if strings.Join(names, ",") != "telesia-web,telesia-api" {
		t.Fatalf("service documents = %v, want both surfaces", names)
	}

	projectOnly := mustParseDocuments(t, `
apiVersion: enclii.dev/v1
kind: Project
metadata:
  name: legacy
spec:
  services:
    - name: legacy-web
      port: 3000
`)
	only := manifestServiceNameDocuments(projectOnly)
	if len(only) != 1 || only[0].Metadata.Name != "legacy" {
		t.Fatalf("project-only manifest = %+v, want the single primary document", only)
	}
}
