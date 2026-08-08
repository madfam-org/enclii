package checks

import (
	"strings"
	"testing"
)

// Gate behaviour with the kustomize `images:` transformer in play.
//
// Every case here goes through CheckImageDigestPinned — the same entry point
// the onboarding gate used before this file existed — so the table doubles as
// the regression proof: run it against the pre-transformer implementation and
// the resolution cases go red.
//
// The reproduction that motivated it: madfam-org/nauta's web-deployment.yaml
// carries a bare `image: ghcr.io/madfam-org/nauta/web` and its
// kustomization.yaml supplies the digest, exactly as CI's pin step writes it.
// `kustomize build` renders a fully pinned reference; the gate rejected it.

const bareNameDeployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: nauta
spec:
  template:
    spec:
      containers:
        - name: web
          image: web
`

// The nauta shape: full GHCR path in the manifest, digest supplied by the
// kustomization with a no-op newName (what `kustomize edit set image` writes).
const fullNameDeployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: nauta
spec:
  template:
    spec:
      containers:
        - name: web
          image: ghcr.io/madfam-org/nauta/web
`

const workerDeployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: worker
  namespace: nauta
spec:
  template:
    spec:
      containers:
        - name: worker
          image: worker
`

// web-api must NOT be matched by an images[] entry named `web`: kustomize
// matches on a prefix that ends at the reference, a ":" tag or an "@" digest.
const webAPIDeployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-api
spec:
  template:
    spec:
      containers:
        - name: web-api
          image: web-api
`

const nautaDigest = "sha256:a07d12ee70fe8537fe11daffe4f925d2b1c61ab2f319ccc39bfe3d38f0912bc4"

const zeroDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"

func kustomization(path, body string) ManifestFile {
	return ManifestFile{
		Path: path,
		Content: `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - web-deployment.yaml
` + body,
	}
}

