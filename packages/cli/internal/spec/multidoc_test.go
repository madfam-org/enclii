package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression coverage for gotcha 9, hit live on 2026-08-21: the released CLI
// rejected the ONBOARD-STANDARD two-document enclii.yaml
// (kind: Project + kind: Service) with "service spec validation failed",
// because the loader decoded only the first document and then validated the
// Project document as if it were a Service. That forced domains to be wired
// by hand for both kalya and crea-map.

// twoDocManifest is the standard onboard shape.
const twoDocManifest = `apiVersion: enclii.dev/v1
kind: Project
metadata:
  name: kalya
  slug: kalya
spec:
  description: Kalya booking engine
---
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: kalya-api
  project: kalya
spec:
  build:
    type: auto
  runtime:
    port: 8080
    replicas: 2
    healthCheck: /health
`

const singleDocManifest = `apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: kalya-api
  project: kalya
spec:
  build:
    type: auto
  runtime:
    port: 8080
    replicas: 2
    healthCheck: /health
`

// multiServiceManifest has a Project plus two Services, which cannot be
// resolved without --service.
const multiServiceManifest = `apiVersion: enclii.dev/v1
kind: Project
metadata:
  name: kalya
  slug: kalya
---
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: kalya-api
  project: kalya
spec:
  build:
    type: auto
  runtime:
    port: 8080
    replicas: 1
---
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: kalya-web
  project: kalya
spec:
  build:
    type: auto
  runtime:
    port: 3000
    replicas: 1
`

// writeManifest writes the manifest into a temp dir that also satisfies
// build-type auto-detection, and chdirs there because the parser resolves
// the project root from the working directory.
func writeManifest(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "enclii.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	// Satisfy validateAutoDetection for build type "auto".
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"test"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalDir) })

	return path
}

// The `domains add` / `domains list` path: a two-document enclii.yaml must
// parse, with the Project document ignored.
func TestParseServiceSpec_TwoDocumentManifest(t *testing.T) {
	path := writeManifest(t, twoDocManifest)

	parsed, err := NewParser().ParseServiceSpec(path)
	if err != nil {
		t.Fatalf("two-document manifest failed to parse: %v", err)
	}
	if parsed.Kind != "Service" {
		t.Errorf("Kind = %q, want Service (the Project document must be skipped)", parsed.Kind)
	}
	if parsed.Metadata.Name != "kalya-api" {
		t.Errorf("Metadata.Name = %q, want kalya-api", parsed.Metadata.Name)
	}
	if parsed.Metadata.Project != "kalya" {
		t.Errorf("Metadata.Project = %q, want kalya", parsed.Metadata.Project)
	}
	if parsed.Spec.Runtime.Port != 8080 {
		t.Errorf("Runtime.Port = %d, want 8080", parsed.Spec.Runtime.Port)
	}
}

// `domains add <host> --service kalya-api` selects by metadata.name.
func TestParseServiceSpecNamed_TwoDocumentManifestSelectsByName(t *testing.T) {
	path := writeManifest(t, twoDocManifest)

	parsed, err := NewParser().ParseServiceSpecNamed(path, "kalya-api")
	if err != nil {
		t.Fatalf("named parse failed: %v", err)
	}
	if parsed.Metadata.Name != "kalya-api" {
		t.Errorf("Metadata.Name = %q, want kalya-api", parsed.Metadata.Name)
	}
}

// Single-document service.yaml behavior must be byte-identical to before.
func TestParseServiceSpec_SingleDocumentUnchanged(t *testing.T) {
	path := writeManifest(t, singleDocManifest)

	parsed, err := NewParser().ParseServiceSpec(path)
	if err != nil {
		t.Fatalf("single-document manifest failed to parse: %v", err)
	}
	if parsed.Metadata.Name != "kalya-api" || parsed.Metadata.Project != "kalya" {
		t.Errorf("unexpected metadata: %+v", parsed.Metadata)
	}
	if parsed.Spec.Runtime.Port != 8080 {
		t.Errorf("Runtime.Port = %d, want 8080", parsed.Spec.Runtime.Port)
	}

	// A single document is used whether or not a name is supplied.
	named, err := NewParser().ParseServiceSpecNamed(path, "kalya-api")
	if err != nil {
		t.Fatalf("named single-document parse failed: %v", err)
	}
	if named.Metadata.Name != "kalya-api" {
		t.Errorf("named Metadata.Name = %q", named.Metadata.Name)
	}
}

