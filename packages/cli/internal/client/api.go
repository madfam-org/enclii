package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

type APIClient struct {
	baseURL    string
	httpClient *http.Client
	token      string
	userAgent  string
}

func NewAPIClient(baseURL, token string) *APIClient {
	return &APIClient{
		baseURL:   baseURL,
		token:     token,
		userAgent: "enclii-cli/1.0.0",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type APIError struct {
	StatusCode int    `json:"status_code"`
	Message    string `json:"message"`
	Details    string `json:"details,omitempty"`
}

func (e APIError) Error() string {
	if e.Details != "" {
		return fmt.Sprintf("API error %d: %s (%s)", e.StatusCode, e.Message, e.Details)
	}
	return fmt.Sprintf("API error %d: %s", e.StatusCode, e.Message)
}

// HTTP helper methods
func (c *APIClient) makeRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}

func (c *APIClient) get(ctx context.Context, path string, result interface{}) error {
	resp, err := c.makeRequest(ctx, "GET", path, nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	return c.handleResponse(resp, result)
}

func (c *APIClient) post(ctx context.Context, path string, payload interface{}, result interface{}) error {
	var body io.Reader
	if payload != nil {
		jsonData, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %w", err)
		}
		body = bytes.NewBuffer(jsonData)
	}

	resp, err := c.makeRequest(ctx, "POST", path, body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	return c.handleResponse(resp, result)
}

func (c *APIClient) patch(ctx context.Context, path string, payload interface{}, result interface{}) error {
	var body io.Reader
	if payload != nil {
		jsonData, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %w", err)
		}
		body = bytes.NewBuffer(jsonData)
	}

	resp, err := c.makeRequest(ctx, "PATCH", path, body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	return c.handleResponse(resp, result)
}

func (c *APIClient) handleResponse(resp *http.Response, result interface{}) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var apiErr struct {
			Error string `json:"error"`
		}

		if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Error != "" {
			return APIError{
				StatusCode: resp.StatusCode,
				Message:    apiErr.Error,
			}
		}

		return APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	if result != nil {
		if err := json.Unmarshal(body, result); err != nil {
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}
	}

	return nil
}

// Projects
func (c *APIClient) CreateProject(ctx context.Context, name, slug string) (*types.Project, error) {
	payload := map[string]string{
		"name": name,
		"slug": slug,
	}

	var project types.Project
	if err := c.post(ctx, "/v1/projects", payload, &project); err != nil {
		return nil, fmt.Errorf("failed to create project: %w", err)
	}

	return &project, nil
}

func (c *APIClient) GetProject(ctx context.Context, slug string) (*types.Project, error) {
	var project types.Project
	if err := c.get(ctx, fmt.Sprintf("/v1/projects/%s", slug), &project); err != nil {
		return nil, fmt.Errorf("failed to get project: %w", err)
	}

	return &project, nil
}

func (c *APIClient) ListProjects(ctx context.Context) ([]*types.Project, error) {
	var response struct {
		Projects []*types.Project `json:"projects"`
	}

	if err := c.get(ctx, "/v1/projects", &response); err != nil {
		return nil, fmt.Errorf("failed to list projects: %w", err)
	}

	return response.Projects, nil
}

// Services
func (c *APIClient) CreateService(ctx context.Context, projectSlug string, service *types.Service) (*types.Service, error) {
	payload := servicePayload(service)

	var createdService types.Service
	if err := c.post(ctx, fmt.Sprintf("/v1/projects/%s/services", projectSlug), payload, &createdService); err != nil {
		return nil, fmt.Errorf("failed to create service: %w", err)
	}

	return &createdService, nil
}

func (c *APIClient) UpdateService(ctx context.Context, serviceID string, service *types.Service) (*types.Service, error) {
	payload := servicePayload(service)

	var response struct {
		Service *types.Service `json:"service"`
		Message string         `json:"message"`
	}
	if err := c.patch(ctx, fmt.Sprintf("/v1/services/%s", serviceID), payload, &response); err != nil {
		return nil, fmt.Errorf("failed to update service: %w", err)
	}
	if response.Service == nil {
		return nil, fmt.Errorf("failed to update service: missing service response")
	}

	return response.Service, nil
}

func servicePayload(service *types.Service) map[string]interface{} {
	payload := map[string]interface{}{
		"name":               service.Name,
		"git_repo":           service.GitRepo,
		"app_path":           service.AppPath,
		"auto_deploy":        service.AutoDeploy,
		"auto_deploy_branch": service.AutoDeployBranch,
		"auto_deploy_env":    service.AutoDeployEnv,
		"build_config":       service.BuildConfig,
		"jobs":               service.Jobs,
	}
	if service.Type != "" {
		payload["type"] = service.Type
	}
	if service.Region != "" {
		payload["region"] = service.Region
	}
	return payload
}

func (c *APIClient) GetService(ctx context.Context, serviceID string) (*types.Service, error) {
	var service types.Service
	if err := c.get(ctx, fmt.Sprintf("/v1/services/%s", serviceID), &service); err != nil {
		return nil, fmt.Errorf("failed to get service: %w", err)
	}

	return &service, nil
}

func (c *APIClient) ListServices(ctx context.Context, projectSlug string) ([]*types.Service, error) {
	var response struct {
		Services []*types.Service `json:"services"`
	}

	if err := c.get(ctx, fmt.Sprintf("/v1/projects/%s/services", projectSlug), &response); err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	return response.Services, nil
}

// DeleteService deletes a service by ID
func (c *APIClient) DeleteService(ctx context.Context, serviceID string) error {
	resp, err := c.makeRequest(ctx, "DELETE", fmt.Sprintf("/v1/services/%s", serviceID), nil)
	if err != nil {
		return fmt.Errorf("failed to delete service: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to delete service: %s", string(body))
	}

	return nil
}

// Environments
func (c *APIClient) CreateEnvironment(ctx context.Context, projectSlug, envName string) (*types.Environment, error) {
	payload := map[string]string{
		"name": envName,
	}

	var env types.Environment
	if err := c.post(ctx, fmt.Sprintf("/v1/projects/%s/environments", projectSlug), payload, &env); err != nil {
		return nil, fmt.Errorf("failed to create environment: %w", err)
	}

	return &env, nil
}

// Build & Deploy
func (c *APIClient) BuildService(ctx context.Context, serviceID, gitSHA string) (*types.Release, error) {
	payload := map[string]string{
		"git_sha": gitSHA,
	}

	var release types.Release
	if err := c.post(ctx, fmt.Sprintf("/v1/services/%s/build", serviceID), payload, &release); err != nil {
		return nil, fmt.Errorf("failed to build service: %w", err)
	}

	return &release, nil
}

func (c *APIClient) DeployService(ctx context.Context, serviceID string, req DeployRequest) (*types.Deployment, error) {
	var deployment types.Deployment
	if err := c.post(ctx, fmt.Sprintf("/v1/services/%s/deploy", serviceID), req, &deployment); err != nil {
		return nil, fmt.Errorf("failed to deploy service: %w", err)
	}

	return &deployment, nil
}

func (c *APIClient) GetServiceStatus(ctx context.Context, serviceID string) (*ServiceStatus, error) {
	var status ServiceStatus
	if err := c.get(ctx, fmt.Sprintf("/v1/services/%s/status", serviceID), &status); err != nil {
		return nil, fmt.Errorf("failed to get service status: %w", err)
	}

	return &status, nil
}

func (c *APIClient) ListReleases(ctx context.Context, serviceID string) ([]*types.Release, error) {
	var response struct {
		Releases []*types.Release `json:"releases"`
	}

	if err := c.get(ctx, fmt.Sprintf("/v1/services/%s/releases", serviceID), &response); err != nil {
		return nil, fmt.Errorf("failed to list releases: %w", err)
	}

	return response.Releases, nil
}

// Deployments
func (c *APIClient) GetLatestDeployment(ctx context.Context, serviceID string) (*DeploymentWithRelease, error) {
	var response DeploymentWithRelease
	if err := c.get(ctx, fmt.Sprintf("/v1/services/%s/deployments/latest", serviceID), &response); err != nil {
		return nil, fmt.Errorf("failed to get latest deployment: %w", err)
	}

	return &response, nil
}

func (c *APIClient) GetDeployment(ctx context.Context, deploymentID string) (*types.Deployment, error) {
	var deployment types.Deployment
	if err := c.get(ctx, fmt.Sprintf("/v1/deployments/%s", deploymentID), &deployment); err != nil {
		return nil, fmt.Errorf("failed to get deployment: %w", err)
	}

	return &deployment, nil
}

// GetDeploymentByVersion resolves a deployment by Heroku-style semantic
// version (P2.6). The `version` argument is the integer component of v{n};
// passing 42 maps to /v1/services/<id>/versions/42.
func (c *APIClient) GetDeploymentByVersion(ctx context.Context, serviceID string, version int) (*types.Deployment, error) {
	var deployment types.Deployment
	if err := c.get(ctx, fmt.Sprintf("/v1/services/%s/versions/%d", serviceID, version), &deployment); err != nil {
		return nil, fmt.Errorf("failed to get deployment by version v%d: %w", version, err)
	}
	return &deployment, nil
}

func (c *APIClient) ListServiceDeployments(ctx context.Context, serviceID string) ([]*types.Deployment, error) {
	var response struct {
		Deployments []*types.Deployment `json:"deployments"`
	}

	if err := c.get(ctx, fmt.Sprintf("/v1/services/%s/deployments", serviceID), &response); err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}

	return response.Deployments, nil
}

