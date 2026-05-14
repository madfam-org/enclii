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
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestOperatorCapabilitiesIncludeCoreSurfaces(t *testing.T) {
	require.True(t, operationSupported("apps", "status", opsCapabilities))
	require.True(t, operationSupported("apps", "retire", opsCapabilities))
	require.True(t, operationSupported("jobs", "trigger", opsCapabilities))
	require.True(t, operationSupported("storage", "repair-plan", opsCapabilities))
	require.True(t, operationSupported("quote-flow", "verify", opsCapabilities))
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
	assert.Equal(t, []any{"PruneLast=true"}, syncSpec["syncOptions"])

	annotations := updated.GetAnnotations()
	assert.Equal(t, "ops.apps.sync", annotations["enclii.dev/last-ops-operation"])
	assert.Equal(t, "sync-monitoring-1", annotations["enclii.dev/last-ops-idempotency-key"])
	assert.Equal(t, "recover reviewed GitOps drift", annotations["enclii.dev/last-ops-reason"])
}

func TestHandleOpsAppsRetireApplyDeletesApplicationWithOrphanPropagation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "argoproj.io/v1alpha1",
			"kind":       "Application",
			"metadata": map[string]any{
				"name":      "legacy-crm",
				"namespace": "argocd",
			},
			"status": map[string]any{
				"sync":   map[string]any{"status": "OutOfSync", "revision": "abc123"},
				"health": map[string]any{"status": "Degraded"},
			},
		},
	}
	dynClient := fake.NewSimpleDynamicClient(runtime.NewScheme(), app)
	handler := &Handler{k8sClient: &k8sclient.Client{DynamicClient: dynClient}}
	router := gin.New()
	router.POST("/v1/ops/:domain/:action", handler.HandleOpsOperation)

	body, err := json.Marshal(operatorOperationRequest{
		Operation:      "ops.apps.retire",
		DryRun:         false,
		Reason:         "retire reviewed legacy Argo application after successor onboarding",
		IdempotencyKey: "retire-legacy-crm-1",
		Scope:          map[string]string{"namespace": "argocd"},
		Args:           map[string]string{"target": "legacy-crm"},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/ops/apps/retire", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
	var resp operatorOperationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "submitted", resp.Status)
	assert.Equal(t, "ops.apps.retire", resp.Operation)
	assert.Equal(t, "Orphan", resp.Data.(map[string]any)["propagation"])

	_, err = dynClient.Resource(argoApplicationGVR).Namespace("argocd").Get(context.Background(), "legacy-crm", metav1.GetOptions{})
	require.True(t, k8serrors.IsNotFound(err))
}

func TestHandleOpsJobsTriggerApplyUsesCronJobTemplate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	suspend := true
	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "forgesight-mexico-wave-seed",
			Namespace: "forgesight",
		},
		Spec: batchv1.CronJobSpec{
			Schedule: "15 1 * * *",
			Suspend:  &suspend,
			JobTemplate: batchv1.JobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "forgesight-mexico-wave-seed"},
				},
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							RestartPolicy: corev1.RestartPolicyNever,
							Containers: []corev1.Container{
								{
									Name:    "seed",
									Image:   "ghcr.io/madfam-org/forgesight/pipeline:latest",
									Command: []string{"python", "scripts/run_mexico_wave.py"},
									Args:    []string{"--seed-only"},
									Env: []corev1.EnvVar{
										{Name: "ENVIRONMENT", Value: "production"},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	clientset := k8sfake.NewSimpleClientset(cronJob)
	handler := &Handler{k8sClient: &k8sclient.Client{KubeClient: clientset}}
	router := gin.New()
	router.POST("/v1/ops/:domain/:action", handler.HandleOpsOperation)

	body, err := json.Marshal(operatorOperationRequest{
		Operation: "ops.jobs.trigger",
		DryRun:    false,
		Reason:    "populate verified ForgeSight market data",
		Scope:     map[string]string{"namespace": "forgesight"},
		Args:      map[string]string{"target": "forgesight-mexico-wave-seed"},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/ops/jobs/trigger", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
	var resp operatorOperationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "submitted", resp.Status)
	assert.False(t, resp.DryRun)
	assert.NotEmpty(t, resp.Warnings)

	jobs, err := clientset.BatchV1().Jobs("forgesight").List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, jobs.Items, 1)
	job := jobs.Items[0]
	assert.Equal(t, "forgesight-mexico-wave-seed", job.Labels["enclii.dev/source-cronjob"])
	assert.Equal(t, "populate verified ForgeSight market data", job.Annotations["enclii.dev/last-ops-reason"])
	require.Len(t, job.Spec.Template.Spec.Containers, 1)
	container := job.Spec.Template.Spec.Containers[0]
	assert.Equal(t, "ghcr.io/madfam-org/forgesight/pipeline:latest", container.Image)
	assert.Equal(t, []string{"python", "scripts/run_mexico_wave.py"}, container.Command)
	assert.Equal(t, []string{"--seed-only"}, container.Args)
	assert.Equal(t, "production", container.Env[0].Value)
}

func TestOperatorLogInt64ArgBoundsAndValidation(t *testing.T) {
	value, err := operatorLogInt64Arg(map[string]string{"limitBytes": "999999999"}, "limitBytes", 262144, 1048576)
	require.NoError(t, err)
	assert.Equal(t, int64(1048576), value)

	value, err = operatorLogInt64Arg(map[string]string{}, "tailLines", 400, 5000)
	require.NoError(t, err)
	assert.Equal(t, int64(400), value)

	_, err = operatorLogInt64Arg(map[string]string{"tailLines": "-1"}, "tailLines", 400, 5000)
	require.Error(t, err)
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

func TestHandleOpsQuoteFlowVerifyReportsAuthAndMarketBlockers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	readyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"status": "ok"}))
	}))
	defer readyServer.Close()
	pricingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"status": "ok"}))
	}))
	defer pricingServer.Close()

	handler := &Handler{config: &config.Config{}}
	router := gin.New()
	router.POST("/v1/ops/:domain/:action", handler.HandleOpsOperation)

	body, err := json.Marshal(operatorOperationRequest{
		Operation: "ops.quote-flow.verify",
		DryRun:    true,
		Scope:     map[string]string{"project": "tablaco"},
		Args: map[string]string{
			"agent":                   "selva",
			"require_market_verified": "true",
			"selva_worker_url":        readyServer.URL + "/health",
			"yantra_project_url":      readyServer.URL + "/projects/tablaco",
			"cotiza_import_url":       readyServer.URL + "/import/health",
			"forgesight_pricing_url":  pricingServer.URL + "/health",
		},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/ops/quote-flow/verify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp operatorOperationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "blocked_by_auth", resp.Status)
	assert.Equal(t, "ops.quote-flow.verify", resp.Operation)

	data, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "tablaco", data["project"])
	assert.Equal(t, "selva", data["agent"])
	checks, ok := data["checks"].([]any)
	require.True(t, ok)
	assertQuoteFlowCheckStatus(t, checks, "selva_worker_auth", "blocked_auth")
	assertQuoteFlowCheckStatus(t, checks, "forgesight_pricing_health", "market_data_unavailable")
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

func assertQuoteFlowCheckStatus(t *testing.T, checks []any, name, status string) {
	t.Helper()
	for _, raw := range checks {
		check, ok := raw.(map[string]any)
		require.True(t, ok)
		if check["name"] == name {
			assert.Equal(t, status, check["status"])
			return
		}
	}
	t.Fatalf("quote-flow check %s not found", name)
}
