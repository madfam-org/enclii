package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ==================== Functions API Tests ====================

func TestAPIClient_ListFunctions(t *testing.T) {
	fn1ID := uuid.New()
	fn2ID := uuid.New()
	projectID := uuid.New()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/v1/projects/my-project/functions", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		response := struct {
			Functions []*types.Function `json:"functions"`
		}{
			Functions: []*types.Function{
				{
					ID:        fn1ID,
					ProjectID: projectID,
					Name:      "hello-world",
					Status:    types.FunctionStatusReady,
					Config: types.FunctionConfig{
						Runtime: types.FunctionRuntimeGo,
						Handler: "main.Handler",
					},
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				},
				{
					ID:        fn2ID,
					ProjectID: projectID,
					Name:      "process-data",
					Status:    types.FunctionStatusBuilding,
					Config: types.FunctionConfig{
						Runtime: types.FunctionRuntimePython,
						Handler: "handler.main",
					},
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	functions, err := client.ListFunctions(ctx, "my-project")

	require.NoError(t, err)
	assert.Len(t, functions, 2)
	assert.Equal(t, fn1ID, functions[0].ID)
	assert.Equal(t, "hello-world", functions[0].Name)
	assert.Equal(t, types.FunctionStatusReady, functions[0].Status)
	assert.Equal(t, fn2ID, functions[1].ID)
	assert.Equal(t, "process-data", functions[1].Name)
}

func TestAPIClient_ListFunctions_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "internal server error",
		})
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	functions, err := client.ListFunctions(ctx, "bad-project")

	require.Error(t, err)
	assert.Nil(t, functions)
	assert.Contains(t, err.Error(), "failed to list functions")
}

