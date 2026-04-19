package checks

import (
	"strings"
	"testing"
)

// Deployment manifest fixtures. Kept inline so the test reads in one page.
const goodDeployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: cotiza-api
  namespace: digifab
spec:
  replicas: 1
  selector:
    matchLabels:
      app: cotiza-api
  template:
    metadata:
      labels:
        app: cotiza-api
    spec:
      containers:
        - name: api
          image: ghcr.io/madfam-org/cotiza/cotiza-api@sha256:deadbeefcafef00dbaadf00ddeadbeefcafef00dbaadf00ddeadbeefcafef00d
`

const latestTagDeployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: avala-web
  namespace: avala
spec:
  template:
    spec:
      containers:
        - name: web
          image: ghcr.io/madfam-org/avala/avala-web:latest
`

const mutableTagDeployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: forj-web
  namespace: forj
spec:
  template:
    spec:
      containers:
        - name: web
          image: ghcr.io/madfam-org/forj/forj-web:v1.2.3
`

// CronJob nests the podSpec under jobTemplate — if we don't handle that
// the gate silently passes CronJobs with :latest images.
const cronjobLatest = `apiVersion: batch/v1
kind: CronJob
metadata:
  name: digest-cron
  namespace: ns
spec:
  schedule: "0 * * * *"
  jobTemplate:
    spec:
      template:
        spec:
          containers:
            - name: worker
              image: ghcr.io/madfam-org/stale/worker:latest
`

// Multi-doc: Deployment (bad) + Service (irrelevant). Must only flag the
// Deployment.
const multiDoc = `apiVersion: v1
kind: Service
metadata:
  name: cotiza-api
spec:
  ports:
    - port: 80
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cotiza-api
spec:
  template:
    spec:
      containers:
        - name: api
          image: ghcr.io/madfam-org/cotiza/cotiza-api:main
`

func TestCheckImageDigestPinned_Good(t *testing.T) {
	issues, err := CheckImageDigestPinned([]ManifestFile{
		{Path: "deployment.yaml", Content: goodDeployment},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected 0 issues, got %d: %+v", len(issues), issues)
	}
}

func TestCheckImageDigestPinned_LatestTag(t *testing.T) {
	issues, err := CheckImageDigestPinned([]ManifestFile{
		{Path: "avala-web.yaml", Content: latestTagDeployment},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Kind != "Deployment" || issues[0].Name != "avala-web" {
		t.Fatalf("wrong resource reported: %+v", issues[0])
	}
	if !strings.Contains(issues[0].Message, ":latest") {
		t.Fatalf("expected :latest mention in message, got %q", issues[0].Message)
	}
	if issues[0].Severity != "blocker" {
		t.Fatalf("expected severity=blocker, got %q", issues[0].Severity)
	}
}

func TestCheckImageDigestPinned_MutableTag(t *testing.T) {
	issues, err := CheckImageDigestPinned([]ManifestFile{
		{Path: "forj-web.yaml", Content: mutableTagDeployment},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if !strings.Contains(issues[0].Message, "mutable tag") {
		t.Fatalf("expected mutable-tag mention, got %q", issues[0].Message)
	}
}

func TestCheckImageDigestPinned_CronJob(t *testing.T) {
	// If this test fails, CronJob workloads sneak :latest past the gate.
	issues, err := CheckImageDigestPinned([]ManifestFile{
		{Path: "cron.yaml", Content: cronjobLatest},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Kind != "CronJob" {
		t.Fatalf("expected Kind=CronJob, got %q", issues[0].Kind)
	}
}

func TestCheckImageDigestPinned_MultiDoc(t *testing.T) {
	issues, err := CheckImageDigestPinned([]ManifestFile{
		{Path: "multi.yaml", Content: multiDoc},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue from the Deployment only, got %d", len(issues))
	}
	if issues[0].Kind != "Deployment" {
		t.Fatalf("expected Kind=Deployment, got %q", issues[0].Kind)
	}
}

func TestCheckImageDigestPinned_EmptyInput(t *testing.T) {
	issues, err := CheckImageDigestPinned(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected 0 issues for nil input")
	}
}

func TestCheckImageDigestPinned_IgnoresNonWorkloadKinds(t *testing.T) {
	// Pods and random CRDs aren't part of the workload kind set. We don't
	// want the gate emitting noise for an Ingress that happens to list an
	// image field (hypothetical) or a ConfigMap.
	input := `apiVersion: v1
kind: ConfigMap
metadata:
  name: image
data:
  image: "latest"
`
	issues, err := CheckImageDigestPinned([]ManifestFile{
		{Path: "cm.yaml", Content: input},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected 0 issues on non-workload kinds, got %d", len(issues))
	}
}

func TestImageIsDigestPinned_Unit(t *testing.T) {
	cases := []struct {
		image string
		want  bool
	}{
		{"ghcr.io/foo/bar@sha256:abcd", true},
		{"foo/bar:latest", false},
		{"foo/bar:v1.0", false},
		{"foo/bar", false},
		{"", false},
	}
	for _, c := range cases {
		got := imageIsDigestPinned(c.image)
		if got != c.want {
			t.Errorf("imageIsDigestPinned(%q) = %v, want %v", c.image, got, c.want)
		}
	}
}
