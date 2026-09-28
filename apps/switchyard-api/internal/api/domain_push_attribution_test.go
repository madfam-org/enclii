package api

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/manifest"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// singleDocManifest builds a one-document manifest the way the parser hands it
// to the push reconcile: one Service document, a flat domain list, and an
// optional network block.
func singleDocManifest(name string, runtimePort int, network []manifest.EncliiYAMLNetworkService, hosts ...string) *manifest.EncliiYAML {
	doc := &manifest.EncliiYAML{Kind: manifest.KindService}
	doc.Metadata.Name = name
	doc.Spec.Runtime.Port = runtimePort
	for _, host := range hosts {
		doc.Spec.Domains = append(doc.Spec.Domains, domainDecl(host))
	}
	if network != nil {
		doc.Spec.Network = &manifest.EncliiYAMLNetwork{Services: network}
	}
	return doc
}

// boundHosts flattens bindings into service -> sorted hostnames.
func boundHosts(bindings []manifestDomainBinding) map[string][]string {
	out := map[string][]string{}
	for _, binding := range bindings {
		hosts := binding.Hostnames()
		sort.Strings(hosts)
		out[binding.Service.Name] = append(out[binding.Service.Name], hosts...)
	}
	return out
}

func serviceOrderings(services ...*types.Service) [][]*types.Service {
	forward := append([]*types.Service(nil), services...)
	reversed := make([]*types.Service, 0, len(services))
	for i := len(services) - 1; i >= 0; i-- {
		reversed = append(reversed, services[i])
	}
	return [][]*types.Service{forward, reversed}
}

// The production shape that flipped hostnames on every push: ONE document named
// after the project (not after any service), listing hostnames for a web app,
// an API and an admin console, with a network block that declares all three as
// HTTP services. The old code routed every hostname to the first network entry
// with a port. No hostname may be routed anywhere now — and in particular none
// may be routed to the web service.
func TestPushDomainBindingsSingleDocumentMultiServiceRoutesNothing(t *testing.T) {
	web := &types.Service{Name: "example-web"}
	api := &types.Service{Name: "example-api"}
	admin := &types.Service{Name: "example-admin"}

	doc := singleDocManifest("example", 80, []manifest.EncliiYAMLNetworkService{
		{Name: "example-web", Port: 3000},
		{Name: "example-api", Port: 4000},
		{Name: "example-admin", Port: 5000},
	}, "web.example.com", "api.example.com", "admin.example.com", "staging-api.example.com")

	for _, services := range serviceOrderings(web, api, admin) {
		bindings, skipped := pushDomainBindings(context.Background(), services, []*manifest.EncliiYAML{doc})
		if len(bindings) != 0 {
			t.Fatalf("an ambiguous single-document manifest must route NO hostname, got %v", boundHosts(bindings))
		}
		if len(skipped) != 1 {
			t.Fatalf("want one skip message covering every hostname, got %d: %v", len(skipped), skipped)
		}
		for _, want := range []string{
			"web.example.com", "api.example.com", "admin.example.com", "staging-api.example.com",
			"example-web", "example-api", "example-admin",
		} {
			if !strings.Contains(skipped[0], want) {
				t.Fatalf("skip message must name %q so an operator can act on it; got %q", want, skipped[0])
			}
		}
	}
}

// Registering only ONE of the manifest's HTTP services does not make the
// manifest unambiguous: the manifest itself says the hostnames could belong to
// several services, and the one that happens to be registered is not
// thereby their owner.
func TestPushDomainBindingsOnlyOneOfSeveralDeclaredServicesRegistered(t *testing.T) {
	web := &types.Service{Name: "example-web"}
	doc := singleDocManifest("example", 80, []manifest.EncliiYAMLNetworkService{
		{Name: "example-web", Port: 3000},
		{Name: "example-api", Port: 4000},
	}, "web.example.com", "api.example.com")

	bindings, skipped := pushDomainBindings(context.Background(), []*types.Service{web}, []*manifest.EncliiYAML{doc})
	if len(bindings) != 0 {
		t.Fatalf("must not route to the only registered service when the manifest declares two HTTP services, got %v", boundHosts(bindings))
	}
	if len(skipped) != 1 {
		t.Fatalf("want one skip message, got %v", skipped)
	}
}

