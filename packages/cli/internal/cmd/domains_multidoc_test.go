package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

// End-to-end coverage for gotcha 9 through the actual `domains add` /
// `domains list` code path. Before the fix, resolveService decoded only the
// first document of the onboard-standard enclii.yaml — `kind: Project` —
// and failed with "service spec validation failed", so neither command ever
// reached the API. These tests assert the commands now resolve the service
// and construct the expected request paths.

const domainsTwoDocManifest = `apiVersion: enclii.dev/v1
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

// recordingAPI stands in for switchyard-api and records the paths the CLI
// requests, so the tests can assert path construction.
type recordingAPI struct {
	mu        sync.Mutex
	paths     []string
	serviceID uuid.UUID
	envID     uuid.UUID
}

func (r *recordingAPI) record(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paths = append(r.paths, path)
}

func (r *recordingAPI) requested(path string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.paths {
		if p == path {
			return true
		}
	}
	return false
}

func newRecordingAPI(t *testing.T) (*recordingAPI, string) {
	t.Helper()
	api := &recordingAPI{serviceID: uuid.New(), envID: uuid.New()}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		api.record(req.URL.Path)
		w.Header().Set("Content-Type", "application/json")

		switch {
		case req.URL.Path == "/v1/projects/kalya/services":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"services": []map[string]any{
					{"id": api.serviceID, "name": "kalya-api", "project_id": uuid.New()},
				},
			})
		case req.URL.Path == "/v1/projects/kalya/environments":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"environments": []map[string]any{
					{"id": api.envID, "name": "production"},
				},
			})
		case strings.HasSuffix(req.URL.Path, "/domains") && req.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"domains": []any{}})
		case strings.HasSuffix(req.URL.Path, "/domains") && req.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         uuid.New(),
				"domain":     "app.kalya.app",
				"dns_cname":  "tunnel.cfargotunnel.com",
				"created_at": "2026-08-21T00:00:00Z",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"unexpected path"}`))
		}
	}))
	t.Cleanup(srv.Close)

	return api, srv.URL
}

// writeTwoDocManifest drops the manifest in a temp dir and chdirs there,
// because the spec parser resolves the project root from the cwd.
func writeTwoDocManifest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "enclii.yaml")
	require.NoError(t, os.WriteFile(path, []byte(domainsTwoDocManifest), 0o644))
	// Satisfy build-type auto-detection.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"t"}`), 0o644))

	original, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(original) })

	return path
}

func TestDomainsList_AcceptsTwoDocumentManifest(t *testing.T) {
	manifest := writeTwoDocManifest(t)
	api, endpoint := newRecordingAPI(t)
	cfg := &config.Config{APIEndpoint: endpoint, APIToken: "test-token"}

	err := runDomainsList(cfg, "kalya-api", "", manifest, false)
	require.NoError(t, err, "two-document enclii.yaml must not fail service spec validation")

	assert.True(t, api.requested("/v1/projects/kalya/services"),
		"expected the project slug to come from the Service document")
	assert.True(t, api.requested("/v1/services/"+api.serviceID.String()+"/domains"),
		"expected the domains list path for the resolved service, got %v", api.paths)
}

func TestDomainsAdd_AcceptsTwoDocumentManifest(t *testing.T) {
	manifest := writeTwoDocManifest(t)
	api, endpoint := newRecordingAPI(t)
	cfg := &config.Config{APIEndpoint: endpoint, APIToken: "test-token"}

	err := runDomainsAdd(cfg, "app.kalya.app", "kalya-api", "production", manifest, true, "")
	require.NoError(t, err, "two-document enclii.yaml must not fail service spec validation")

	assert.True(t, api.requested("/v1/projects/kalya/environments"),
		"expected the environment lookup, got %v", api.paths)
	assert.True(t, api.requested("/v1/services/"+api.serviceID.String()+"/domains"),
		"expected the domains add path for the resolved service, got %v", api.paths)
}

// Without --service the Service document still resolves, because there is
// exactly one in the manifest.
func TestDomainsList_TwoDocumentManifestWithoutServiceFlag(t *testing.T) {
	manifest := writeTwoDocManifest(t)
	api, endpoint := newRecordingAPI(t)
	cfg := &config.Config{APIEndpoint: endpoint, APIToken: "test-token"}

	err := runDomainsList(cfg, "", "", manifest, false)
	require.NoError(t, err)
	assert.True(t, api.requested("/v1/services/"+api.serviceID.String()+"/domains"),
		"expected the service name to come from the Service document, got %v", api.paths)
}
