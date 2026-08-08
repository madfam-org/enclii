package checks

import (
	"strings"
	"testing"
)

// Resolution reporting and the transformer's unit semantics.
//
// The read-proof requirement drives most of this file: a gate that cannot
// distinguish "no kustomization here" from "kustomization present with an
// empty images: block" reports both as a clean pass, and a silently-empty
// transformer is exactly the state that would make the digest gate vacuous
// again.

func TestCollectWorkloadImages_ResolutionStats(t *testing.T) {
	cases := []struct {
		name      string
		manifests []ManifestFile
		want      ImageResolutionStats
	}{
		{
			name:      "empty input scans nothing and says so",
			manifests: nil,
			want:      ImageResolutionStats{},
		},
		{
			name: "no kustomization is distinguishable from an empty one",
			manifests: []ManifestFile{
				{Path: "web-deployment.yaml", Content: bareNameDeployment},
			},
			want: ImageResolutionStats{
				ManifestsScanned: 1,
				WorkloadImages:   1,
			},
		},
		{
			name: "kustomization with no images block is visible",
			manifests: []ManifestFile{
				{Path: "web-deployment.yaml", Content: bareNameDeployment},
				kustomization("kustomization.yaml", ""),
			},
			want: ImageResolutionStats{
				ManifestsScanned:   2,
				KustomizationFound: true,
				KustomizationFile:  "kustomization.yaml",
				WorkloadImages:     1,
			},
		},
		{
			name: "kustomization with an empty images list is visible",
			manifests: []ManifestFile{
				{Path: "web-deployment.yaml", Content: bareNameDeployment},
				kustomization("kustomization.yaml", "images: []\n"),
			},
			want: ImageResolutionStats{
				ManifestsScanned:   2,
				KustomizationFound: true,
				KustomizationFile:  "kustomization.yaml",
				WorkloadImages:     1,
			},
		},
		{
			name: "entries that match are counted",
			manifests: []ManifestFile{
				{Path: "web-deployment.yaml", Content: bareNameDeployment},
				{Path: "worker-deployment.yaml", Content: workerDeployment},
				kustomization("kustomization.yaml", `images:
  - name: web
    newName: ghcr.io/madfam-org/nauta/web
    digest: `+nautaDigest+`
  - name: unused
    newTag: v9
`),
			},
			want: ImageResolutionStats{
				ManifestsScanned:    3,
				KustomizationFound:  true,
				KustomizationFile:   "kustomization.yaml",
				KustomizeEntries:    2,
				WorkloadImages:      2,
				ResolvedByKustomize: 1,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, stats, err := CollectWorkloadImages(tc.manifests)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if stats != tc.want {
				t.Fatalf("stats = %+v, want %+v", stats, tc.want)
			}
			if stats.Summary() == "" {
				t.Error("Summary() must never be empty — it is what the operator reads")
			}
		})
	}
}

