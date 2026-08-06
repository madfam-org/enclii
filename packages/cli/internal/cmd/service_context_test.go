package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func TestParseLocalServiceContexts_MultiDocEncliiYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".enclii.yml")
	requireWriteFile(t, path, `apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: api
  project: platform
---
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: worker
  project: platform
`)

	services := parseLocalServiceContexts(path)

	assert.Len(t, services, 2)
	assert.Equal(t, localServiceContext{Name: "api", Project: "platform"}, services[0])
	assert.Equal(t, localServiceContext{Name: "worker", Project: "platform"}, services[1])
}

func TestProjectForService_PicksMatchingDocProject(t *testing.T) {
	services := []localServiceContext{
		{Name: "api", Project: "platform"},
		{Name: "worker", Project: "ops"},
	}

	assert.Equal(t, "ops", projectForService(services, "worker"))
	assert.Equal(t, "platform", projectForService(services, "missing"))
}

// newProjectsAPIStub returns an APIClient backed by an httptest server that
// serves GET /v1/projects with the given body. calls counts requests so tests
// can assert the API was (not) consulted.
func newProjectsAPIStub(t *testing.T, body string, calls *int) *client.APIClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls != nil {
			*calls++
		}
		if r.URL.Path != "/v1/projects" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return client.NewAPIClient(srv.URL, "test-token")
}

func TestResolveProjectSlug_FlagWins(t *testing.T) {
	t.Chdir(t.TempDir())
	calls := 0
	api := newProjectsAPIStub(t, `{"projects":[]}`, &calls)
	cfg := &config.Config{Project: "default"}

	slug, err := resolveProjectSlug(context.Background(), api, cfg, "storefront")

	require.NoError(t, err)
	assert.Equal(t, "storefront", slug)
	assert.Zero(t, calls, "flag resolution must not hit the API")
}

func TestResolveProjectSlug_ConfigBeatsLocalSpec(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	requireWriteFile(t, filepath.Join(dir, ".enclii.yml"), `kind: Service
metadata:
  name: api
  project: from-spec
`)
	calls := 0
	api := newProjectsAPIStub(t, `{"projects":[]}`, &calls)
	cfg := &config.Config{Project: "from-config"}

	slug, err := resolveProjectSlug(context.Background(), api, cfg, "")

	require.NoError(t, err)
	assert.Equal(t, "from-config", slug)
	assert.Zero(t, calls)
}

func TestResolveProjectSlug_DefaultPlaceholderFallsThroughToLocalSpec(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	requireWriteFile(t, filepath.Join(dir, ".enclii.yml"), `kind: Service
metadata:
  name: api
  project: platform
`)
	calls := 0
	api := newProjectsAPIStub(t, `{"projects":[]}`, &calls)
	// config.Load() injects the literal "default" via viper — it is a
	// placeholder, not a real project slug, and must not reach the API.
	cfg := &config.Config{Project: "default"}

	slug, err := resolveProjectSlug(context.Background(), api, cfg, "")

	require.NoError(t, err)
	assert.Equal(t, "platform", slug)
	assert.Zero(t, calls)
}

func TestResolveProjectSlug_SingleProjectFromAPI(t *testing.T) {
	t.Chdir(t.TempDir())
	api := newProjectsAPIStub(t, `{"projects":[{"id":"5f0e8bcd-77a8-4f0c-9c3f-111111111111","name":"Dhanam","slug":"dhanam"}]}`, nil)
	cfg := &config.Config{Project: "default"}

	slug, err := resolveProjectSlug(context.Background(), api, cfg, "")

	require.NoError(t, err)
	assert.Equal(t, "dhanam", slug)
}

func TestResolveProjectSlug_MultipleProjectsErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	api := newProjectsAPIStub(t, `{"projects":[
		{"id":"5f0e8bcd-77a8-4f0c-9c3f-111111111111","name":"A","slug":"alpha"},
		{"id":"5f0e8bcd-77a8-4f0c-9c3f-222222222222","name":"B","slug":"beta"}
	]}`, nil)
	cfg := &config.Config{Project: ""}

	_, err := resolveProjectSlug(context.Background(), api, cfg, "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "alpha")
	assert.Contains(t, err.Error(), "beta")
	assert.Contains(t, err.Error(), "--project")
}

func TestResolveProjectSlug_NoProjectsErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	api := newProjectsAPIStub(t, `{"projects":[]}`, nil)
	cfg := &config.Config{Project: "default"}

	_, err := resolveProjectSlug(context.Background(), api, cfg, "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no projects found")
}

func requireWriteFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}
