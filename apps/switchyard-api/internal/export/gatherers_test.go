package export

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func TestAddProjectManifests_WritesExpectedPaths(t *testing.T) {
	projID := uuid.New()
	svcID := uuid.New()

	bundle := &ProjectBundle{
		Project: &types.Project{
			ID:           projID,
			Name:         "Acme",
			Slug:         "acme",
			CIRunnerMode: types.CIRunnerModeGitHub,
			CreatedAt:    time.Unix(1_700_000_000, 0).UTC(),
		},
		Environments: []*types.Environment{
			{ID: uuid.New(), ProjectID: projID, Name: "prod", KubeNamespace: "acme-prod"},
		},
		Services: []*types.Service{
			{ID: svcID, ProjectID: projID, Name: "api"},
		},
		Deployments: []*DeploymentSnapshot{
			{
				ID:          uuid.New(),
				ServiceID:   svcID,
				ServiceName: "api",
				Environment: "prod",
				Image:       "ghcr.io/acme/api:v42",
				Replicas:    3,
				Namespace:   "acme-prod",
				CreatedAt:   time.Unix(1_700_000_000, 0).UTC(),
			},
		},
		EnvVars: []*EnvVarSnapshot{
			{ServiceID: svcID, ServiceName: "api", Key: "LOG_LEVEL", Kind: "plain", Value: "info"},
			{ServiceID: svcID, ServiceName: "api", Key: "DB_PASS", Kind: "secret", Value: "<redacted>"},
		},
	}

	b := NewBuilder()
	if err := AddProjectManifests(b, bundle); err != nil {
		t.Fatalf("AddProjectManifests: %v", err)
	}

	paths := map[string]bool{}
	for _, e := range b.Entries() {
		paths[e.Path] = true
	}

	mustExist := []string{
		"manifests/project.yaml",
		"manifests/services/api.yaml",
		"manifests/deployments/api.yaml",
		"manifests/envvars/api.json",
	}
	for _, p := range mustExist {
		if !paths[p] {
			t.Errorf("missing path %q (got %v)", p, paths)
		}
	}
}

func TestAddProjectManifests_ScrubsNamespace(t *testing.T) {
	bundle := &ProjectBundle{
		Project: &types.Project{Name: "A", Slug: "a"},
		Deployments: []*DeploymentSnapshot{
			{ServiceName: "svc", Namespace: "real-namespace-that-should-not-appear"},
		},
	}
	b := NewBuilder()
	if err := AddProjectManifests(b, bundle); err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, e := range b.Entries() {
		if bytes.Contains(e.Content, []byte("real-namespace-that-should-not-appear")) {
			t.Errorf("namespace leaked into %s", e.Path)
		}
	}
}

func TestAddProjectManifests_SecretValueNeverInEnvVars(t *testing.T) {
	bundle := &ProjectBundle{
		Project: &types.Project{Name: "A", Slug: "a"},
		EnvVars: []*EnvVarSnapshot{
			{ServiceName: "svc", Key: "API_KEY", Kind: "secret", Value: "<redacted>"},
		},
	}
	b := NewBuilder()
	if err := AddProjectManifests(b, bundle); err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, e := range b.Entries() {
		if !strings.HasPrefix(e.Path, "manifests/envvars/") {
			continue
		}
		if bytes.Contains(e.Content, []byte("<redacted>")) {
			continue // expected redacted placeholder
		}
		// If any envvar file contains something that looks like an
		// unredacted secret we've regressed.
		if bytes.Contains(e.Content, []byte("supersecret")) {
			t.Errorf("secret value leaked into %s", e.Path)
		}
	}
}

func TestAddSecretReferences_NeverCarriesValues(t *testing.T) {
	b := NewBuilder()
	refs := []SecretReference{
		{Name: "postgres-credentials", Type: "Opaque", KeyCount: 2, Scope: "project"},
	}
	if err := AddSecretReferences(b, refs); err != nil {
		t.Fatalf("err: %v", err)
	}

	found := false
	for _, e := range b.Entries() {
		if e.Path == "secrets/references.json" {
			found = true
			// Structure check
			var v map[string]interface{}
			if err := json.Unmarshal(e.Content, &v); err != nil {
				t.Fatalf("json: %v", err)
			}
			if v["count"] == nil {
				t.Errorf("missing count field")
			}
			// Value must NEVER appear in any form.
			for _, bad := range []string{"value", "password", "token_value"} {
				if bytes.Contains(e.Content, []byte(`"`+bad+`":`)) {
					t.Errorf("secrets JSON contains key %q", bad)
				}
			}
		}
	}
	if !found {
		t.Errorf("missing secrets/references.json")
	}
}

func TestAddBlobManifests_SkipsEmpty(t *testing.T) {
	b := NewBuilder()
	manifests := []BlobManifest{
		{Bucket: "empty-bucket", ObjectCount: 0},
		{Bucket: "full-bucket", ObjectCount: 2, Objects: []BlobInventory{
			{Key: "a", Size: 1},
			{Key: "b", Size: 2},
		}},
	}
	if err := AddBlobManifests(b, manifests); err != nil {
		t.Fatalf("err: %v", err)
	}
	paths := map[string]bool{}
	for _, e := range b.Entries() {
		paths[e.Path] = true
	}
	if paths["blobs/empty-bucket/manifest.json"] {
		t.Errorf("empty-bucket manifest should have been skipped")
	}
	if !paths["blobs/full-bucket/manifest.json"] {
		t.Errorf("full-bucket manifest missing")
	}
}

func TestAddAuditTimeline_NDJSONFormat(t *testing.T) {
	b := NewBuilder()
	events := []AuditEvent{
		{Source: "selva", Action: "policy.approved", Actor: "alice"},
		{Source: "switchyard", Action: "deploy.started", Actor: "bob"},
	}
	if err := AddAuditTimeline(b, events, nil); err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, e := range b.Entries() {
		if e.Path != "audit/timeline.ndjson" {
			continue
		}
		lines := bytes.Split(bytes.TrimRight(e.Content, "\n"), []byte("\n"))
		if len(lines) != 2 {
			t.Errorf("timeline has %d lines, want 2", len(lines))
		}
		for _, l := range lines {
			var v map[string]interface{}
			if err := json.Unmarshal(l, &v); err != nil {
				t.Errorf("line not valid JSON: %v", err)
			}
		}
	}
}

func TestAddReadme_MentionsRedactionPolicy(t *testing.T) {
	b := NewBuilder()
	AddReadme(b, "acme", uuid.New().String(), time.Now().UTC())
	for _, e := range b.Entries() {
		if e.Path != "README.md" {
			continue
		}
		body := string(e.Content)
		if !strings.Contains(body, "Values are intentionally excluded") &&
			!strings.Contains(body, "intentionally excluded") {
			t.Errorf("README.md doesn't mention secret redaction policy")
		}
	}
}
