package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	k8sclient "github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
)

func TestOperatorCapabilitiesIncludeCoreSurfaces(t *testing.T) {
	require.True(t, operationSupported("apps", "status", opsCapabilities))
	require.True(t, operationSupported("storage", "repair-plan", opsCapabilities))
	require.True(t, operationSupported("github", "runs", providerCapabilities))
	require.True(t, operationSupported("hetzner", "vswitch", providerCapabilities))
	require.False(t, operationSupported("porkbun", "charge-card", providerCapabilities))
}

func TestOperatorCapabilitiesAdvertiseReadAdapterStatus(t *testing.T) {
	for _, capability := range opsCapabilities {
		assert.Equal(t, "partial", capability.Status, "ops %s should expose read adapters plus apply contracts", capability.Name)
	}
	assertCapabilityStatus(t, providerCapabilities, "github", "partial")
	assertCapabilityStatus(t, providerCapabilities, "cloudflare", "partial")
	assertCapabilityStatus(t, providerCapabilities, "porkbun", "contract")
	assertCapabilityStatus(t, providerCapabilities, "hetzner", "contract")
}

func TestHandleOpsOperationDryRunReturnsPlan(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &Handler{}
	router := gin.New()
	router.POST("/v1/ops/:domain/:action", handler.HandleOpsOperation)

	body, err := json.Marshal(operatorOperationRequest{
		Operation: "ops.apps.sync",
		DryRun:    true,
		Args:      map[string]string{"target": "monitoring"},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/ops/apps/sync", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp operatorOperationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "ops.apps.sync", resp.Operation)
	assert.Equal(t, "planned", resp.Status)
	assert.True(t, resp.DryRun)
	assert.NotEmpty(t, resp.Steps)
}

func TestHandleOpsApplyBlocksUntilAdapterIsWired(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &Handler{}
	router := gin.New()
	router.POST("/v1/ops/:domain/:action", handler.HandleOpsOperation)

	body, err := json.Marshal(operatorOperationRequest{
		Operation: "ops.apps.sync",
		DryRun:    false,
		Reason:    "recover drift after reviewed manifest update",
		Args:      map[string]string{"target": "monitoring"},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/ops/apps/sync", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotImplemented, rec.Code)
	var resp operatorOperationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "adapter_required", resp.Status)
	assert.False(t, resp.DryRun)
}

func TestHandleOpsAppsSyncApplyUsesDynamicAdapter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "argoproj.io/v1alpha1",
			"kind":       "Application",
			"metadata": map[string]any{
				"name":      "monitoring",
				"namespace": "argocd",
			},
			"status": map[string]any{
				"sync":   map[string]any{"status": "OutOfSync", "revision": "abc123"},
				"health": map[string]any{"status": "Healthy"},
			},
		},
	}
	dynClient := fake.NewSimpleDynamicClient(runtime.NewScheme(), app)
	handler := &Handler{k8sClient: &k8sclient.Client{DynamicClient: dynClient}}
	router := gin.New()
	router.POST("/v1/ops/:domain/:action", handler.HandleOpsOperation)

	body, err := json.Marshal(operatorOperationRequest{
		Operation:      "ops.apps.sync",
		DryRun:         false,
		Reason:         "recover reviewed GitOps drift",
		IdempotencyKey: "sync-monitoring-1",
		Scope:          map[string]string{"namespace": "argocd"},
		Args:           map[string]string{"target": "monitoring"},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/ops/apps/sync", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
	var resp operatorOperationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "submitted", resp.Status)
	assert.False(t, resp.DryRun)

	updated, err := dynClient.Resource(argoApplicationGVR).Namespace("argocd").Get(context.Background(), "monitoring", metav1.GetOptions{})
	require.NoError(t, err)
	operation, found, err := unstructured.NestedMap(updated.Object, "operation")
	require.NoError(t, err)
	require.True(t, found)
	syncSpec, ok := operation["sync"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, syncSpec["prune"])

	annotations := updated.GetAnnotations()
	assert.Equal(t, "ops.apps.sync", annotations["enclii.dev/last-ops-operation"])
	assert.Equal(t, "sync-monitoring-1", annotations["enclii.dev/last-ops-idempotency-key"])
	assert.Equal(t, "recover reviewed GitOps drift", annotations["enclii.dev/last-ops-reason"])
}

func TestHandleOpsAppsStatusUsesDynamicAdapter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "argoproj.io/v1alpha1",
			"kind":       "Application",
			"metadata": map[string]any{
				"name":      "monitoring",
				"namespace": "argocd",
			},
			"status": map[string]any{
				"sync":   map[string]any{"status": "Synced"},
				"health": map[string]any{"status": "Healthy"},
			},
		},
	}
	handler := &Handler{k8sClient: &k8sclient.Client{DynamicClient: fake.NewSimpleDynamicClient(runtime.NewScheme(), app)}}
	router := gin.New()
	router.POST("/v1/ops/:domain/:action", handler.HandleOpsOperation)

	body, err := json.Marshal(operatorOperationRequest{
		Operation: "ops.apps.status",
		DryRun:    true,
		Scope:     map[string]string{"namespace": "argocd"},
		Args:      map[string]string{"target": "monitoring"},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/ops/apps/status", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp operatorOperationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "succeeded", resp.Status)
	assert.Equal(t, "ops.apps.status", resp.Operation)
	assert.NotNil(t, resp.Data)
	data, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(1), data["count"])
}