// A registered service the network block does not mention could be serving
// HTTP; nothing in the manifest rules it out, so it counts as a candidate.
func TestPushDomainBindingsUndeclaredRegisteredServiceIsACandidate(t *testing.T) {
	web := &types.Service{Name: "example-web"}
	api := &types.Service{Name: "example-api"}
	doc := singleDocManifest("example", 80, []manifest.EncliiYAMLNetworkService{
		{Name: "example-web", Port: 3000},
	}, "web.example.com")

	for _, services := range serviceOrderings(web, api) {
		bindings, skipped := pushDomainBindings(context.Background(), services, []*manifest.EncliiYAML{doc})
		if len(bindings) != 0 || len(skipped) != 1 {
			t.Fatalf("want nothing routed and one skip, got bindings=%v skipped=%v", boundHosts(bindings), skipped)
		}
	}
}

// Without a network block, more than one registered service is ambiguous. The
// old code picked the first by name.
func TestPushDomainBindingsNoNetworkBlockSeveralServicesRoutesNothing(t *testing.T) {
	alpha := &types.Service{Name: "alpha-api"}
	beta := &types.Service{Name: "beta-web"}
	doc := singleDocManifest("example", 80, nil, "web.example.com")

	for _, services := range serviceOrderings(alpha, beta) {
		bindings, skipped := pushDomainBindings(context.Background(), services, []*manifest.EncliiYAML{doc})
		if len(bindings) != 0 || len(skipped) != 1 {
			t.Fatalf("want nothing routed and one skip, got bindings=%v skipped=%v", boundHosts(bindings), skipped)
		}
	}
}

// The unambiguous cases keep reconciling exactly as before.

func TestPushDomainBindingsSingleServiceStillReconciles(t *testing.T) {
	only := &types.Service{Name: "example-web"}

	// With and without a network block.
	for _, doc := range []*manifest.EncliiYAML{
		singleDocManifest("example-web", 3000, nil, "example.com", "www.example.com"),
		singleDocManifest("example-web", 3000, []manifest.EncliiYAMLNetworkService{
			{Name: "example-web", Port: 3000},
		}, "example.com", "www.example.com"),
	} {
		bindings, skipped := pushDomainBindings(context.Background(), []*types.Service{only}, []*manifest.EncliiYAML{doc})
		if len(skipped) != 0 {
			t.Fatalf("an unambiguous manifest must skip nothing, got %v", skipped)
		}
		got := boundHosts(bindings)
		if len(got) != 1 || strings.Join(got["example-web"], ",") != "example.com,www.example.com" {
			t.Fatalf("want every hostname on example-web, got %v", got)
		}
		// The narrowed document keeps the runtime port the provisioner reads.
		if bindings[0].Document.Spec.Runtime.Port != 3000 {
			t.Fatalf("narrowed document lost runtime.port: %+v", bindings[0].Document.Spec.Runtime)
		}
	}
}

// The case selectDomainHostService was written for: a web service and a
// headless worker. The worker declares `port: 0`, so the web service is the
// only candidate, in either SQL row order.
func TestPushDomainBindingsWebPlusHeadlessWorkerStillReconciles(t *testing.T) {
	web := &types.Service{Name: "example-web"}
	worker := &types.Service{Name: "example-worker"}
	doc := singleDocManifest("example-web", 3000, []manifest.EncliiYAMLNetworkService{
		{Name: "example-worker", Port: 0},
		{Name: "example-web", Port: 3000},
	}, "example.com", "app.example.com")

	for _, services := range serviceOrderings(worker, web) {
		bindings, skipped := pushDomainBindings(context.Background(), services, []*manifest.EncliiYAML{doc})
		if len(skipped) != 0 {
			t.Fatalf("want nothing skipped, got %v", skipped)
		}
		got := boundHosts(bindings)
		if len(got) != 1 || strings.Join(got["example-web"], ",") != "app.example.com,example.com" {
			t.Fatalf("want every hostname on example-web, got %v", got)
		}
	}
}