func TestCheckImageDigestPinned_KustomizeImagesTransformer(t *testing.T) {
	cases := []struct {
		name string
		// manifests is the fetched manifest set, exactly as the gate sees it.
		manifests []ManifestFile
		// wantErr is a substring of the expected fail-closed error, "" when
		// the manifest set must be interpretable.
		wantErr string
		// wantImages are the EFFECTIVE images expected to be reported as
		// blockers, in order. Empty means the set must pass the gate.
		wantImages []string
		// wantMessages are substrings required in the corresponding messages.
		wantMessages []string
	}{
		{
			name: "digest override on a bare image name passes",
			manifests: []ManifestFile{
				{Path: "web-deployment.yaml", Content: bareNameDeployment},
				kustomization("kustomization.yaml", `images:
  - name: web
    newName: ghcr.io/madfam-org/nauta/web
    digest: `+nautaDigest+`
`),
			},
		},
		{
			name: "nauta shape: newName equal to name plus digest passes",
			manifests: []ManifestFile{
				{Path: "web-deployment.yaml", Content: fullNameDeployment},
				kustomization("kustomization.yaml", `images:
  - name: ghcr.io/madfam-org/nauta/web
    newName: ghcr.io/madfam-org/nauta/web
    digest: `+nautaDigest+`
`),
			},
		},
		{
			name: "digest with no newName passes and keeps the manifest name",
			manifests: []ManifestFile{
				{Path: "web-deployment.yaml", Content: fullNameDeployment},
				kustomization("kustomization.yaml", `images:
  - name: ghcr.io/madfam-org/nauta/web
    digest: `+nautaDigest+`
`),
			},
		},
		{
			name: "kustomization.yml spelling is honoured",
			manifests: []ManifestFile{
				{Path: "web-deployment.yaml", Content: bareNameDeployment},
				kustomization("kustomization.yml", `images:
  - name: web
    newName: ghcr.io/madfam-org/nauta/web
    digest: `+nautaDigest+`
`),
			},
		},
		{
			name: "extensionless Kustomization spelling is honoured",
			manifests: []ManifestFile{
				{Path: "web-deployment.yaml", Content: bareNameDeployment},
				kustomization("Kustomization", `images:
  - name: web
    newName: ghcr.io/madfam-org/nauta/web
    digest: `+nautaDigest+`
`),
			},
		},
		{
			name: "newTag still fails the digest rule on the resolved image",
			manifests: []ManifestFile{
				{Path: "web-deployment.yaml", Content: bareNameDeployment},
				kustomization("kustomization.yaml", `images:
  - name: web
    newName: ghcr.io/madfam-org/nauta/web
    newTag: v1.2.3
`),
			},
			wantImages:   []string{"ghcr.io/madfam-org/nauta/web:v1.2.3"},
			wantMessages: []string{"mutable tag"},
		},
		{
			name: "newName alone still fails: no tag, no digest",
			manifests: []ManifestFile{
				{Path: "web-deployment.yaml", Content: bareNameDeployment},
				kustomization("kustomization.yaml", `images:
  - name: web
    newName: ghcr.io/madfam-org/nauta/web
`),
			},
			wantImages:   []string{"ghcr.io/madfam-org/nauta/web"},
			wantMessages: []string{"not digest-pinned"},
		},
		{
			name: "all-zero placeholder digest fails",
			manifests: []ManifestFile{
				{Path: "web-deployment.yaml", Content: bareNameDeployment},
				kustomization("kustomization.yaml", `images:
  - name: web
    newName: ghcr.io/madfam-org/nauta/web
    digest: `+zeroDigest+`
`),
			},
			wantImages:   []string{"ghcr.io/madfam-org/nauta/web@" + zeroDigest},
			wantMessages: []string{"all-zero placeholder digest"},
		},
		{
			name: "all-zero placeholder written straight into the manifest fails",
			manifests: []ManifestFile{
				{Path: "web-deployment.yaml", Content: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  template:
    spec:
      containers:
        - name: web
          image: ghcr.io/madfam-org/nauta/web@` + zeroDigest + `
`},
			},
			wantImages:   []string{"ghcr.io/madfam-org/nauta/web@" + zeroDigest},
			wantMessages: []string{"all-zero placeholder digest"},
		},
		{
			name: "image with no matching entry is judged unchanged",
			manifests: []ManifestFile{
				{Path: "worker-deployment.yaml", Content: workerDeployment},
				kustomization("kustomization.yaml", `images:
  - name: web
    newName: ghcr.io/madfam-org/nauta/web
    digest: `+nautaDigest+`
`),
			},
			wantImages:   []string{"worker"},
			wantMessages: []string{"no images[] entry matched"},
		},
		{
			name: "entry name must not match a longer image name by prefix",
			manifests: []ManifestFile{
				{Path: "web-api-deployment.yaml", Content: webAPIDeployment},
				kustomization("kustomization.yaml", `images:
  - name: web
    newName: ghcr.io/madfam-org/nauta/web
    digest: `+nautaDigest+`
`),
			},
			wantImages:   []string{"web-api"},
			wantMessages: []string{"not digest-pinned"},
		},
		{
			name: "no kustomization present leaves the manifest value strict",
			manifests: []ManifestFile{
				{Path: "web-deployment.yaml", Content: bareNameDeployment},
			},
			wantImages:   []string{"web"},
			wantMessages: []string{"not digest-pinned"},
		},
		{
			name: "kustomization with an empty images list changes nothing",
			manifests: []ManifestFile{
				{Path: "web-deployment.yaml", Content: bareNameDeployment},
				kustomization("kustomization.yaml", "images: []\n"),
			},
			wantImages:   []string{"web"},
			wantMessages: []string{"no images[] entry matched"},
		},
		{
			name: "multiple workloads: only the overridden one passes",
			manifests: []ManifestFile{
				{Path: "web-deployment.yaml", Content: bareNameDeployment},
				{Path: "worker-deployment.yaml", Content: workerDeployment},
				{Path: "cron.yaml", Content: `apiVersion: batch/v1
kind: CronJob
metadata:
  name: reaper
spec:
  schedule: "0 * * * *"
  jobTemplate:
    spec:
      template:
        spec:
          containers:
            - name: reaper
              image: reaper:latest
`},
				kustomization("kustomization.yaml", `images:
  - name: web
    newName: ghcr.io/madfam-org/nauta/web
    digest: `+nautaDigest+`
`),
			},
			wantImages:   []string{"worker", "reaper:latest"},
			wantMessages: []string{"no images[] entry matched", "not digest-pinned"},
		},
		{
			name: "malformed kustomization is a gate failure, never a pass",
			manifests: []ManifestFile{
				{Path: "web-deployment.yaml", Content: bareNameDeployment},
				{Path: "kustomization.yaml", Content: "images:\n  - name: web\n   digest: bad-indent\n"},
			},
			wantErr: "cannot be parsed as YAML",
		},
		{
			name: "digest and newTag together is a manifest error, not a guess",
			manifests: []ManifestFile{
				{Path: "web-deployment.yaml", Content: bareNameDeployment},
				kustomization("kustomization.yaml", `images:
  - name: web
    digest: `+nautaDigest+`
    newTag: v1.2.3
`),
			},
			wantErr: "mutually exclusive",
		},
		{
			name: "entry without a name is a manifest error",
			manifests: []ManifestFile{
				{Path: "web-deployment.yaml", Content: bareNameDeployment},
				kustomization("kustomization.yaml", `images:
  - newName: ghcr.io/madfam-org/nauta/web
    digest: `+nautaDigest+`
`),
			},
			wantErr: "has no `name`",
		},
		{
			name: "duplicate entry names are ambiguous and rejected",
			manifests: []ManifestFile{
				{Path: "web-deployment.yaml", Content: bareNameDeployment},
				kustomization("kustomization.yaml", `images:
  - name: web
    digest: `+nautaDigest+`
  - name: web
    newName: ghcr.io/madfam-org/nauta/web
`),
			},
			wantErr: "repeats the name",
		},
		{
			name: "a digest carrying its own @ separator is rejected",
			manifests: []ManifestFile{
				{Path: "web-deployment.yaml", Content: bareNameDeployment},
				kustomization("kustomization.yaml", `images:
  - name: web
    newName: ghcr.io/madfam-org/nauta/web
    digest: "@`+nautaDigest+`"
`),
			},
			wantErr: "bare algorithm:hex value",
		},
		{
			name: "two kustomizations in one directory are ambiguous",
			manifests: []ManifestFile{
				{Path: "web-deployment.yaml", Content: bareNameDeployment},
				kustomization("kustomization.yaml", `images:
  - name: web
    newName: ghcr.io/madfam-org/nauta/web
    digest: `+nautaDigest+`
`),
				kustomization("kustomization.yml", "images: []\n"),
			},
			wantErr: "ambiguous",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issues, err := CheckImageDigestPinned(tc.manifests)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil (issues: %+v)", tc.wantErr, issues)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tc.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(issues) != len(tc.wantImages) {
				t.Fatalf("expected %d issue(s) %v, got %d: %+v",
					len(tc.wantImages), tc.wantImages, len(issues), issues)
			}
			for i, wantImage := range tc.wantImages {
				if issues[i].Image != wantImage {
					t.Errorf("issue %d: judged image = %q, want %q", i, issues[i].Image, wantImage)
				}
				if i < len(tc.wantMessages) && !strings.Contains(issues[i].Message, tc.wantMessages[i]) {
					t.Errorf("issue %d: message %q does not contain %q",
						i, issues[i].Message, tc.wantMessages[i])
				}
			}
		})
	}
}