func TestHandleOpsAppsDiffUsesArgoDriftAdapter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "argoproj.io/v1alpha1",
			"kind":       "Application",
			"metadata": map[string]any{
				"name":      "monitoring",
				"namespace": "argocd",
			},
			"status": map[string]any{
				"sync":   map[string]any{"status": "OutOfSync", "revision": "abc123"},
				"health": map[string]any{"status": "Degraded"},
				"resources": []any{
					map[string]any{
						"group":     "apps",
						"kind":      "Deployment",
						"namespace": "enclii",
						"name":      "switchyard-api",
						"status":    "OutOfSync",
						"health":    map[string]any{"status": "Degraded"},
					},
					map[string]any{
						"kind":      "Service",
						"namespace": "enclii",
						"name":      "switchyard-api",
						"status":    "Synced",
					},
				},
				"conditions": []any{
					map[string]any{"type": "ComparisonError", "message": "live state differs"},
				},
			},
		},
	}
	handler := &Handler{k8sClient: &k8sclient.Client{DynamicClient: fake.NewSimpleDynamicClient(runtime.NewScheme(), app)}}
	router := gin.New()
	router.POST("/v1/ops/:domain/:action", handler.HandleOpsOperation)

	body, err := json.Marshal(operatorOperationRequest{
		Operation: "ops.apps.diff",
		DryRun:    true,
		Scope:     map[string]string{"namespace": "argocd"},
		Args:      map[string]string{"target": "monitoring"},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/ops/apps/diff", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp operatorOperationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "succeeded", resp.Status)
	assert.Equal(t, "ops.apps.diff", resp.Operation)

	data, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(1), data["count"])
	assert.Equal(t, float64(1), data["driftedCount"])
	assert.Equal(t, float64(1), data["driftedResources"])

	applications, ok := data["applications"].([]any)
	require.True(t, ok)
	require.Len(t, applications, 1)
	summary, ok := applications[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "OutOfSync", summary["syncStatus"])
	assert.Equal(t, "Degraded", summary["healthStatus"])
	assert.Equal(t, true, summary["drifted"])
	assert.Equal(t, float64(1), summary["driftedResources"])

	resources, ok := summary["resources"].([]any)
	require.True(t, ok)
	require.Len(t, resources, 2)
	driftedResource, ok := resources[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Deployment", driftedResource["kind"])
	assert.Equal(t, true, driftedResource["drifted"])

	conditions, ok := summary["conditions"].([]any)
	require.True(t, ok)
	require.Len(t, conditions, 1)
	condition, ok := conditions[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "ComparisonError", condition["type"])
}

func TestHandleOpsAppsDiffReportsUnavailableWithoutDynamicClient(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &Handler{k8sClient: &k8sclient.Client{}}
	router := gin.New()
	router.POST("/v1/ops/:domain/:action", handler.HandleOpsOperation)

	body, err := json.Marshal(operatorOperationRequest{
		Operation: "ops.apps.diff",
		DryRun:    true,
		Scope:     map[string]string{"namespace": "argocd"},
		Args:      map[string]string{"target": "monitoring"},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/ops/apps/diff", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp operatorOperationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "adapter_unconfigured", resp.Status)
	require.NotEmpty(t, resp.Warnings)
	assert.Contains(t, resp.Warnings[0], "dynamic client")
}

func TestHandleProviderOperationApplyRequiresReason(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &Handler{}
	router := gin.New()
	router.POST("/v1/providers/:provider/:action", handler.HandleProviderOperation)

	body, err := json.Marshal(operatorOperationRequest{
		Operation: "providers.github.rerun",
		DryRun:    false,
		Args:      map[string]string{"target": "25430873929"},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/providers/github/rerun", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "reason is required")
}

func TestHandleProviderGitHubReadReportsUnconfiguredAdapter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &Handler{}
	router := gin.New()
	router.POST("/v1/providers/:provider/:action", handler.HandleProviderOperation)

	body, err := json.Marshal(operatorOperationRequest{
		Operation: "providers.github.runs",
		DryRun:    true,
		Args:      map[string]string{"target": "madfam-org/enclii"},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/providers/github/runs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp operatorOperationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "adapter_unconfigured", resp.Status)
	assert.Contains(t, resp.Warnings[0], "github token")
}

func TestHandleProviderGitHubPackagesUsesRESTAdapter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldBaseURL := githubReadAPIBaseURL
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer gh-test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "application/vnd.github+json", r.Header.Get("Accept"))
		switch r.URL.Path {
		case "/orgs/madfam-org/packages/container/switchyard-api", "/users/madfam-org/packages/container/switchyard-api":
			http.NotFound(w, r)
		case "/orgs/madfam-org/packages/container/enclii":
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"id":           42,
				"name":         "enclii",
				"package_type": "container",
				"visibility":   "public",
				"html_url":     "https://github.com/orgs/madfam-org/packages/container/package/enclii",
				"created_at":   "2026-05-01T00:00:00Z",
				"updated_at":   "2026-05-02T00:00:00Z",
				"owner":        map[string]any{"login": "madfam-org", "type": "Organization"},
				"repository":   map[string]any{"full_name": "madfam-org/enclii", "visibility": "public", "private": false},
			}))
		case "/orgs/madfam-org/packages/container/enclii/versions":
			assert.Equal(t, "20", r.URL.Query().Get("per_page"))
			require.NoError(t, json.NewEncoder(w).Encode([]map[string]any{
				{
					"id":         101,
					"name":       "sha256:abc123",
					"created_at": "2026-05-02T00:00:00Z",
					"updated_at": "2026-05-03T00:00:00Z",
					"metadata":   map[string]any{"container": map[string]any{"tags": []any{"latest", "sha-abc123"}}},
				},
			}))
		default:
			t.Errorf("unexpected github path %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	githubReadAPIBaseURL = server.URL
	defer func() { githubReadAPIBaseURL = oldBaseURL }()

	handler := &Handler{config: &config.Config{GitHubToken: "gh-test-token"}}
	router := gin.New()
	router.POST("/v1/providers/:provider/:action", handler.HandleProviderOperation)

	body, err := json.Marshal(operatorOperationRequest{
		Operation: "providers.github.packages",
		DryRun:    true,
		Args:      map[string]string{"target": "madfam-org/enclii", "package": "switchyard-api"},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/providers/github/packages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp operatorOperationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "succeeded", resp.Status)
	assert.Equal(t, "providers.github.packages", resp.Operation)

	data, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "madfam-org/enclii", data["repository"])
	assert.Equal(t, "ghcr.io", data["registry"])
	assert.Equal(t, float64(1), data["count"])
	assert.Equal(t, float64(1), data["missingCount"])

	candidates, ok := data["candidates"].([]any)
	require.True(t, ok)
	require.Len(t, candidates, 2)
	assert.Equal(t, "switchyard-api", candidates[0])
	assert.Equal(t, "enclii", candidates[1])

	packages, ok := data["packages"].([]any)
	require.True(t, ok)
	require.Len(t, packages, 1)
	pkg, ok := packages[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "enclii", pkg["candidate"])
	assert.Equal(t, "org", pkg["scope"])
	assert.Equal(t, float64(1), pkg["versionCount"])

	metadata, ok := pkg["metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "public", metadata["visibility"])
	assert.Equal(t, "enclii", metadata["name"])

	versions, ok := pkg["versions"].([]any)
	require.True(t, ok)
	require.Len(t, versions, 1)
	version, ok := versions[0].(map[string]any)
	require.True(t, ok)
	tags, ok := version["tags"].([]any)
	require.True(t, ok)
	assert.ElementsMatch(t, []any{"latest", "sha-abc123"}, tags)
}

func assertCapabilityStatus(t *testing.T, capabilities []operatorCapability, name, status string) {
	t.Helper()
	for _, capability := range capabilities {
		if capability.Name == name {
			assert.Equal(t, status, capability.Status)
			return
		}
	}
	t.Fatalf("capability %s not found", name)
}
