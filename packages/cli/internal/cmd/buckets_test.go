package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

// TestStorageCommandStructure confirms the day-2 lifecycle is wired with the
// same create/ls/destroy shape as `enclii addon`.
func TestStorageCommandStructure(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "http://localhost:0"}
	cmd := NewStorageCommand(cfg)
	require.NotNil(t, cmd)
	assert.Equal(t, "buckets", cmd.Name())

	names := map[string]bool{}
	for _, sub := range cmd.Commands() {
		names[sub.Name()] = true
	}
	assert.True(t, names["create"], "storage create must exist")
	assert.True(t, names["ls"], "storage ls must exist")
	assert.True(t, names["destroy"], "storage destroy must exist")
}

func TestStorageRegisteredOnRoot(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "http://localhost:0"}
	root := NewRootCommand(cfg)

	names := map[string]bool{}
	for _, sub := range root.Commands() {
		names[sub.Name()] = true
	}
	assert.True(t, names["buckets"], "enclii buckets must be a top-level command")
}

// TestBucketsNameDoesNotCollide guards the reason this command is not called
// `storage`: `enclii volumes` already claims that alias for cluster block
// storage, and cobra resolves the earlier registration first — a `storage`
// command would silently never run.
func TestBucketsNameDoesNotCollide(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "http://localhost:0"}
	root := NewRootCommand(cfg)

	// Every name this command answers to must be claimed by it alone.
	// (Note: the root command tree has a separate pre-existing collision —
	// `addon`'s "db" alias is shadowed by the top-level `db` command — which
	// is out of scope here.)
	for _, name := range []string{"buckets", "bucket", "r2", "object-storage"} {
		claimed := []string{}
		for _, sub := range root.Commands() {
			for _, candidate := range append([]string{sub.Name()}, sub.Aliases...) {
				if candidate == name {
					claimed = append(claimed, sub.Name())
				}
			}
		}
		assert.Equal(t, []string{"buckets"}, claimed,
			"%q must resolve to the buckets command and nothing else", name)

		found, _, err := root.Find([]string{name, "create"})
		require.NoError(t, err)
		assert.Equal(t, "create", found.Name(), "%q create must resolve", name)
		assert.Equal(t, "buckets", found.Parent().Name(), "%q must reach the buckets tree", name)
	}
}