func TestPushDomainBindingsOnlyHeadlessServicesRoutesNothing(t *testing.T) {
	worker := &types.Service{Name: "example-worker"}
	doc := singleDocManifest("example", 0, []manifest.EncliiYAMLNetworkService{
		{Name: "example-worker", Port: 0},
	}, "example.com")

	bindings, skipped := pushDomainBindings(context.Background(), []*types.Service{worker}, []*manifest.EncliiYAML{doc})
	if len(bindings) != 0 || len(skipped) != 1 || !strings.Contains(skipped[0], "headless") {
		t.Fatalf("want nothing routed and a headless skip, got bindings=%v skipped=%v", boundHosts(bindings), skipped)
	}
}

// The single HTTP candidate is declared but has no service record: skip, and
// never borrow the registered headless service.
func TestPushDomainBindingsSoleCandidateNotRegistered(t *testing.T) {
	worker := &types.Service{Name: "example-worker"}
	doc := singleDocManifest("example", 3000, []manifest.EncliiYAMLNetworkService{
		{Name: "example-web", Port: 3000},
		{Name: "example-worker", Port: 0},
	}, "example.com")

	bindings, skipped := pushDomainBindings(context.Background(), []*types.Service{worker}, []*manifest.EncliiYAML{doc})
	if len(bindings) != 0 || len(skipped) != 1 || !strings.Contains(skipped[0], "example-web") {
		t.Fatalf("want nothing routed and a skip naming example-web, got bindings=%v skipped=%v", boundHosts(bindings), skipped)
	}
}

