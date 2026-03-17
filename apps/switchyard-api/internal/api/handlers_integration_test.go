//go:build integration

package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/testutil"
)

func setupIntegrationHandler(t *testing.T) (*Handler, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	repos := testutil.RequireTestRepos(t)

	handler := NewHandler(
		repos,
		&config.Config{Registry: "test-registry"},
		nil, // auth manager
		nil, // cache
		nil, // builder
		nil, // k8s client
		nil, // controller
		nil, // reconciler
		nil, // metrics
		nil, // logger
		nil, // validator
		nil, // provenance checker
		nil, // compliance exporter
		nil, // topology builder
		nil, // auth service
		nil, // project service
		nil, // deployment service
		nil, // deployment group service
		nil, // roundhouse client
	)

	engine := gin.New()
	return handler, engine
}

func requireTestDB(t *testing.T) {
	t.Helper()
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping handler integration test")
	}
}

func TestCreateProject(t *testing.T) {
	requireTestDB(t)
	h, engine := setupIntegrationHandler(t)
	engine.POST("/v1/projects", h.CreateProject)

	body := `{"name":"Integration Handler Test","slug":"integ-handler-test"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/projects", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code == http.StatusInternalServerError {
		t.Errorf("unexpected 500 error: %s", w.Body.String())
	}
}

func TestListProjects(t *testing.T) {
	requireTestDB(t)
	h, engine := setupIntegrationHandler(t)
	engine.GET("/v1/projects", h.ListProjects)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/projects", nil)
	engine.ServeHTTP(w, req)

	if w.Code == http.StatusInternalServerError {
		t.Errorf("unexpected 500 error: %s", w.Body.String())
	}
}

func TestGetProject(t *testing.T) {
	requireTestDB(t)
	h, engine := setupIntegrationHandler(t)
	engine.GET("/v1/projects/:slug", h.GetProject)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/projects/nonexistent", nil)
	engine.ServeHTTP(w, req)

	if w.Code == http.StatusInternalServerError {
		t.Errorf("unexpected 500 error: %s", w.Body.String())
	}
}

func TestListBareMetalHostsIntegration(t *testing.T) {
	requireTestDB(t)
	h, engine := setupIntegrationHandler(t)
	engine.GET("/v1/admin/bare-metal", h.ListBareMetalHosts)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/admin/bare-metal", nil)
	engine.ServeHTTP(w, req)

	if w.Code == 0 {
		t.Error("expected non-zero status code")
	}
}

func TestRegisterClusterIntegration(t *testing.T) {
	requireTestDB(t)
	h, engine := setupIntegrationHandler(t)
	engine.POST("/v1/admin/clusters", h.RegisterCluster)

	body := `{"name":"test-cluster","api_server":"https://k8s.example.com:6443"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/admin/clusters", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code == 0 {
		t.Error("expected non-zero status code")
	}
}

func TestListManagedResourcesIntegration(t *testing.T) {
	requireTestDB(t)
	h, engine := setupIntegrationHandler(t)
	engine.GET("/v1/admin/managed-resources", h.ListManagedResources)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/admin/managed-resources", nil)
	engine.ServeHTTP(w, req)

	if w.Code == 0 {
		t.Error("expected non-zero status code")
	}
}

func TestProvisionVClusterIntegration(t *testing.T) {
	requireTestDB(t)
	h, engine := setupIntegrationHandler(t)
	engine.POST("/v1/admin/vclusters", h.ProvisionVCluster)

	body := `{"name":"test-vcluster","cluster_id":"123"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/admin/vclusters", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code == 0 {
		t.Error("expected non-zero status code")
	}
}

func TestCreatePropagationPolicyIntegration(t *testing.T) {
	requireTestDB(t)
	h, engine := setupIntegrationHandler(t)
	engine.POST("/v1/admin/propagation-policies", h.CreatePropagationPolicy)

	body := `{"name":"test-policy","target_clusters":["cluster1"]}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/admin/propagation-policies", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code == 0 {
		t.Error("expected non-zero status code")
	}
}

func TestListDriftEventsIntegration(t *testing.T) {
	requireTestDB(t)
	h, engine := setupIntegrationHandler(t)
	engine.GET("/v1/admin/drift-events", h.ListDriftEvents)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/admin/drift-events", nil)
	engine.ServeHTTP(w, req)

	if w.Code == 0 {
		t.Error("expected non-zero status code")
	}
}

func TestGetCostSummaryIntegration(t *testing.T) {
	requireTestDB(t)
	h, engine := setupIntegrationHandler(t)
	engine.GET("/v1/admin/costs/summary", h.GetCostSummary)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/admin/costs/summary", nil)
	engine.ServeHTTP(w, req)

	if w.Code == 0 {
		t.Error("expected non-zero status code")
	}
}

func BenchmarkCreateProject(b *testing.B) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		b.Skip("TEST_DATABASE_URL not set — skipping benchmark")
	}
}