func TestAPIClient_ListAllFunctions(t *testing.T) {
	fnID := uuid.New()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/v1/functions", r.URL.Path)

		response := struct {
			Functions []*types.Function `json:"functions"`
		}{
			Functions: []*types.Function{
				{
					ID:     fnID,
					Name:   "global-fn",
					Status: types.FunctionStatusReady,
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	functions, err := client.ListAllFunctions(ctx)

	require.NoError(t, err)
	assert.Len(t, functions, 1)
	assert.Equal(t, "global-fn", functions[0].Name)
}

func TestAPIClient_GetFunction(t *testing.T) {
	fnID := uuid.New()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/v1/functions/"+fnID.String(), r.URL.Path)

		fn := types.Function{
			ID:     fnID,
			Name:   "my-function",
			Status: types.FunctionStatusReady,
			Config: types.FunctionConfig{
				Runtime:     types.FunctionRuntimeNode,
				Handler:     "handler.main",
				Memory:      "256Mi",
				CPU:         "200m",
				Timeout:     30,
				MinReplicas: 0,
				MaxReplicas: 5,
			},
			Endpoint:  "https://fn.enclii.dev/my-function",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(fn)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	fn, err := client.GetFunction(ctx, fnID.String())

	require.NoError(t, err)
	assert.Equal(t, fnID, fn.ID)
	assert.Equal(t, "my-function", fn.Name)
	assert.Equal(t, types.FunctionRuntimeNode, fn.Config.Runtime)
	assert.Equal(t, "256Mi", fn.Config.Memory)
	assert.Equal(t, 5, fn.Config.MaxReplicas)
}

func TestAPIClient_GetFunction_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "function not found",
		})
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	fn, err := client.GetFunction(ctx, "nonexistent-id")

	require.Error(t, err)
	assert.Nil(t, fn)
	assert.Contains(t, err.Error(), "failed to get function")
}

func TestAPIClient_CreateFunction(t *testing.T) {
	fnID := uuid.New()
	projectID := uuid.New()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/projects/my-project/functions", r.URL.Path)

		var req map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, "new-function", req["name"])

		fn := types.Function{
			ID:        fnID,
			ProjectID: projectID,
			Name:      "new-function",
			Status:    types.FunctionStatusPending,
			Config: types.FunctionConfig{
				Runtime: types.FunctionRuntimeGo,
				Handler: "main.Handler",
				Memory:  "128Mi",
			},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(fn)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	fn, err := client.CreateFunction(ctx, "my-project", "new-function", types.FunctionConfig{
		Runtime: types.FunctionRuntimeGo,
		Handler: "main.Handler",
		Memory:  "128Mi",
	})

	require.NoError(t, err)
	assert.Equal(t, fnID, fn.ID)
	assert.Equal(t, "new-function", fn.Name)
	assert.Equal(t, types.FunctionStatusPending, fn.Status)
}

func TestAPIClient_DeleteFunction(t *testing.T) {
	fnID := uuid.New()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/v1/functions/"+fnID.String(), r.URL.Path)

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	err := client.DeleteFunction(ctx, fnID.String())

	require.NoError(t, err)
}

func TestAPIClient_DeleteFunction_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"permission denied"}`))
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	err := client.DeleteFunction(ctx, "some-id")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete function")
}

func TestAPIClient_InvokeFunction_JSONData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/functions/my-fn/invoke", r.URL.Path)

		result := FunctionInvokeResult{
			StatusCode: 200,
			Body:       `{"result": "success"}`,
			ColdStart:  true,
			Duration:   "120ms",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	result, err := client.InvokeFunction(ctx, "my-fn", `{"key": "value"}`)

	require.NoError(t, err)
	assert.Equal(t, 200, result.StatusCode)
	assert.Equal(t, `{"result": "success"}`, result.Body)
	assert.True(t, result.ColdStart)
	assert.Equal(t, "120ms", result.Duration)
}

func TestAPIClient_InvokeFunction_RawString(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		// When data is not valid JSON, it should be wrapped in {"body": data}
		assert.Equal(t, "hello world", req["body"])

		result := FunctionInvokeResult{
			StatusCode: 200,
			Body:       "processed",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	result, err := client.InvokeFunction(ctx, "my-fn", "hello world")

	require.NoError(t, err)
	assert.Equal(t, 200, result.StatusCode)
}

func TestAPIClient_InvokeFunction_EmptyData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result := FunctionInvokeResult{
			StatusCode: 200,
			Body:       "ok",
			ColdStart:  false,
			Duration:   "5ms",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	result, err := client.InvokeFunction(ctx, "my-fn", "")

	require.NoError(t, err)
	assert.Equal(t, 200, result.StatusCode)
	assert.False(t, result.ColdStart)
}

func TestAPIClient_GetFunctionLogs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/v1/functions/my-fn/logs", r.URL.Path)
		assert.Equal(t, "50", r.URL.Query().Get("lines"))

		response := struct {
			Logs []string `json:"logs"`
		}{
			Logs: []string{
				"2025-01-15T10:00:00Z [INFO] Function started",
				"2025-01-15T10:00:01Z [INFO] Processing request",
				"2025-01-15T10:00:01Z [INFO] Response sent (200)",
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	logs, err := client.GetFunctionLogs(ctx, "my-fn", 50)

	require.NoError(t, err)
	assert.Len(t, logs, 3)
	assert.Contains(t, logs[0], "Function started")
	assert.Contains(t, logs[2], "Response sent")
}

func TestAPIClient_GetFunctionLogs_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "function not found",
		})
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	logs, err := client.GetFunctionLogs(ctx, "nonexistent", 10)

	require.Error(t, err)
	assert.Nil(t, logs)
	assert.Contains(t, err.Error(), "failed to get function logs")
}

// ==================== Admin Onboarding API Tests ====================

func TestAPIClient_OnboardProject_WithProvisioning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/admin/onboard", r.URL.Path)
		assert.Equal(t, "Bearer admin-token", r.Header.Get("Authorization"))

		var req types.OnboardingRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, "madfam-org/new-service", req.RepoFullName)
		assert.Equal(t, "new-service", req.ProjectName)
		assert.Equal(t, "new-service-ns", req.Namespace)
		assert.NotNil(t, req.ProvisionPostgres)
		assert.Equal(t, "new_service_db", req.ProvisionPostgres.DatabaseName)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":         "completed",
			"repo":           req.RepoFullName,
			"project":        req.ProjectName,
			"namespace":      req.Namespace,
			"postgres_ready": true,
		})
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "admin-token")
	ctx := context.Background()

	branch := "main"
	var result map[string]interface{}
	err := client.OnboardProject(ctx, &types.OnboardingRequest{
		RepoFullName: "madfam-org/new-service",
		ProjectName:  "new-service",
		Namespace:    "new-service-ns",
		Branch:       &branch,
		ProvisionPostgres: &types.PostgresProvisionSpec{
			DatabaseName: "new_service_db",
			RoleName:     "new_service_role",
			RolePassword: "secure-password",
			Extensions:   []string{"uuid-ossp", "pgcrypto"},
		},
	}, &result)

	require.NoError(t, err)
	assert.Equal(t, "completed", result["status"])
	assert.Equal(t, true, result["postgres_ready"])
}

func TestAPIClient_OnboardProject_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "admin access required",
		})
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "viewer-token")
	ctx := context.Background()

	var result map[string]interface{}
	err := client.OnboardProject(ctx, &types.OnboardingRequest{
		RepoFullName: "madfam-org/test",
		ProjectName:  "test",
	}, &result)

	require.Error(t, err)
	var apiErr APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
}

func TestAPIClient_EnsureOnboarding_RepairMode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/admin/onboard/ensure", r.URL.Path)

		var req types.OnboardingRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, "madfam-org/coupler", req.RepoFullName)
		assert.Equal(t, "coupler", req.ProjectName)
		assert.Equal(t, "coupler", req.Namespace)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "completed",
			"mode":      "repair",
			"namespace": req.Namespace,
			"step_results": []map[string]string{
				{"name": "registry_credentials", "status": "ok"},
			},
		})
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "admin-token")
	ctx := context.Background()

	var result map[string]interface{}
	err := client.EnsureOnboarding(ctx, &types.OnboardingRequest{
		RepoFullName: "madfam-org/coupler",
		ProjectName:  "coupler",
		Namespace:    "coupler",
	}, &result)

	require.NoError(t, err)
	assert.Equal(t, "repair", result["mode"])
	assert.Equal(t, "completed", result["status"])
}

func TestAPIClient_PreflightOnboard_AllPassing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/admin/onboard/preflight", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(types.PreflightResult{
			Pass:       true,
			Violations: nil,
		})
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	var result types.PreflightResult
	err := client.PreflightOnboard(ctx, &types.OnboardingRequest{
		RepoFullName: "madfam-org/clean-repo",
		ProjectName:  "clean-repo",
	}, &result)

	require.NoError(t, err)
	assert.True(t, result.Pass)
	assert.Empty(t, result.Violations)
}

func TestAPIClient_PreflightOnboard_MultipleViolations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(types.PreflightResult{
			Pass: false,
			Violations: []types.PreflightIssue{
				{File: "deployment.yaml", Kind: "Deployment", Name: "api", Message: "image not fully qualified"},
				{File: "service.yaml", Kind: "Service", Name: "api", Message: "missing port name"},
				{File: "deployment.yaml", Kind: "Deployment", Name: "api", Message: "no resource limits set"},
			},
		})
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	var result types.PreflightResult
	err := client.PreflightOnboard(ctx, &types.OnboardingRequest{
		RepoFullName: "madfam-org/bad-manifests",
		ProjectName:  "bad-manifests",
	}, &result)

	require.NoError(t, err)
	assert.False(t, result.Pass)
	assert.Len(t, result.Violations, 3)
	assert.Equal(t, "deployment.yaml", result.Violations[0].File)
	assert.Equal(t, "service.yaml", result.Violations[1].File)
	assert.Contains(t, result.Violations[2].Message, "resource limits")
}