// A `kind: Project` manifest attributes each hostname to a service
// (spec.services[].domains[]). That attribution is honoured even though the
// network block lists several HTTP services, and a hostname whose declared
// service is not registered is skipped without costing the others.
func TestPushDomainBindingsHonoursProjectKindAttribution(t *testing.T) {
	documents, err := manifest.ParseEncliiYAMLDocuments([]byte(`apiVersion: enclii.dev/v1
kind: Project
metadata:
  name: example
spec:
  network:
    services:
      - name: example-landing
        port: 8080
      - name: example-gateway
        port: 8787
      - name: example-docs
        port: 8081
  services:
    - name: example-gateway
      port: 8787
      domains:
        - host: api.example.com
    - name: example-landing
      port: 8080
      domains:
        - host: example.com
        - host: www.example.com
    - name: example-docs
      port: 8081
      domains:
        - host: docs.example.com
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	landing := &types.Service{Name: "example-landing"}
	gateway := &types.Service{Name: "example-gateway"}

	for _, services := range serviceOrderings(landing, gateway) {
		bindings, skipped := pushDomainBindings(context.Background(), services, documents)
		got := boundHosts(bindings)
		if strings.Join(got["example-gateway"], ",") != "api.example.com" {
			t.Fatalf("api.example.com must route to example-gateway only, got %v", got)
		}
		if strings.Join(got["example-landing"], ",") != "example.com,www.example.com" {
			t.Fatalf("landing hostnames must route to example-landing only, got %v", got)
		}
		if len(got) != 2 {
			t.Fatalf("no other service may receive a hostname, got %v", got)
		}
		if len(skipped) != 1 || !strings.Contains(skipped[0], "docs.example.com") || !strings.Contains(skipped[0], "example-docs") {
			t.Fatalf("docs.example.com must be skipped naming its unregistered service, got %v", skipped)
		}

		// Each binding carries the per-service port the Project declared.
		for _, binding := range bindings {
			for _, domain := range binding.Document.Spec.Domains {
				if binding.Service.Name == "example-gateway" && domain.Port != 8787 {
					t.Fatalf("gateway hostname lost its port: %+v", domain)
				}
			}
		}
	}
}

// Narrowing must not alias the parsed document's domain slice: the provisioner
// runs in a goroutine per binding, and a shared backing array would let one
// binding's document observe another's hostnames.
func TestAttributeSingleDocumentDomainsDoesNotAliasTheSourceDocument(t *testing.T) {
	doc := singleDocManifest("example-web", 3000, nil, "example.com", "www.example.com")
	web := &types.Service{Name: "example-web"}

	bindings, _ := attributeSingleDocumentDomains([]*types.Service{web}, doc)
	if len(bindings) != 1 {
		t.Fatalf("want one binding, got %d", len(bindings))
	}
	bindings[0].Document.Spec.Domains[0].Name = "mutated.example.com"
	if doc.Spec.Domains[0].Name != "example.com" {
		t.Fatalf("binding document aliases the source manifest's domains")
	}
}

// The multi-document path is unchanged: each document's hostnames bind to the
// service it names, whatever the network block says.
func TestPushDomainBindingsMultiDocumentBindsPerDocument(t *testing.T) {
	webDoc := singleDocManifest("example-web", 3000, []manifest.EncliiYAMLNetworkService{
		{Name: "example-web", Port: 3000},
	}, "example.com")
	apiDoc := singleDocManifest("example-api", 4000, []manifest.EncliiYAMLNetworkService{
		{Name: "example-api", Port: 4000},
	}, "api.example.com")

	web := &types.Service{Name: "example-web"}
	api := &types.Service{Name: "example-api"}

	bindings, skipped := pushDomainBindings(context.Background(), []*types.Service{api, web}, []*manifest.EncliiYAML{webDoc, apiDoc})
	if len(skipped) != 0 {
		t.Fatalf("want nothing skipped, got %v", skipped)
	}
	got := boundHosts(bindings)
	if strings.Join(got["example-web"], ",") != "example.com" || strings.Join(got["example-api"], ",") != "api.example.com" {
		t.Fatalf("want per-document binding, got %v", got)
	}
}

func TestPushDomainBindingsHandlesEmptyInputs(t *testing.T) {
	web := &types.Service{Name: "example-web"}
	if bindings, skipped := pushDomainBindings(context.Background(), []*types.Service{web}, nil); len(bindings) != 0 || len(skipped) != 0 {
		t.Fatalf("no documents must bind and skip nothing, got %v %v", bindings, skipped)
	}
	noDomains := singleDocManifest("example-web", 3000, nil)
	if bindings, skipped := pushDomainBindings(context.Background(), []*types.Service{web}, []*manifest.EncliiYAML{noDomains}); len(bindings) != 0 || len(skipped) != 0 {
		t.Fatalf("a manifest with no domains must bind and skip nothing, got %v %v", bindings, skipped)
	}
	doc := singleDocManifest("example-web", 3000, nil, "example.com")
	if bindings, skipped := pushDomainBindings(context.Background(), []*types.Service{nil}, []*manifest.EncliiYAML{doc}); len(bindings) != 0 || len(skipped) != 1 {
		t.Fatalf("only-nil services must bind nothing and say so, got %v %v", bindings, skipped)
	}
}

// pushRecordingLogger adds Info capture to recordingLogger, so a test can
// assert that no provisioning was started.
type pushRecordingLogger struct {
	recordingLogger
	infos []string
}

func (r *pushRecordingLogger) Info(_ context.Context, msg string, _ ...logging.Field) {
	r.infos = append(r.infos, msg)
}

// The webhook entry point must not panic or provision anything for an
// ambiguous manifest, and must say so at warning level.
func TestReconcileDeclaredDomainsFromPushAmbiguousIsANoOp(t *testing.T) {
	logger := &pushRecordingLogger{}
	h := &Handler{logger: logger}
	web := &types.Service{Name: "example-web"}
	api := &types.Service{Name: "example-api"}
	doc := singleDocManifest("example", 80, []manifest.EncliiYAMLNetworkService{
		{Name: "example-web", Port: 3000},
		{Name: "example-api", Port: 4000},
	}, "web.example.com", "api.example.com")

	h.reconcileDeclaredDomainsFromPush(context.Background(), []*types.Service{web, api}, []*manifest.EncliiYAML{doc})

	warned := false
	for _, msg := range logger.warns {
		if strings.Contains(msg, "Declared hostnames left unprovisioned by this push") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("an ambiguous manifest must be logged as a warning; warnings: %v", logger.warns)
	}
	for _, msg := range logger.infos {
		if strings.Contains(msg, "Reconciling domains declared in enclii.yaml") {
			t.Fatalf("an ambiguous manifest must not start any provisioning; infos: %v", logger.infos)
		}
	}
}
