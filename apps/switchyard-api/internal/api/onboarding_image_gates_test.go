package api

import (
	"testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/checks"
)

func TestParseGHCRImage(t *testing.T) {
	cases := []struct {
		image       string
		wantOrg     string
		wantPackage string
		wantZero    bool
	}{
		{
			image:       "ghcr.io/madfam-org/enclii/switchyard-api@sha256:abc",
			wantOrg:     "madfam-org",
			wantPackage: "enclii/switchyard-api",
		},
		{
			image:       "ghcr.io/madfam-org/avala/avala-web:latest",
			wantOrg:     "madfam-org",
			wantPackage: "avala/avala-web",
		},
		{
			image:       "ghcr.io/madfam-org/cotiza/cotiza-api:v1.2.3",
			wantOrg:     "madfam-org",
			wantPackage: "cotiza/cotiza-api",
		},
		{
			image:    "docker.io/library/postgres:16",
			wantZero: true,
		},
		{
			image:    "nginx:1.25",
			wantZero: true,
		},
		{
			image:    "",
			wantZero: true,
		},
		{
			image:    "ghcr.io/only-one-segment",
			wantZero: true,
		},
	}
	for _, c := range cases {
		got := parseGHCRImage(c.image)
		if c.wantZero {
			if got.Image != "" {
				t.Errorf("parseGHCRImage(%q) = %+v, want zero", c.image, got)
			}
			continue
		}
		if got.Org != c.wantOrg || got.Package != c.wantPackage {
			t.Errorf("parseGHCRImage(%q) = %+v, want org=%s pkg=%s",
				c.image, got, c.wantOrg, c.wantPackage)
		}
	}
}

func TestExtractGHCRPackages_DedupesAcrossFiles(t *testing.T) {
	// Two manifests, three containers, but only two distinct GHCR packages.
	// docker.io/library/postgres must be ignored.
	manifests := []checks.ManifestFile{
		{
			Path: "deploy1.yaml",
			Content: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: a
spec:
  template:
    spec:
      containers:
        - name: a
          image: ghcr.io/madfam-org/avala/avala-web@sha256:abc
        - name: side
          image: docker.io/library/postgres:16
`,
		},
		{
			Path: "deploy2.yaml",
			Content: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: a-copy
spec:
  template:
    spec:
      containers:
        - name: a
          image: ghcr.io/madfam-org/avala/avala-web:v2
        - name: b
          image: ghcr.io/madfam-org/cotiza/cotiza-api@sha256:def
`,
		},
	}
	got := extractGHCRPackages(manifests)
	if len(got) != 2 {
		t.Fatalf("expected 2 unique packages, got %d: %+v", len(got), got)
	}
	// Sorted order: avala first, then cotiza.
	if got[0].Package != "avala/avala-web" {
		t.Errorf("expected first package avala/avala-web, got %s", got[0].Package)
	}
	if got[1].Package != "cotiza/cotiza-api" {
		t.Errorf("expected second package cotiza/cotiza-api, got %s", got[1].Package)
	}
}

func TestExtractGHCRPackages_CronJob(t *testing.T) {
	// CronJob nests its containers under jobTemplate.spec.template.spec.containers.
	// A prior bug here would have silently skipped CronJob images.
	manifests := []checks.ManifestFile{
		{
			Path: "cron.yaml",
			Content: `apiVersion: batch/v1
kind: CronJob
metadata:
  name: digest
spec:
  schedule: "0 * * * *"
  jobTemplate:
    spec:
      template:
        spec:
          containers:
            - name: digest
              image: ghcr.io/madfam-org/enclii/enclii-status@sha256:abc
`,
		},
	}
	got := extractGHCRPackages(manifests)
	if len(got) != 1 {
		t.Fatalf("expected 1 package, got %d", len(got))
	}
	if got[0].Package != "enclii/enclii-status" {
		t.Errorf("expected enclii/enclii-status, got %s", got[0].Package)
	}
}