// Logs
func (c *APIClient) GetLogs(ctx context.Context, deploymentID string, opts LogOptions) ([]LogLine, error) {
	params := url.Values{}
	if opts.Follow {
		params.Set("follow", "true")
	}
	if opts.Lines > 0 {
		params.Set("lines", fmt.Sprintf("%d", opts.Lines))
	}
	if opts.Since != nil {
		params.Set("since", opts.Since.Format(time.RFC3339))
	}

	var response struct {
		Logs []LogLine `json:"logs"`
	}

	endpoint := fmt.Sprintf("/v1/deployments/%s/logs", deploymentID)
	if params.Encode() != "" {
		endpoint += "?" + params.Encode()
	}

	if err := c.get(ctx, endpoint, &response); err != nil {
		return nil, fmt.Errorf("failed to get logs: %w", err)
	}

	return response.Logs, nil
}

// GetLogsRaw returns logs as a string (for streaming)
func (c *APIClient) GetLogsRaw(ctx context.Context, deploymentID string, opts LogOptions) (string, error) {
	params := url.Values{}
	if opts.Follow {
		params.Set("follow", "true")
	}
	if opts.Lines > 0 {
		params.Set("lines", fmt.Sprintf("%d", opts.Lines))
	}
	if opts.Since != nil {
		params.Set("since", opts.Since.Format(time.RFC3339))
	}

	var response struct {
		Logs string `json:"logs"`
	}

	endpoint := fmt.Sprintf("/v1/deployments/%s/logs", deploymentID)
	if params.Encode() != "" {
		endpoint += "?" + params.Encode()
	}

	if err := c.get(ctx, endpoint, &response); err != nil {
		return "", fmt.Errorf("failed to get logs: %w", err)
	}

	return response.Logs, nil
}

