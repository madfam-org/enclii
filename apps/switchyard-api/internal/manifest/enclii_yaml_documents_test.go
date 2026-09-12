package manifest

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The house manifest shape, and the one that produced enclii#546: a Project
// document first, then one Service document per surface, each declaring its own
// domains, network entries and status entries.
const telesiaShapedManifest = `
apiVersion: enclii.dev/v1
kind: Project
metadata:
  name: telesia
spec:
  promotion:
    pattern: auto
    min_soak_minutes: 10
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

func domainNames(config *EncliiYAML) []string {
	if config == nil {
		return nil
	}
	names := make([]string, 0, len(config.Spec.Domains))
	for _, d := range config.Spec.Domains {
		names = append(names, d.Name)
	}
	return names
}

// One yaml.Unmarshal read the first document and discarded the rest, so this
// manifest exposed a promotion policy and nothing else.
func TestParseEncliiYAMLDocumentsReadsEveryDocument(t *testing.T) {
	docs, err := ParseEncliiYAMLDocuments([]byte(telesiaShapedManifest))
	if err != nil {
		t.Fatalf("ParseEncliiYAMLDocuments() error = %v", err)
	}
	if len(docs) != 3 {
		t.Fatalf("parsed %d documents, want 3: %v", len(docs), DocumentNames(docs))
	}

	want := []string{"Project/telesia", "Service/telesia-web", "Service/telesia-api"}
	if got := DocumentNames(docs); !reflect.DeepEqual(got, want) {
		t.Fatalf("DocumentNames() = %v, want %v", got, want)
	}
	if got := len(ServiceDocuments(docs)); got != 2 {
		t.Fatalf("ServiceDocuments() returned %d documents, want 2", got)
	}
}

// A whole-manifest caller must land on a surface, not on the promotion policy:
// the Project document declares no domains, and returning it is what
// provisioned zero hostnames for telesia at first onboarding.
func TestParseEncliiYAMLReturnsTheFirstServiceDocument(t *testing.T) {
	config, err := ParseEncliiYAML([]byte(telesiaShapedManifest))
	if err != nil {
		t.Fatalf("ParseEncliiYAML() error = %v", err)
	}
	if config.Kind != KindService || config.Metadata.Name != "telesia-web" {
		t.Fatalf("ParseEncliiYAML() = %s/%s, want Service/telesia-web", config.Kind, config.Metadata.Name)
	}
	if got := domainNames(config); !reflect.DeepEqual(got, []string{"telesia.quest", "app.telesia.quest"}) {
		t.Fatalf("domains = %v, want the web document's two hostnames", got)
	}
}

// A Project-only manifest (the `spec.services[]` shape) has no Service document
// to prefer, and must keep parsing exactly as it did.
func TestParseEncliiYAMLFallsBackToTheFirstDocumentWithoutAService(t *testing.T) {
	config, err := ParseEncliiYAML([]byte(`
apiVersion: enclii.dev/v1
kind: Project
metadata:
  name: legacy
spec:
  services:
    - name: legacy-web
      port: 3000
      domains:
        - host: legacy.madfam.io
          primary: true
