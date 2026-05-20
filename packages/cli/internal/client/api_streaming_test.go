package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ==================== buildWSURL Tests ====================

func TestBuildWSURL_HTTPS(t *testing.T) {
	client := NewAPIClient("https://api.enclii.dev", "test-token")

	url, err := client.buildWSURL("svc-123", "production", StreamLogsOptions{})

	require.NoError(t, err)
	assert.Equal(t, "wss://api.enclii.dev/v1/services/svc-123/logs/stream?env=production", url)
}

func TestBuildWSURL_HTTP(t *testing.T) {
	client := NewAPIClient("http://localhost:8080", "test-token")

	url, err := client.buildWSURL("svc-456", "staging", StreamLogsOptions{})

	require.NoError(t, err)
	assert.Equal(t, "ws://localhost:8080/v1/services/svc-456/logs/stream?env=staging", url)
}

func TestBuildWSURL_WithAllOptions(t *testing.T) {
	client := NewAPIClient("https://api.enclii.dev", "test-token")
	since := time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC)

	url, err := client.buildWSURL("svc-789", "dev", StreamLogsOptions{
		Lines:      100,
		Timestamps: true,
		Since:      &since,
	})

	require.NoError(t, err)
	assert.Contains(t, url, "wss://api.enclii.dev/v1/services/svc-789/logs/stream")
	assert.Contains(t, url, "env=dev")
	assert.Contains(t, url, "lines=100")
	assert.Contains(t, url, "timestamps=true")
	assert.Contains(t, url, "since=2025-03-01T12%3A00%3A00Z")
}

func TestBuildWSURL_NoEnvName(t *testing.T) {
	client := NewAPIClient("https://api.enclii.dev", "test-token")

	url, err := client.buildWSURL("svc-123", "", StreamLogsOptions{
		Lines: 50,
	})

	require.NoError(t, err)
	assert.Equal(t, "wss://api.enclii.dev/v1/services/svc-123/logs/stream?lines=50", url)
	assert.NotContains(t, url, "env=")
}

func TestBuildWSURL_NoOptions(t *testing.T) {
	client := NewAPIClient("https://api.enclii.dev", "test-token")

	url, err := client.buildWSURL("svc-123", "", StreamLogsOptions{})

	require.NoError(t, err)
	assert.Equal(t, "wss://api.enclii.dev/v1/services/svc-123/logs/stream", url)
	assert.NotContains(t, url, "?")
}

func TestBuildWSURL_LinesOnly(t *testing.T) {
	client := NewAPIClient("http://localhost:4200", "tok")

	url, err := client.buildWSURL("abc", "", StreamLogsOptions{Lines: 25})

	require.NoError(t, err)
	assert.Equal(t, "ws://localhost:4200/v1/services/abc/logs/stream?lines=25", url)
}

// ==================== Custom Domains API Tests ====================

func TestAPIClient_ListCustomDomains(t *testing.T) {
	domainID := uuid.New()
	serviceID := uuid.New()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/v1/services/"+serviceID.String()+"/domains", r.URL.Path)

		response := struct {
			Domains []CustomDomainResponse `json:"domains"`
		}{
			Domains: []CustomDomainResponse{
				{
					ID:               domainID,
					ServiceID:        serviceID,
					Domain:           "api.example.com",
					Verified:         true,
					TLSEnabled:       true,
					Status:           "active",
					IsPlatformDomain: false,
					CreatedAt:        time.Now(),
					UpdatedAt:        time.Now(),
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	domains, err := client.ListCustomDomains(ctx, serviceID.String())

	require.NoError(t, err)
	assert.Len(t, domains, 1)
	assert.Equal(t, "api.example.com", domains[0].Domain)
	assert.True(t, domains[0].Verified)
	assert.True(t, domains[0].TLSEnabled)
	assert.Equal(t, "active", domains[0].Status)
}

func TestAPIClient_ListCustomDomains_Empty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Domains []CustomDomainResponse `json:"domains"`
		}{Domains: []CustomDomainResponse{}})
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	domains, err := client.ListCustomDomains(ctx, uuid.New().String())

	require.NoError(t, err)
	assert.Empty(t, domains)
}

