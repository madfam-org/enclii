package export

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

type fakeStorage struct {
	mu      sync.Mutex
	uploads map[string][]byte
	deletes []string
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{uploads: map[string][]byte{}}
}

func (f *fakeStorage) Upload(ctx context.Context, key string, body io.Reader, contentType string) error {
	buf, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uploads[key] = buf
	return nil
}

func (f *fakeStorage) Delete(ctx context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes = append(f.deletes, key)
	delete(f.uploads, key)
	return nil
}

func (f *fakeStorage) GetPresignedURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	return "https://r2.example.com/" + key + "?sig=fake", nil
}

func (f *fakeStorage) get(key string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.uploads[key]
}

type fakeBundleProvider struct {
	bundle *ProjectBundle
	err    error
}

func (f *fakeBundleProvider) Fetch(ctx context.Context, projectID uuid.UUID) (*ProjectBundle, error) {
	return f.bundle, f.err
}

type fakeDumpProvider struct {
	dumps []DBDump
}

func (f *fakeDumpProvider) Dump(ctx context.Context, addons []*types.DatabaseAddon) ([]DBDump, error) {
	return f.dumps, nil
}

type fakeBlobProvider struct {
	manifests []BlobManifest
}

func (f *fakeBlobProvider) ListProjectBlobs(ctx context.Context, projectSlug string) ([]BlobManifest, error) {
	return f.manifests, nil
}

type fakeSecretProvider struct {
	refs []SecretReference
}

func (f *fakeSecretProvider) ListSecretReferences(ctx context.Context, projectSlug string) ([]SecretReference, error) {
	return f.refs, nil
}

type fakeAuditProvider struct {
	timeline    []AuditEvent
	deployments []AuditEvent
}

func (f *fakeAuditProvider) ProjectEvents(ctx context.Context, projectID uuid.UUID) ([]AuditEvent, []AuditEvent, error) {
	return f.timeline, f.deployments, nil
}

type fakeNotifier struct {
	mu       sync.Mutex
	readyTo  []string
	approval []string
}

func (f *fakeNotifier) ExportReady(ctx context.Context, to, projectSlug, exportID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readyTo = append(f.readyTo, to)
	return nil
}

func (f *fakeNotifier) ExportApprovalRequested(ctx context.Context, projectSlug, exportID, requestedBy string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.approval = append(f.approval, requestedBy)
	return nil
}

// ---------------------------------------------------------------------------
// Tests for assembly logic (service pipeline without DB)
// ---------------------------------------------------------------------------

func TestPipeline_AssemblesTarballWithAllSections(t *testing.T) {
	projID := uuid.New()
	svcID := uuid.New()
	project := &types.Project{
		ID: projID, Name: "Acme", Slug: "acme",
		CIRunnerMode: types.CIRunnerModeGitHub,
		CreatedAt:    time.Now().UTC(),
	}

	bundle := &ProjectBundle{
		Project: project,
		Services: []*types.Service{
			{ID: svcID, ProjectID: projID, Name: "api"},
		},
		EnvVars: []*EnvVarSnapshot{
			{ServiceID: svcID, ServiceName: "api", Key: "LOG_LEVEL", Kind: "plain", Value: "info"},
		},
	}

	// Build in-memory to avoid going through Service.runPipeline's DB.
	b := NewBuilder()
	AddReadme(b, project.Slug, uuid.New().String(), time.Now().UTC())
	if err := AddProjectManifests(b, bundle); err != nil {
		t.Fatalf("manifests: %v", err)
	}
	if err := AddSecretReferences(b, []SecretReference{
		{Name: "postgres-credentials", Type: "Opaque", KeyCount: 2},
	}); err != nil {
		t.Fatalf("secrets: %v", err)
	}
	if err := AddDatabaseDumps(b, []DBDump{{
		AddonName: "maindb",
		DumpGz:    []byte("GZIP-OPAQUE"),
		SchemaSQL: []byte("CREATE TABLE foo();\n"),
	}}); err != nil {
		t.Fatalf("dumps: %v", err)
	}
	if err := AddAuditTimeline(b,
		[]AuditEvent{{Source: "selva", Action: "test"}},
		[]AuditEvent{{Source: "switchyard", Action: "deploy"}},
	); err != nil {
		t.Fatalf("audit: %v", err)
	}

	parts, manifest, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	expected := []string{
		"README.md",
		"manifests/project.yaml",
		"manifests/services/api.yaml",
		"manifests/envvars/api.json",
		"secrets/references.json",
		"databases/maindb/pg_dump.sql.gz",
		"databases/maindb/schema.sql",
		"audit/timeline.ndjson",
		"audit/deployments.ndjson",
	}
	got := map[string]bool{}
	for _, f := range manifest.Files {
		got[f.Path] = true
	}
	for _, p := range expected {
		if !got[p] {
			t.Errorf("missing expected path %q; got %v", p, manifestPaths(manifest))
		}
	}
}

