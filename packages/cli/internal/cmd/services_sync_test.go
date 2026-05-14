package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadRawServiceSpecsSkipsKubernetesServices(t *testing.T) {
	dir := t.TempDir()

	writeTestFile(t, dir, "k8s-service.yaml", `apiVersion: v1
kind: Service
metadata:
  name: cms-backend
spec:
  selector:
    app: cms-backend
`)

	writeTestFile(t, dir, "enclii-service.yaml", `apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: switchyard-api
  project: enclii
spec:
  build:
    type: dockerfile
    source:
      git:
        repository: https://github.com/madfam-org/enclii
        branch: main
        autoDeploy: true
`)

	specs, err := readRawServiceSpecs(dir)
	if err != nil {
		t.Fatalf("readRawServiceSpecs returned error: %v", err)
	}

	if len(specs) != 1 {
		t.Fatalf("expected 1 Enclii service spec, got %d", len(specs))
	}
	if specs[0].Metadata.Name != "switchyard-api" {
		t.Fatalf("expected switchyard-api, got %q", specs[0].Metadata.Name)
	}
}

func TestReadRawServiceSpecsReadsMultiDocumentEncliiFiles(t *testing.T) {
	dir := t.TempDir()

	writeTestFile(t, dir, ".enclii.yml", `apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: switchyard-api
  project: enclii
spec:
  build:
    type: dockerfile
---
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: switchyard-ui
  project: enclii
spec:
  build:
    type: dockerfile
`)

	specs, err := readRawServiceSpecs(dir)
	if err != nil {
		t.Fatalf("readRawServiceSpecs returned error: %v", err)
	}

	if len(specs) != 2 {
		t.Fatalf("expected 2 Enclii service specs, got %d", len(specs))
	}
	if specs[0].Metadata.Name != "switchyard-api" {
		t.Fatalf("expected first spec switchyard-api, got %q", specs[0].Metadata.Name)
	}
	if specs[1].Metadata.Name != "switchyard-ui" {
		t.Fatalf("expected second spec switchyard-ui, got %q", specs[1].Metadata.Name)
	}
}

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