`))
	if err != nil {
		t.Fatalf("ParseEncliiYAML() error = %v", err)
	}
	if config.Kind != KindProject {
		t.Fatalf("Kind = %q, want Project", config.Kind)
	}
	if got := domainNames(config); !reflect.DeepEqual(got, []string{"legacy.madfam.io"}) {
		t.Fatalf("domains = %v, want the project spec's flattened hostname", got)
	}
}

// Defect 1, per service: each document's domains belong to the service that
// document names. Telesia had to merge them into one document by hand.
func TestDocumentForServiceReturnsThatServicesOwnDomains(t *testing.T) {
	docs, err := ParseEncliiYAMLDocuments([]byte(telesiaShapedManifest))
	if err != nil {
		t.Fatalf("ParseEncliiYAMLDocuments() error = %v", err)
	}

	for service, want := range map[string][]string{
		"telesia-web": {"telesia.quest", "app.telesia.quest"},
		"telesia-api": {"api.telesia.quest"},
	} {
		doc := DocumentForService(docs, service)
		if doc == nil {
			t.Fatalf("DocumentForService(%q) = nil; its hostnames would be provisioned against another service", service)
		}
		if doc.Metadata.Name != service {
			t.Fatalf("DocumentForService(%q) returned the %q document", service, doc.Metadata.Name)
		}
		if got := domainNames(doc); !reflect.DeepEqual(got, want) {
			t.Fatalf("DocumentForService(%q) domains = %v, want %v", service, got, want)
		}
	}

	// Case and surrounding whitespace are not a different service.
	if doc := DocumentForService(docs, "  Telesia-API  "); doc == nil || doc.Metadata.Name != "telesia-api" {
		t.Fatalf("DocumentForService() did not match on a case/whitespace variant: %v", doc)
	}

	// A name no document declares must answer nothing rather than guess.
	if doc := DocumentForService(docs, "telesia-worker"); doc != nil {
		t.Fatalf("DocumentForService(telesia-worker) = %q; a multi-document manifest must not fall back to another service's document",
			doc.Metadata.Name)
	}
}

// Rule 2 of DocumentForService: a single-document manifest answers for whatever
// service asks. janua's one document is named `janua` while its registered
// services are janua-api / janua-admin / …; matching by name would stop
// reconciling eight live hostnames.
func TestDocumentForServiceFallsBackOnlyForASingleDocumentManifest(t *testing.T) {
	docs, err := ParseEncliiYAMLDocuments([]byte(`
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: janua
spec:
  domains:
    - name: auth.madfam.io
`))
	if err != nil {
		t.Fatalf("ParseEncliiYAMLDocuments() error = %v", err)
	}
	doc := DocumentForService(docs, "janua-api")
	if doc == nil || doc.Metadata.Name != "janua" {
		t.Fatalf("DocumentForService() = %v, want the sole document for a legacy single-document manifest", doc)
	}
}

// Every document's network entries are generated, not just the first one's.
func TestMergedNetworkUnionsEveryDocument(t *testing.T) {
	docs, err := ParseEncliiYAMLDocuments([]byte(telesiaShapedManifest))
	if err != nil {
		t.Fatalf("ParseEncliiYAMLDocuments() error = %v", err)
	}

	network := MergedNetwork(docs)
	if network == nil {
		t.Fatal("MergedNetwork() = nil; no NetworkPolicy would be generated at all")
	}
	var names []string
	for _, svc := range network.Services {
		names = append(names, svc.Name)
	}
	want := []string{"telesia-web", "telesia-api", "telesia-worker"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("MergedNetwork() services = %v, want %v", names, want)
	}

	// No document declaring a network block keeps the caller's nil gate honest.
	if got := MergedNetwork([]*EncliiYAML{{Kind: KindService}}); got != nil {
		t.Fatalf("MergedNetwork() = %+v, want nil when nothing declares a network block", got)
	}
}

// The same for status entries: telesia merged all three into document one to
// get them onto the status page.
func TestStatusEntriesAreReadableFromEveryDocument(t *testing.T) {
	docs, err := ParseEncliiYAMLDocuments([]byte(telesiaShapedManifest))
	if err != nil {
		t.Fatalf("ParseEncliiYAMLDocuments() error = %v", err)
	}
	if !HasStatusDeclaration(docs) {
		t.Fatal("HasStatusDeclaration() = false; status registration would be skipped entirely")
	}

	var entries []string
	for _, doc := range docs {
		if doc.Spec.Status == nil {
			continue
		}
		for _, entry := range doc.Spec.Status.Entries {
			entries = append(entries, entry.Name)
		}
	}
	if want := []string{"Telesia", "Telesia API"}; !reflect.DeepEqual(entries, want) {
		t.Fatalf("status entries = %v, want %v", entries, want)
	}
}

// A single-document manifest must parse to exactly the struct it parsed to
// before the decoder loop existed — same fields, same order, same YAML bytes.
func TestSingleDocumentManifestParsesExactlyAsBefore(t *testing.T) {
	const single = `
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: my-api
  project: acme
spec:
  runtime:
    port: 8080
  headers:
    X-Frame-Options: DENY
  domains:
    - name: API.Example.com
      environment: production
      tlsEnabled: true
    - name: staging.example.com
      environment: staging
      tlsEnabled: false