// GetServiceLogsHistoryRaw returns logs for a service/environment without
// depending on the latest deployment row. This is the preferred path for
// operator one-shot reads because live pods are service-scoped, while
// deployment records can lag or be reconciled after delivery incidents.
func (c *APIClient) GetServiceLogsHistoryRaw(ctx context.Context, serviceID, envName string, opts LogOptions) (string, error) {
	params := url.Values{}
	if envName != "" {
		params.Set("env", envName)
	}
	if opts.Lines > 0 {
		params.Set("lines", fmt.Sprintf("%d", opts.Lines))
	}
	if opts.Since != nil {
		params.Set("since", opts.Since.Format(time.RFC3339))
	}

	var response struct {
		Logs string `json:"logs"`
	}

	endpoint := fmt.Sprintf("/v1/services/%s/logs/history", serviceID)
	if params.Encode() != "" {
		endpoint += "?" + params.Encode()
	}

	if err := c.get(ctx, endpoint, &response); err != nil {
		return "", fmt.Errorf("failed to get service logs: %w", err)
	}

	return response.Logs, nil
}

// Rollback
func (c *APIClient) RollbackDeployment(ctx context.Context, deploymentID string, req RollbackRequest) error {
	if err := c.post(ctx, fmt.Sprintf("/v1/deployments/%s/rollback", deploymentID), req, nil); err != nil {
		return fmt.Errorf("failed to rollback deployment: %w", err)
	}

	return nil
}

