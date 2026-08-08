package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/checks"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
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

// resolvePackages runs the manifest set through the same resolution the gate
// uses, then extracts GHCR packages from the EFFECTIVE images.
func resolvePackages(t *testing.T, manifests []checks.ManifestFile) []ghcrImageRef {
	t.Helper()
	images, _, err := checks.CollectWorkloadImages(manifests)
	if err != nil {
		t.Fatalf("CollectWorkloadImages: %v", err)
	}
	return extractGHCRPackages(images)
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
	got := resolvePackages(t, manifests)
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
	got := resolvePackages(t, manifests)
	if len(got) != 1 {
		t.Fatalf("expected 1 package, got %d", len(got))
	}
	if got[0].Package != "enclii/enclii-status" {
		t.Errorf("expected enclii/enclii-status, got %s", got[0].Package)
	}
}

// The GHCR existence gate must see the image the kustomization produces. With
// the house convention the Deployment carries a bare short name, which is not
// a ghcr.io reference at all — reading the raw manifest here silently skipped
// the existence check for every repo following the convention.
func TestExtractGHCRPackages_UsesKustomizeNewName(t *testing.T) {
	manifests := []checks.ManifestFile{
		{
			Path: "web-deployment.yaml",
			Content: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  template:
    spec:
      containers:
        - name: web
          image: web
`,
		},
		{
			Path: "kustomization.yaml",
			Content: `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
images:
  - name: web
    newName: ghcr.io/madfam-org/nauta/web
    digest: sha256:a07d12ee70fe8537fe11daffe4f925d2b1c61ab2f319ccc39bfe3d38f0912bc4
`,
		},
	}
	got := resolvePackages(t, manifests)
	if len(got) != 1 {
		t.Fatalf("expected 1 package from the kustomize-resolved image, got %d: %+v", len(got), got)
	}
	if got[0].Org != "madfam-org" || got[0].Package != "nauta/web" {
		t.Errorf("expected madfam-org/nauta/web, got %+v", got[0])
	}
}

// A skipped run must announce itself. "pass: true" with no resolution is the
// shape that let an unmeasured repo look onboardable.
func TestRunImageGates_SkipWithoutTokenIsVisible(t *testing.T) {
	h := &Handler{config: &config.Config{}} // GitHubToken empty
	result, resolution, err := h.runImageGates(
		context.Background(), "madfam-org", "nauta", "infra/k8s/production", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected no gate result when skipping, got %+v", result)
	}
	if resolution.Ran {
		t.Error("resolution.Ran must be false when the gates never fetched manifests")
	}
	if resolution.SkipReason == "" {
		t.Error("a skipped run must carry a skip_reason")
	}
}

// The resolution block is documented in ONBOARDING_GUIDE.md as a flat object;
// the embedded stats struct must actually marshal that way.
func TestImageGateResolution_MarshalsFlat(t *testing.T) {
	body, err := json.Marshal(imageGateResolution{
		Ran:     true,
		Summary: "summary",
		ImageResolutionStats: checks.ImageResolutionStats{
			ManifestsScanned:    4,
			KustomizationFound:  true,
			KustomizationFile:   "kustomization.yaml",
			KustomizeEntries:    2,
			WorkloadImages:      3,
			ResolvedByKustomize: 3,
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{
		`"ran":true`,
		`"summary":"summary"`,
		`"manifests_scanned":4`,
		`"kustomization_found":true`,
		`"kustomization_file":"kustomization.yaml"`,
		`"kustomize_entries":2`,
		`"workload_images":3`,
		`"resolved_by_kustomize":3`,
	} {
		if !strings.Contains(string(body), key) {
			t.Errorf("resolution JSON %s missing %s", body, key)
		}
	}
}

// A kustomization we cannot interpret must reach the caller as an error so the
// gate fails closed, not as an empty package list that reads like a pass.
func TestExtractGHCRPackages_MalformedKustomizationIsError(t *testing.T) {
	manifests := []checks.ManifestFile{
		{
			Path: "kustomization.yaml",
			Content: `images:
  - name: web
    digest: sha256:abc
    newTag: v1
`,
		},
	}
	if _, _, err := checks.CollectWorkloadImages(manifests); err == nil {
		t.Fatal("expected an error for a kustomization setting both digest and newTag")
	}
}