func TestStorageCreateRequiresProject(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "http://localhost:0"}
	root := NewRootCommand(cfg)
	root.SetArgs([]string{"buckets", "create", "my-bucket"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project is required")
}

// TestStorageCreateTargetsProjectScopedEndpoint pins the day-2 contract: the
// call must hit the project's storage endpoint and nothing else. Onboarding
// endpoints (ArgoCD registration, domains, namespaces) must never be touched.
func TestStorageCreateTargetsProjectScopedEndpoint(t *testing.T) {
	var seenPaths []string
	var body map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPaths = append(seenPaths, r.Method+" "+r.URL.Path)
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"bucket":{"bucket":"karafiel-documents","namespace":"karafiel",
			"secret_name":"karafiel-credentials","endpoint":"https://acct.r2.cloudflarestorage.com",
			"action":"provisioned","secret_keys":["R2_BUCKET_NAME","R2_ENDPOINT_URL","R2_ACCESS_KEY_ID","R2_SECRET_ACCESS_KEY","STORAGE_BACKEND"]},
			"message":"Bucket provisioned with its own scoped credentials"}`))
	}))
	defer srv.Close()

	cfg := &config.Config{APIEndpoint: srv.URL, APIToken: "tok"}
	root := NewRootCommand(cfg)
	out := &strings.Builder{}
	root.SetArgs([]string{"buckets", "create", "karafiel-documents", "--project", "karafiel"})
	root.SetOut(out)
	root.SetErr(io.Discard)

	require.NoError(t, root.Execute())

	require.Len(t, seenPaths, 1, "storage create must make exactly one API call")
	assert.Equal(t, "POST /v1/projects/karafiel/storage/buckets", seenPaths[0])
	assert.Equal(t, "karafiel-documents", body["bucket_name"])

	rendered := out.String()
	assert.Contains(t, rendered, "karafiel-documents")
	assert.Contains(t, rendered, "karafiel/karafiel-credentials")
	// The output names the keys, never their values.
	assert.NotContains(t, rendered, "R2_SECRET_ACCESS_KEY=")
}

func TestStorageCreateRotateFlag(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"bucket":{"bucket":"b","action":"rotated","secret_keys":[]},"message":"ok"}`))
	}))
	defer srv.Close()

	cfg := &config.Config{APIEndpoint: srv.URL, APIToken: "tok"}
	root := NewRootCommand(cfg)
	root.SetArgs([]string{"buckets", "create", "my-bucket", "--project", "p", "--rotate"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	require.NoError(t, root.Execute())
	assert.Equal(t, true, body["rotate"])
}

// TestStorageDestroyRequiresConfirmation: revocation is immediately breaking,
// so it must not happen without --yes.
func TestStorageDestroyRequiresConfirmation(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &config.Config{APIEndpoint: srv.URL, APIToken: "tok"}
	root := NewRootCommand(cfg)
	root.SetArgs([]string{"buckets", "destroy", "my-bucket", "--project", "p"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "confirmation required")
	assert.False(t, called, "no API call may be made without --yes")
}

// TestStorageLsFailsOnCriticalFinding makes `storage ls` usable as a CI gate.
func TestStorageLsFailsOnCriticalFinding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"namespace":"karafiel","count":1,
			"buckets":[{"namespace":"karafiel","secret_name":"r2-credentials","bucket":"tezca-documents",
				"storage_backend":"r2","has_access_key_id":true,"has_secret_access_key":true,"managed":false}],
			"findings":[{"severity":"critical","kind":"shared_credentials","namespace":"karafiel",
				"secret":"r2-credentials","message":"the same R2 access key is installed in 2 namespaces",
				"remediation":"enclii buckets create tezca-documents --project karafiel --rotate"}]}`))
	}))
	defer srv.Close()

	cfg := &config.Config{APIEndpoint: srv.URL, APIToken: "tok"}
	root := NewRootCommand(cfg)
	out := &strings.Builder{}
	root.SetArgs([]string{"buckets", "ls", "--project", "karafiel"})
	root.SetOut(out)
	root.SetErr(io.Discard)

	err := root.Execute()
	require.Error(t, err, "a critical finding must fail the command")
	assert.Contains(t, err.Error(), "critical")
	assert.Contains(t, out.String(), "tezca-documents")
}

func TestStorageLsCleanNamespaceSucceeds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"namespace":"karafiel","count":1,
			"buckets":[{"namespace":"karafiel","secret_name":"karafiel-credentials","bucket":"karafiel-documents",
				"storage_backend":"r2","has_access_key_id":true,"has_secret_access_key":true,"managed":true}],
			"findings":[]}`))
	}))
	defer srv.Close()

	cfg := &config.Config{APIEndpoint: srv.URL, APIToken: "tok"}
	root := NewRootCommand(cfg)
	out := &strings.Builder{}
	root.SetArgs([]string{"buckets", "ls", "--project", "karafiel"})
	root.SetOut(out)
	root.SetErr(io.Discard)

	require.NoError(t, root.Execute())
	assert.Contains(t, out.String(), "complete")
}

// TestOpsStorageR2AuditRegistered confirms the drift guard is reachable from
// the existing operator surface rather than a new subsystem.
func TestOpsStorageR2AuditRegistered(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "http://localhost:0"}
	storage := findSubcommand(NewOpsCommand(cfg), "storage")
	require.NotNil(t, storage)
	assert.NotNil(t, findSubcommand(storage, "r2-audit"),
		"enclii ops storage r2-audit must exist")
}