// InstantRollbackRequest is the body for POST /v1/services/{id}/rollback.
type InstantRollbackRequest struct {
	TargetDeploymentID string `json:"target_deployment_id"`
	Reason             string `json:"reason,omitempty"`
	ChangeTicketURL    string `json:"change_ticket_url,omitempty"`
}

// InstantRollbackResponse mirrors the API response.
type InstantRollbackResponse struct {
	Message          string `json:"message"`
	TookMS           int64  `json:"took_ms"`
	ScaledUp         bool   `json:"scaled_up"`
	FromDeploymentID string `json:"from_deployment_id,omitempty"`
	ToDeploymentID   string `json:"to_deployment_id"`
	// P2.6: Heroku-style v-numbers. Pointers because historical rows may
	// not have version_number allocated yet.
	FromVersion   *int   `json:"from_version,omitempty"`
	ToVersion     *int   `json:"to_version,omitempty"`
	ReadyReplicas int32  `json:"ready_replicas"`
	Strategy      string `json:"strategy"`
	Namespace     string `json:"namespace"`
}

// InstantRollback performs a selector-flip rollback — traffic shifts to the
// target deployment's ReplicaSet in <30s for still-running targets.
func (c *APIClient) InstantRollback(ctx context.Context, serviceID string, req InstantRollbackRequest) (*InstantRollbackResponse, error) {
	var resp InstantRollbackResponse
	if err := c.post(ctx, fmt.Sprintf("/v1/services/%s/rollback", serviceID), req, &resp); err != nil {
		return nil, fmt.Errorf("failed to perform instant rollback: %w", err)
	}
	return &resp, nil
}

// Health check
func (c *APIClient) Health(ctx context.Context) (*HealthResponse, error) {
	var health HealthResponse
	if err := c.get(ctx, "/health", &health); err != nil {
		return nil, fmt.Errorf("failed to check health: %w", err)
	}

	return &health, nil
}

// Request/Response types
type DeployRequest struct {
	ReleaseID       string            `json:"release_id"`
	EnvironmentName string            `json:"environment_name"` // e.g., "dev", "staging", "production"
	Environment     map[string]string `json:"environment,omitempty"`
	Replicas        int               `json:"replicas,omitempty"`
}

type RollbackRequest struct {
	ToRelease string `json:"to_release,omitempty"`
}

type LogOptions struct {
	Follow bool
	Lines  int
	Since  *time.Time
}

type LogLine struct {
	Timestamp time.Time `json:"timestamp"`
	Pod       string    `json:"pod"`
	Message   string    `json:"message"`
	Level     string    `json:"level,omitempty"`
}

type ServiceStatus struct {
	ServiceID   string                 `json:"service_id"`
	Environment string                 `json:"environment"`
	Status      types.DeploymentStatus `json:"status"`
	Health      types.HealthStatus     `json:"health"`
	Replicas    int                    `json:"replicas"`
	Version     string                 `json:"version"`
	Uptime      time.Duration          `json:"uptime"`
	LastDeploy  time.Time              `json:"last_deploy"`
}

type DeploymentWithRelease struct {
	Deployment *types.Deployment `json:"deployment"`
	Release    *types.Release    `json:"release,omitempty"`
}

type HealthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Version string `json:"version"`
}

// Environment Variables / Secrets

// EnvVarRequest represents a request to create or update an environment variable
type EnvVarRequest struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	IsSecret bool   `json:"is_secret"`
}