// A single-document file that is genuinely invalid must still fail
// validation the way it always did — the multi-doc scan must not convert a
// validation error into a "no Service document" error.
func TestParseServiceSpec_SingleInvalidDocumentStillFailsValidation(t *testing.T) {
	path := writeManifest(t, `apiVersion: enclii.dev/v1
kind: Project
metadata:
  name: kalya
`)

	_, err := NewParser().ParseServiceSpec(path)
	if err == nil {
		t.Fatal("expected a validation error for a lone Project document")
	}
	if !strings.Contains(err.Error(), "service spec validation failed") {
		t.Errorf("error = %q, want the original validation message", err)
	}
}

func TestParseServiceSpecNamed_AmbiguousManifestErrorsHelpfully(t *testing.T) {
	path := writeManifest(t, multiServiceManifest)

	_, err := NewParser().ParseServiceSpecNamed(path, "")
	if err == nil {
		t.Fatal("expected an error when several services are present and no name is given")
	}
	msg := err.Error()
	// The message must name the candidates and say how to disambiguate.
	for _, want := range []string{"kalya-api", "kalya-web", "--service"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not mention %q", msg, want)
		}
	}
}

func TestParseServiceSpecNamed_SelectsAmongSeveralServices(t *testing.T) {
	path := writeManifest(t, multiServiceManifest)

	for _, tc := range []struct {
		name     string
		wantPort int
	}{
		{"kalya-api", 8080},
		{"kalya-web", 3000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := NewParser().ParseServiceSpecNamed(path, tc.name)
			if err != nil {
				t.Fatalf("selecting %s failed: %v", tc.name, err)
			}
			if parsed.Metadata.Name != tc.name {
				t.Errorf("Metadata.Name = %q, want %q", parsed.Metadata.Name, tc.name)
			}
			if parsed.Spec.Runtime.Port != tc.wantPort {
				t.Errorf("Runtime.Port = %d, want %d", parsed.Spec.Runtime.Port, tc.wantPort)
			}
		})
	}
}

func TestParseServiceSpecNamed_UnknownServiceListsCandidates(t *testing.T) {
	path := writeManifest(t, multiServiceManifest)

	_, err := NewParser().ParseServiceSpecNamed(path, "kalya-worker")
	if err == nil {
		t.Fatal("expected an error for a service not in the manifest")
	}
	msg := err.Error()
	if !strings.Contains(msg, "kalya-worker") {
		t.Errorf("error %q does not name the requested service", msg)
	}
	for _, want := range []string{"kalya-api", "kalya-web"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not list candidate %q", msg, want)
		}
	}
}

// A trailing `---` produces an empty document that must be ignored rather
// than treated as a candidate.
func TestParseServiceSpec_IgnoresEmptyTrailingDocument(t *testing.T) {
	path := writeManifest(t, twoDocManifest+"---\n")

	parsed, err := NewParser().ParseServiceSpec(path)
	if err != nil {
		t.Fatalf("manifest with trailing separator failed to parse: %v", err)
	}
	if parsed.Metadata.Name != "kalya-api" {
		t.Errorf("Metadata.Name = %q, want kalya-api", parsed.Metadata.Name)
	}
}

func TestParseServiceSpec_MissingFile(t *testing.T) {
	_, err := NewParser().ParseServiceSpec(filepath.Join(t.TempDir(), "absent.yaml"))
	if err == nil {
		t.Fatal("expected an error for a missing file")
	}
	if !strings.Contains(err.Error(), "failed to read service spec file") {
		t.Errorf("error = %q, want the original read-failure message", err)
	}
}

func TestParseServiceSpec_MalformedYAML(t *testing.T) {
	path := writeManifest(t, "apiVersion: enclii.dev/v1\n\tkind: Service\n")

	_, err := NewParser().ParseServiceSpec(path)
	if err == nil {
		t.Fatal("expected an error for malformed YAML")
	}
	if !strings.Contains(err.Error(), "failed to parse service spec YAML") {
		t.Errorf("error = %q, want the original parse-failure message", err)
	}
}