func TestAPIClient_AddCustomDomain(t *testing.T) {
	domainID := uuid.New()
	serviceID := uuid.New()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.URL.Path, "/domains")

		var req CustomDomainRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, "custom.example.com", req.Domain)
		assert.True(t, req.TLSEnabled)

		domain := CustomDomainResponse{
			ID:         domainID,
			ServiceID:  serviceID,
			Domain:     req.Domain,
			TLSEnabled: req.TLSEnabled,
			Status:     "pending",
			Verified:   false,
			DNSCNAME:   "abc123.cfargotunnel.com",
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(domain)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	domain, err := client.AddCustomDomain(ctx, serviceID.String(), CustomDomainRequest{
		Domain:     "custom.example.com",
		TLSEnabled: true,
	})

	require.NoError(t, err)
	assert.Equal(t, "custom.example.com", domain.Domain)
	assert.Equal(t, "pending", domain.Status)
	assert.False(t, domain.Verified)
	assert.Equal(t, "abc123.cfargotunnel.com", domain.DNSCNAME)
}

func TestAPIClient_AddCustomDomain_ReconciledEnvelope(t *testing.T) {
	domainID := uuid.New()
	serviceID := uuid.New()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/services/"+serviceID.String()+"/domains", r.URL.Path)

		var req CustomDomainRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"message":              "Domain already existed; tunnel route reconciled",
			"reconciled":           true,
			"tunnel_route_added":   true,
			"tunnel_route_matches": true,
			"domain": CustomDomainResponse{
				ID:         domainID,
				ServiceID:  serviceID,
				Domain:     req.Domain,
				TLSEnabled: req.TLSEnabled,
				Status:     "active",
				Verified:   true,
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			},
		})
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	domain, err := client.AddCustomDomain(ctx, serviceID.String(), CustomDomainRequest{
		Domain:     "staging-api.example.com",
		TLSEnabled: true,
	})

	require.NoError(t, err)
	assert.Equal(t, "staging-api.example.com", domain.Domain)
	assert.Equal(t, "active", domain.Status)
	assert.True(t, domain.Verified)
	assert.Equal(t, serviceID, domain.ServiceID)
}

func TestAPIClient_GetCustomDomain(t *testing.T) {
	domainID := uuid.New()
	serviceID := uuid.New()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/v1/services/"+serviceID.String()+"/domains/"+domainID.String(), r.URL.Path)

		domain := CustomDomainResponse{
			ID:        domainID,
			ServiceID: serviceID,
			Domain:    "app.example.com",
			Verified:  true,
			Status:    "active",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(domain)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	domain, err := client.GetCustomDomain(ctx, serviceID.String(), domainID.String())

	require.NoError(t, err)
	assert.Equal(t, "app.example.com", domain.Domain)
	assert.True(t, domain.Verified)
}

func TestAPIClient_DeleteCustomDomain(t *testing.T) {
	serviceID := uuid.New()
	domainID := uuid.New()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/v1/services/"+serviceID.String()+"/domains/"+domainID.String(), r.URL.Path)

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	err := client.DeleteCustomDomain(ctx, serviceID.String(), domainID.String())

	require.NoError(t, err)
}

func TestAPIClient_DeleteCustomDomain_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("domain not found"))
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	err := client.DeleteCustomDomain(ctx, uuid.New().String(), uuid.New().String())

	require.Error(t, err)
	var apiErr APIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
}