// EnvVarResponse represents an environment variable in API responses
type EnvVarResponse struct {
	ID            uuid.UUID  `json:"id"`
	ServiceID     uuid.UUID  `json:"service_id"`
	EnvironmentID *uuid.UUID `json:"environment_id,omitempty"`
	Key           string     `json:"key"`
	Value         string     `json:"value"` // Masked as "••••••" if is_secret=true
	IsSecret      bool       `json:"is_secret"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// ServiceInfo represents basic service information for CLI use
type ServiceInfo struct {
	ID        uuid.UUID `json:"id"`
	ProjectID uuid.UUID `json:"project_id"`
	Name      string    `json:"name"`
}

// EnvironmentInfo represents basic environment information for CLI use
type EnvironmentInfo struct {
	ID            uuid.UUID `json:"id"`
	ProjectID     uuid.UUID `json:"project_id"`
	Name          string    `json:"name"`
	KubeNamespace string    `json:"kube_namespace"`
}

// ListEnvVars returns all environment variables for a service
func (c *APIClient) ListEnvVars(ctx context.Context, serviceID string, environmentID *string) ([]EnvVarResponse, error) {
	endpoint := fmt.Sprintf("/v1/services/%s/env-vars", serviceID)
	if environmentID != nil && *environmentID != "" {
		endpoint += "?environment_id=" + url.QueryEscape(*environmentID)
	}

	var response struct {
		EnvVars []EnvVarResponse `json:"environment_variables"`
	}

	if err := c.get(ctx, endpoint, &response); err != nil {
		return nil, fmt.Errorf("failed to list env vars: %w", err)
	}

	return response.EnvVars, nil
}

// CreateEnvVar creates a new environment variable
func (c *APIClient) CreateEnvVar(ctx context.Context, serviceID string, req EnvVarRequest, environmentID *string) (*EnvVarResponse, error) {
	payload := map[string]interface{}{
		"key":       req.Key,
		"value":     req.Value,
		"is_secret": req.IsSecret,
	}

	if environmentID != nil && *environmentID != "" {
		payload["environment_id"] = *environmentID
	}

	var result EnvVarResponse
	if err := c.post(ctx, fmt.Sprintf("/v1/services/%s/env-vars", serviceID), payload, &result); err != nil {
		return nil, fmt.Errorf("failed to create env var: %w", err)
	}

	return &result, nil
}

// BulkCreateEnvVars creates multiple environment variables at once
func (c *APIClient) BulkCreateEnvVars(ctx context.Context, serviceID string, vars []EnvVarRequest, environmentID *string) ([]EnvVarResponse, error) {
	payload := map[string]interface{}{
		"variables": vars,
	}

	if environmentID != nil && *environmentID != "" {
		payload["environment_id"] = *environmentID
	}

	var response struct {
		EnvVars []EnvVarResponse `json:"environment_variables"`
	}

	if err := c.post(ctx, fmt.Sprintf("/v1/services/%s/env-vars/bulk", serviceID), payload, &response); err != nil {
		return nil, fmt.Errorf("failed to bulk create env vars: %w", err)
	}

	return response.EnvVars, nil
}

// DeleteEnvVar deletes an environment variable
func (c *APIClient) DeleteEnvVar(ctx context.Context, serviceID, envVarID string) error {
	resp, err := c.makeRequest(ctx, "DELETE", fmt.Sprintf("/v1/services/%s/env-vars/%s", serviceID, envVarID), nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	return nil
}

// RevealEnvVar reveals the actual value of a secret (logged for audit)
func (c *APIClient) RevealEnvVar(ctx context.Context, serviceID, envVarID string) (string, error) {
	var response struct {
		Value string `json:"value"`
	}

	if err := c.post(ctx, fmt.Sprintf("/v1/services/%s/env-vars/%s/reveal", serviceID, envVarID), nil, &response); err != nil {
		return "", fmt.Errorf("failed to reveal env var: %w", err)
	}

	return response.Value, nil
}

// ListServicesWithInfo returns all services for a project with basic info
func (c *APIClient) ListServicesWithInfo(ctx context.Context, projectSlug string) ([]*ServiceInfo, error) {
	services, err := c.ListServices(ctx, projectSlug)
	if err != nil {
		return nil, err
	}

	result := make([]*ServiceInfo, len(services))
	for i, svc := range services {
		result[i] = &ServiceInfo{
			ID:        svc.ID,
			ProjectID: svc.ProjectID,
			Name:      svc.Name,
		}
	}

	return result, nil
}

// ListEnvironments returns all environments for a project
func (c *APIClient) ListEnvironments(ctx context.Context, projectSlug string) ([]*EnvironmentInfo, error) {
	var response struct {
		Environments []*EnvironmentInfo `json:"environments"`
	}

	if err := c.get(ctx, fmt.Sprintf("/v1/projects/%s/environments", projectSlug), &response); err != nil {
		return nil, fmt.Errorf("failed to list environments: %w", err)
	}

	return response.Environments, nil
}