`

	got, err := ParseEncliiYAML([]byte(single))
	if err != nil {
		t.Fatalf("ParseEncliiYAML() error = %v", err)
	}

	tlsOn, tlsOff := true, false
	want := &EncliiYAML{
		APIVersion: "enclii.dev/v1",
		Kind:       "Service",
		Metadata:   EncliiYAMLMeta{Name: "my-api", Project: "acme"},
		Spec: EncliiYAMLSpec{
			Domains: []EncliiYAMLDomain{
				{Name: "api.example.com", Environment: "production", TLSEnabled: &tlsOn},
				{Name: "staging.example.com", Environment: "staging", TLSEnabled: &tlsOff},
			},
			Runtime: EncliiYAMLRuntime{Port: 8080},
			Headers: map[string]string{"X-Frame-Options": "DENY"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseEncliiYAML() = %+v, want %+v", got, want)
	}

	gotYAML, err := yaml.Marshal(got)
	if err != nil {
		t.Fatalf("yaml.Marshal(got) error = %v", err)
	}
	wantYAML, err := yaml.Marshal(want)
	if err != nil {
		t.Fatalf("yaml.Marshal(want) error = %v", err)
	}
	if string(gotYAML) != string(wantYAML) {
		t.Fatalf("re-marshalled manifest differs:\n got: %s\nwant: %s", gotYAML, wantYAML)
	}

	// And the document loop returns the identical value for the same file.
	docs, err := ParseEncliiYAMLDocuments([]byte(single))
	if err != nil {
		t.Fatalf("ParseEncliiYAMLDocuments() error = %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("parsed %d documents, want 1", len(docs))
	}
	if !reflect.DeepEqual(docs[0], got) {
		t.Fatalf("document loop and ParseEncliiYAML disagree:\n%+v\n%+v", docs[0], got)
	}
}

// An error over a five-document manifest has to say which document to fix.
func TestParseEncliiYAMLDocumentsNamesTheFailingDocument(t *testing.T) {
	_, err := ParseEncliiYAMLDocuments([]byte(`
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: ok
---
apiVersion: enclii.dev/v1
kind: Project
metadata:
  name: ok-project
---
apiVersion: enclii.dev/v1
kind: Deployment
metadata:
  name: wrong
`))
	if err == nil {
		t.Fatal("expected an error for `kind: Deployment`")
	}
	if !strings.Contains(err.Error(), "document 3") {
		t.Fatalf("error %q does not name the failing document index", err)
	}
	if !strings.Contains(err.Error(), "unsupported kind") {
		t.Fatalf("error %q lost the underlying reason", err)
	}

	// Malformed YAML is reported against its own document too.
	_, err = ParseEncliiYAMLDocuments([]byte("apiVersion: enclii.dev/v1\nkind: Service\nmetadata:\n  name: ok\n---\n{{{not valid yaml"))
	if err == nil {
		t.Fatal("expected a decode error for the second document")
	}
	if !strings.Contains(err.Error(), "document 2") || !strings.Contains(err.Error(), "failed to parse") {
		t.Fatalf("error %q does not identify the malformed document", err)
	}
}

// Empty documents are legal YAML: a trailing `---`, a leading separator, a
// comment-only tail. None of them was an error before and none becomes one.
func TestParseEncliiYAMLDocumentsSkipsEmptyDocuments(t *testing.T) {
	docs, err := ParseEncliiYAMLDocuments([]byte(`---
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: only
---
# a trailing comment and nothing else
---
`))
	if err != nil {
		t.Fatalf("ParseEncliiYAMLDocuments() error = %v", err)
	}
	if len(docs) != 1 || docs[0].Metadata.Name != "only" {
		t.Fatalf("parsed %v, want a single `only` document", DocumentNames(docs))
	}

	// An empty file keeps answering the way it always has.
	if _, err := ParseEncliiYAML(nil); err == nil || !strings.Contains(err.Error(), "unsupported apiVersion") {
		t.Fatalf("ParseEncliiYAML(nil) error = %v, want the pre-existing apiVersion error", err)
	}
}