func TestCrossProjectLeak_ByProjectID(t *testing.T) {
	// Construct two project bundles. A has secret DB_PASS=<redacted>,
	// B has API_KEY=<redacted>. The export for project A must contain
	// only A's env-vars.
	projA := &types.Project{ID: uuid.New(), Slug: "acme-a", Name: "A"}
	projB := &types.Project{ID: uuid.New(), Slug: "acme-b", Name: "B"}

	svcA := uuid.New()
	svcB := uuid.New()

	bundleA := &ProjectBundle{
		Project:  projA,
		Services: []*types.Service{{ID: svcA, ProjectID: projA.ID, Name: "api-a"}},
		EnvVars: []*EnvVarSnapshot{
			{ServiceID: svcA, ServiceName: "api-a", Key: "DB_PASS", Kind: "secret", Value: "<redacted>"},
		},
	}
	bundleB := &ProjectBundle{
		Project:  projB,
		Services: []*types.Service{{ID: svcB, ProjectID: projB.ID, Name: "api-b"}},
		EnvVars: []*EnvVarSnapshot{
			{ServiceID: svcB, ServiceName: "api-b", Key: "API_KEY", Kind: "secret", Value: "<redacted>"},
		},
	}

	bA := NewBuilder()
	if err := AddProjectManifests(bA, bundleA); err != nil {
		t.Fatal(err)
	}

	bB := NewBuilder()
	if err := AddProjectManifests(bB, bundleB); err != nil {
		t.Fatal(err)
	}

	partsA, _, err := bA.Build()
	if err != nil {
		t.Fatal(err)
	}

	// Tarball for project A must not contain anything named after B.
	entries, err := ReadTarball(partsA[0].Data)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if bytes.Contains([]byte(e.Path), []byte("api-b")) {
			t.Errorf("project A tarball leaked project B service: %s", e.Path)
		}
	}
}

func TestFakeStorageUploadRoundTrip(t *testing.T) {
	// Sanity check the fake used by later service-integration tests.
	fs := newFakeStorage()
	ctx := context.Background()
	err := fs.Upload(ctx, "x/y", bytes.NewReader([]byte("data")), "application/octet-stream")
	if err != nil {
		t.Fatal(err)
	}
	if string(fs.get("x/y")) != "data" {
		t.Errorf("upload missing")
	}
	_ = fs.Delete(ctx, "x/y")
	if fs.get("x/y") != nil {
		t.Errorf("delete didn't clear")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func manifestPaths(m Manifest) []string {
	out := make([]string, 0, len(m.Files))
	for _, f := range m.Files {
		out = append(out, f.Path)
	}
	return out
}

// silenceLogs returns a logrus logger that discards output during tests.
func silenceLogs() *logrus.Logger {
	l := logrus.New()
	l.SetOutput(io.Discard)
	return l
}