func TestImageResolutionStats_SummaryNamesTheKustomization(t *testing.T) {
	_, stats, err := CollectWorkloadImages([]ManifestFile{
		{Path: "web-deployment.yaml", Content: bareNameDeployment},
		kustomization("kustomization.yaml", `images:
  - name: web
    newName: ghcr.io/madfam-org/nauta/web
    digest: `+nautaDigest+`
`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	summary := stats.Summary()
	for _, want := range []string{"kustomization.yaml", "1 images[] entries", "1 resolved through a kustomization override"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary %q missing %q", summary, want)
		}
	}

	_, bare, err := CollectWorkloadImages([]ManifestFile{
		{Path: "web-deployment.yaml", Content: bareNameDeployment},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(bare.Summary(), "no kustomization found") {
		t.Errorf("summary %q must say no kustomization was found", bare.Summary())
	}
}

// A failing gate must say where the judged value came from, so the operator is
// not left diffing a Deployment against a kustomization by hand.
func TestCheckImageDigestPinnedImages_ReportsProvenance(t *testing.T) {
	images, _, err := CollectWorkloadImages([]ManifestFile{
		{Path: "web-deployment.yaml", Content: bareNameDeployment},
		kustomization("kustomization.yaml", `images:
  - name: web
    newName: ghcr.io/madfam-org/nauta/web
    newTag: v1.2.3
`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	issues := CheckImageDigestPinnedImages(images)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %+v", len(issues), issues)
	}
	issue := issues[0]
	if issue.Image != "ghcr.io/madfam-org/nauta/web:v1.2.3" {
		t.Errorf("judged image = %q, want the resolved value", issue.Image)
	}
	if issue.ManifestImage != "web" {
		t.Errorf("manifest image = %q, want %q", issue.ManifestImage, "web")
	}
	if issue.Source != ImageSourceKustomization {
		t.Errorf("source = %q, want %q", issue.Source, ImageSourceKustomization)
	}
	if issue.KustomizeEntry != "web" {
		t.Errorf("kustomize entry = %q, want %q", issue.KustomizeEntry, "web")
	}
	if issue.KustomizationFile != "kustomization.yaml" {
		t.Errorf("kustomization file = %q, want %q", issue.KustomizationFile, "kustomization.yaml")
	}
	if issue.Container != "web" {
		t.Errorf("container = %q, want %q", issue.Container, "web")
	}
	if !strings.Contains(issue.Message, `resolved from manifest image "web"`) {
		t.Errorf("message %q must name the manifest value it replaced", issue.Message)
	}
}

func TestCheckImageDigestPinnedImages_UnmatchedImageNamesTheKustomization(t *testing.T) {
	images, _, err := CollectWorkloadImages([]ManifestFile{
		{Path: "worker-deployment.yaml", Content: workerDeployment},
		kustomization("kustomization.yaml", `images:
  - name: web
    newName: ghcr.io/madfam-org/nauta/web
    digest: `+nautaDigest+`
`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	issues := CheckImageDigestPinnedImages(images)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Source != ImageSourceManifest {
		t.Errorf("source = %q, want %q", issues[0].Source, ImageSourceManifest)
	}
	if !strings.Contains(issues[0].Message, "did not rewrite it") {
		t.Errorf("message %q must explain that the kustomization did not match", issues[0].Message)
	}
}

func TestKustomizeImageResolver_Resolve(t *testing.T) {
	cases := []struct {
		name    string
		entries string
		image   string
		want    string
		entry   string
	}{
		{
			name:    "digest replaces a tag",
			entries: "images:\n  - name: web\n    digest: " + nautaDigest + "\n",
			image:   "web:v1",
			want:    "web@" + nautaDigest,
			entry:   "web",
		},
		{
			name:    "newName preserves an existing digest",
			entries: "images:\n  - name: web\n    newName: ghcr.io/madfam-org/nauta/web\n",
			image:   "web@" + nautaDigest,
			want:    "ghcr.io/madfam-org/nauta/web@" + nautaDigest,
			entry:   "web",
		},
		{
			name:    "newTag replaces an existing digest",
			entries: "images:\n  - name: web\n    newTag: v2\n",
			image:   "web@" + nautaDigest,
			want:    "web:v2",
			entry:   "web",
		},
		{
			name:    "entry matches a tagged reference",
			entries: "images:\n  - name: ghcr.io/madfam-org/nauta/web\n    digest: " + nautaDigest + "\n",
			image:   "ghcr.io/madfam-org/nauta/web:latest",
			want:    "ghcr.io/madfam-org/nauta/web@" + nautaDigest,
			entry:   "ghcr.io/madfam-org/nauta/web",
		},
		{
			name:    "unrelated image is untouched",
			entries: "images:\n  - name: web\n    digest: " + nautaDigest + "\n",
			image:   "docker.io/library/postgres:16",
			want:    "docker.io/library/postgres:16",
		},
		{
			name:    "registry port is not mistaken for a tag",
			entries: "images:\n  - name: localhost:5000/web\n    digest: " + nautaDigest + "\n",
			image:   "localhost:5000/web",
			want:    "localhost:5000/web@" + nautaDigest,
			entry:   "localhost:5000/web",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolver, err := NewKustomizeImageResolver([]ManifestFile{
				kustomization("kustomization.yaml", tc.entries),
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := resolver.Resolve(tc.image)
			if got.Image != tc.want {
				t.Errorf("Resolve(%q) = %q, want %q", tc.image, got.Image, tc.want)
			}
			if got.Original != tc.image {
				t.Errorf("Original = %q, want %q", got.Original, tc.image)
			}
			if got.Entry != tc.entry {
				t.Errorf("Entry = %q, want %q", got.Entry, tc.entry)
			}
			if got.Overridden() != (tc.entry != "") {
				t.Errorf("Overridden() = %v, want %v", got.Overridden(), tc.entry != "")
			}
		})
	}
}

func TestIsImageMatched_Unit(t *testing.T) {
	cases := []struct {
		image string
		name  string
		want  bool
	}{
		{"web", "web", true},
		{"web:v1", "web", true},
		{"web@sha256:abc", "web", true},
		{"web-api", "web", false},
		{"web-api:v1", "web", false},
		{"ghcr.io/madfam-org/nauta/web", "web", false},
		{"ghcr.io/madfam-org/nauta/web", "ghcr.io/madfam-org/nauta/web", true},
		{"ghcr.io/madfam-org/nauta/web@sha256:abc", "ghcr.io/madfam-org/nauta/web", true},
		{"web", "", false},
	}
	for _, c := range cases {
		if got := isImageMatched(c.image, c.name); got != c.want {
			t.Errorf("isImageMatched(%q, %q) = %v, want %v", c.image, c.name, got, c.want)
		}
	}
}

func TestSplitImageNameTag_Unit(t *testing.T) {
	cases := []struct {
		image      string
		wantName   string
		wantSuffix string
	}{
		{"web", "web", ""},
		{"web:v1", "web", ":v1"},
		{"ghcr.io/org/pkg", "ghcr.io/org/pkg", ""},
		{"ghcr.io/org/pkg:v1", "ghcr.io/org/pkg", ":v1"},
		{"ghcr.io/org/pkg@sha256:abc", "ghcr.io/org/pkg", "@sha256:abc"},
		{"localhost:5000/pkg", "localhost:5000/pkg", ""},
		{"localhost:5000/pkg:v1", "localhost:5000/pkg", ":v1"},
	}
	for _, c := range cases {
		name, suffix := splitImageNameTag(c.image)
		if name != c.wantName || suffix != c.wantSuffix {
			t.Errorf("splitImageNameTag(%q) = (%q, %q), want (%q, %q)",
				c.image, name, suffix, c.wantName, c.wantSuffix)
		}
	}
}

func TestIsKustomizationFile_Unit(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"kustomization.yaml", true},
		{"kustomization.yml", true},
		{"Kustomization", true},
		{"infra/k8s/production/kustomization.yaml", true},
		{"kustomization.json", false},
		{"my-kustomization.yaml", false},
		{"deployment.yaml", false},
	}
	for _, c := range cases {
		if got := IsKustomizationFile(c.path); got != c.want {
			t.Errorf("IsKustomizationFile(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestPlaceholderDigestMessage_Unit(t *testing.T) {
	cases := []struct {
		image       string
		wantMessage bool
	}{
		{"ghcr.io/org/pkg@" + zeroDigest, true},
		{"ghcr.io/org/pkg@sha256:0", true},
		{"ghcr.io/org/pkg@sha256:", true},
		{"ghcr.io/org/pkg@" + nautaDigest, false},
		{"ghcr.io/org/pkg:v1", false},
	}
	for _, c := range cases {
		got := placeholderDigestMessage(c.image)
		if (got != "") != c.wantMessage {
			t.Errorf("placeholderDigestMessage(%q) = %q, want message: %v",
				c.image, got, c.wantMessage)
		}
	}
}