func TestAPIClient_VerifyCustomDomain(t *testing.T) {
	serviceID := uuid.New()
	domainID := uuid.New()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.URL.Path, "/verify")

		response := DomainVerifyResponse{
			Message: "Domain verified successfully",
			Domain: &CustomDomainResponse{
				ID:       domainID,
				Domain:   "verified.example.com",
				Verified: true,
				Status:   "active",
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	result, err := client.VerifyCustomDomain(ctx, serviceID.String(), domainID.String())

	require.NoError(t, err)
	assert.Equal(t, "Domain verified successfully", result.Message)
	assert.NotNil(t, result.Domain)
	assert.True(t, result.Domain.Verified)
}

func TestAPIClient_VerifyCustomDomain_PendingVerification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := DomainVerifyResponse{
			Message:           "DNS TXT record not found",
			VerificationValue: "_enclii-verify=abc123def456",
			Error:             "verification pending",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	result, err := client.VerifyCustomDomain(ctx, uuid.New().String(), uuid.New().String())

	require.NoError(t, err)
	assert.Equal(t, "_enclii-verify=abc123def456", result.VerificationValue)
	assert.Equal(t, "verification pending", result.Error)
}

// ==================== Env Vars API Tests ====================

func TestAPIClient_ListEnvVars(t *testing.T) {
	serviceID := uuid.New()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/v1/services/"+serviceID.String()+"/env-vars", r.URL.Path)

		response := struct {
			EnvVars []EnvVarResponse `json:"environment_variables"`
		}{
			EnvVars: []EnvVarResponse{
				{
					ID:        uuid.New(),
					ServiceID: serviceID,
					Key:       "DATABASE_URL",
					Value:     "postgres://...",
					IsSecret:  false,
				},
				{
					ID:        uuid.New(),
					ServiceID: serviceID,
					Key:       "API_KEY",
					Value:     "******",
					IsSecret:  true,
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	vars, err := client.ListEnvVars(ctx, serviceID.String(), nil)

	require.NoError(t, err)
	assert.Len(t, vars, 2)
	assert.Equal(t, "DATABASE_URL", vars[0].Key)
	assert.False(t, vars[0].IsSecret)
	assert.Equal(t, "API_KEY", vars[1].Key)
	assert.True(t, vars[1].IsSecret)
}

func TestAPIClient_ListEnvVars_WithEnvironmentFilter(t *testing.T) {
	serviceID := uuid.New()
	envID := uuid.New().String()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, envID, r.URL.Query().Get("environment_id"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			EnvVars []EnvVarResponse `json:"environment_variables"`
		}{EnvVars: []EnvVarResponse{}})
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	vars, err := client.ListEnvVars(ctx, serviceID.String(), &envID)

	require.NoError(t, err)
	assert.Empty(t, vars)
}

func TestAPIClient_CreateEnvVar(t *testing.T) {
	serviceID := uuid.New()
	varID := uuid.New()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/services/"+serviceID.String()+"/env-vars", r.URL.Path)

		var req map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, "NEW_VAR", req["key"])
		assert.Equal(t, "new-value", req["value"])
		assert.Equal(t, true, req["is_secret"])

		result := EnvVarResponse{
			ID:        varID,
			ServiceID: serviceID,
			Key:       "NEW_VAR",
			Value:     "******",
			IsSecret:  true,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(result)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	result, err := client.CreateEnvVar(ctx, serviceID.String(), EnvVarRequest{
		Key:      "NEW_VAR",
		Value:    "new-value",
		IsSecret: true,
	}, nil)

	require.NoError(t, err)
	assert.Equal(t, "NEW_VAR", result.Key)
	assert.True(t, result.IsSecret)
}

func TestAPIClient_DeleteEnvVar(t *testing.T) {
	serviceID := uuid.New()
	varID := uuid.New()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/v1/services/"+serviceID.String()+"/env-vars/"+varID.String(), r.URL.Path)

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	err := client.DeleteEnvVar(ctx, serviceID.String(), varID.String())

	require.NoError(t, err)
}

func TestAPIClient_DeleteEnvVar_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("access denied"))
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	err := client.DeleteEnvVar(ctx, uuid.New().String(), uuid.New().String())

	require.Error(t, err)
	var apiErr APIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
}

func TestAPIClient_RevealEnvVar(t *testing.T) {
	serviceID := uuid.New()
	varID := uuid.New()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.URL.Path, "/reveal")

		response := struct {
			Value string `json:"value"`
		}{
			Value: "super-secret-api-key-12345",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	value, err := client.RevealEnvVar(ctx, serviceID.String(), varID.String())

	require.NoError(t, err)
	assert.Equal(t, "super-secret-api-key-12345", value)
}

// ==================== Additional API Client Tests ====================

func TestAPIClient_ListServicesWithInfo(t *testing.T) {
	svc1ID := uuid.New()
	svc2ID := uuid.New()
	projectID := uuid.New()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/v1/projects/my-project/services", r.URL.Path)

		response := struct {
			Services []*types.Service `json:"services"`
		}{
			Services: []*types.Service{
				{ID: svc1ID, ProjectID: projectID, Name: "api"},
				{ID: svc2ID, ProjectID: projectID, Name: "worker"},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	services, err := client.ListServicesWithInfo(ctx, "my-project")

	require.NoError(t, err)
	assert.Len(t, services, 2)
	assert.Equal(t, "api", services[0].Name)
	assert.Equal(t, svc1ID, services[0].ID)
	assert.Equal(t, "worker", services[1].Name)
}

func TestAPIClient_ListEnvironments(t *testing.T) {
	env1ID := uuid.New()
	env2ID := uuid.New()
	projectID := uuid.New()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/v1/projects/acme/environments", r.URL.Path)

		response := struct {
			Environments []*EnvironmentInfo `json:"environments"`
		}{
			Environments: []*EnvironmentInfo{
				{ID: env1ID, ProjectID: projectID, Name: "development", KubeNamespace: "acme-dev"},
				{ID: env2ID, ProjectID: projectID, Name: "production", KubeNamespace: "acme-prod"},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	envs, err := client.ListEnvironments(ctx, "acme")

	require.NoError(t, err)
	assert.Len(t, envs, 2)
	assert.Equal(t, "development", envs[0].Name)
	assert.Equal(t, "acme-dev", envs[0].KubeNamespace)
	assert.Equal(t, "production", envs[1].Name)
}

func TestAPIClient_BulkCreateEnvVars(t *testing.T) {
	serviceID := uuid.New()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/services/"+serviceID.String()+"/env-vars/bulk", r.URL.Path)

		var req map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		vars, ok := req["variables"].([]interface{})
		require.True(t, ok)
		assert.Len(t, vars, 2)

		response := struct {
			EnvVars []EnvVarResponse `json:"environment_variables"`
		}{
			EnvVars: []EnvVarResponse{
				{ID: uuid.New(), ServiceID: serviceID, Key: "VAR1", Value: "val1"},
				{ID: uuid.New(), ServiceID: serviceID, Key: "VAR2", Value: "val2"},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	vars, err := client.BulkCreateEnvVars(ctx, serviceID.String(), []EnvVarRequest{
		{Key: "VAR1", Value: "val1", IsSecret: false},
		{Key: "VAR2", Value: "val2", IsSecret: false},
	}, nil)

	require.NoError(t, err)
	assert.Len(t, vars, 2)
	assert.Equal(t, "VAR1", vars[0].Key)
	assert.Equal(t, "VAR2", vars[1].Key)
}

// ==================== LogStreamMessage and Type Tests ====================

func TestLogStreamMessage_JSONSerialization(t *testing.T) {
	msg := LogStreamMessage{
		Type:      "log",
		Pod:       "api-deployment-abc123-xyz",
		Container: "api",
		Timestamp: time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC),
		Message:   "Server started on port 4200",
	}

	data, err := json.Marshal(msg)
	require.NoError(t, err)

	var decoded LogStreamMessage
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "log", decoded.Type)
	assert.Equal(t, "api-deployment-abc123-xyz", decoded.Pod)
	assert.Equal(t, "api", decoded.Container)
	assert.Equal(t, "Server started on port 4200", decoded.Message)
}

func TestStreamLogsOptions_Defaults(t *testing.T) {
	opts := StreamLogsOptions{}

	assert.Equal(t, 0, opts.Lines)
	assert.False(t, opts.Timestamps)
	assert.Nil(t, opts.Since)
}

func TestAPIError_ErrorString(t *testing.T) {
	tests := []struct {
		name   string
		apiErr APIError
		want   string
	}{
		{
			name:   "error without details",
			apiErr: APIError{StatusCode: 404, Message: "not found"},
			want:   "API error 404: not found",
		},
		{
			name:   "error with details",
			apiErr: APIError{StatusCode: 500, Message: "internal error", Details: "database connection failed"},
			want:   "API error 500: internal error (database connection failed)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.apiErr.Error())
		})
	}
}

func TestNewAPIClient_Defaults(t *testing.T) {
	client := NewAPIClient("https://api.enclii.dev", "my-token")

	assert.Equal(t, "https://api.enclii.dev", client.baseURL)
	assert.Equal(t, "my-token", client.token)
	assert.Equal(t, "enclii-cli/1.0.0", client.userAgent)
	assert.NotNil(t, client.httpClient)
	assert.Equal(t, 30*time.Second, client.httpClient.Timeout)
}

func TestNewAPIClient_EmptyToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// When token is empty, no Authorization header should be set
		assert.Empty(t, r.Header.Get("Authorization"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(HealthResponse{Status: "healthy"})
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "")
	ctx := context.Background()

	health, err := client.Health(ctx)

	require.NoError(t, err)
	assert.Equal(t, "healthy", health.Status)
}
