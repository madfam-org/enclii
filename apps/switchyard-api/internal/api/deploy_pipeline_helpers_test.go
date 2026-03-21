package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// --- Fixtures ---

const (
	testWebhookSecret     = "test-webhook-secret-12345"
	testRoundhouseAPIKey  = "roundhouse-api-key-test"
	testArgocdSecret      = "argocd-webhook-secret-test"
	testRegistry          = "ghcr.io/test-org/test-project"
	testGitHubToken       = "gh-test-token"
	testRepoFullName      = "test-org/test-project"
	testRepoCloneURL      = "https://github.com/test-org/test-project.git"
	testRepoHTMLURL       = "https://github.com/test-org/test-project"
	testRepoSSHURL        = "git@github.com:test-org/test-project.git"
	testDefaultBranch     = "main"
)

// newTestPushEvent creates a valid GitHub push webhook JSON payload.
func newTestPushEvent(repo, sha, branch string) []byte {
	event := GitHubPushEvent{
		Ref:     "refs/heads/" + branch,
		Before:  "0000000000000000000000000000000000000000",
		After:   sha,
		Created: false,
		Deleted: false,
		Forced:  false,
	}
	event.Repository.ID = 12345
	event.Repository.Name = repoName(repo)
	event.Repository.FullName = repo
	event.Repository.CloneURL = "https://github.com/" + repo + ".git"
	event.Repository.SSHURL = "git@github.com:" + repo + ".git"
	event.Repository.HTMLURL = "https://github.com/" + repo
	event.Pusher.Name = "test-user"
	event.Pusher.Email = "test@example.com"
	event.HeadCommit.ID = sha
	event.HeadCommit.Message = "feat: test commit"
	event.HeadCommit.Timestamp = time.Now().Format(time.RFC3339)
	event.HeadCommit.Added = []string{"src/main.go"}
	event.HeadCommit.Modified = []string{"go.mod"}
	event.HeadCommit.Removed = nil
	event.HeadCommit.Author.Name = "Test Author"
	event.HeadCommit.Author.Email = "author@example.com"

	data, _ := json.Marshal(event)
	return data
}

// newTestPushEventWithFiles creates a push event with specific changed files.
func newTestPushEventWithFiles(repo, sha, branch string, added, modified, removed []string) []byte {
	event := GitHubPushEvent{
		Ref:     "refs/heads/" + branch,
		Before:  "0000000000000000000000000000000000000000",
		After:   sha,
		Created: false,
		Deleted: false,
		Forced:  false,
	}
	event.Repository.ID = 12345
	event.Repository.Name = repoName(repo)
	event.Repository.FullName = repo
	event.Repository.CloneURL = "https://github.com/" + repo + ".git"
	event.Repository.SSHURL = "git@github.com:" + repo + ".git"
	event.Repository.HTMLURL = "https://github.com/" + repo
	event.Pusher.Name = "test-user"
	event.Pusher.Email = "test@example.com"
	event.HeadCommit.ID = sha
	event.HeadCommit.Message = "feat: test commit with files"
	event.HeadCommit.Timestamp = time.Now().Format(time.RFC3339)
	event.HeadCommit.Added = added
	event.HeadCommit.Modified = modified
	event.HeadCommit.Removed = removed
	event.HeadCommit.Author.Name = "Test Author"
	event.HeadCommit.Author.Email = "author@example.com"

	data, _ := json.Marshal(event)
	return data
}

// newTestDeleteEvent creates a push event for a branch deletion.
func newTestDeleteEvent(repo, branch string) []byte {
	event := GitHubPushEvent{
		Ref:     "refs/heads/" + branch,
		Before:  "abc1234567890abcdef1234567890abcdef123456",
		After:   "0000000000000000000000000000000000000000",
		Created: false,
		Deleted: true,
		Forced:  false,
	}
	event.Repository.ID = 12345
	event.Repository.Name = repoName(repo)
	event.Repository.FullName = repo
	event.Repository.CloneURL = "https://github.com/" + repo + ".git"
	event.Repository.SSHURL = "git@github.com:" + repo + ".git"
	event.Repository.HTMLURL = "https://github.com/" + repo
	event.Pusher.Name = "test-user"
	event.Pusher.Email = "test@example.com"

	data, _ := json.Marshal(event)
	return data
}

// newTestBuildCallback creates a BuildCallbackRequest for testing.
func newTestBuildCallback(releaseID uuid.UUID, success bool) BuildCallbackRequest {
	req := BuildCallbackRequest{
		JobID:        uuid.New(),
		ReleaseID:    releaseID,
		Success:      success,
		DurationSecs: 42.5,
	}
	if success {
		req.ImageURI = testRegistry + "/test-service:abc1234"
		req.ImageDigest = "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
		req.ImageSizeMB = 128.5
		req.SBOM = `{"bomFormat":"CycloneDX","specVersion":"1.4"}`
		req.SBOMFormat = "cyclonedx-json"
		req.ImageSignature = "MEUCIQDfake..."
		req.LogsURL = "https://logs.example.com/builds/123"
	} else {
		req.ErrorMessage = "build failed: exit code 1"
		req.LogsURL = "https://logs.example.com/builds/123"
	}
	return req
}

// newTestArgocdCallback creates an ArgocdSyncRequest for testing.
func newTestArgocdCallback(appName, trigger, syncStatus, healthStatus string, images []string) ArgocdSyncRequest {
	return ArgocdSyncRequest{
		AppName:      appName,
		Trigger:      trigger,
		SyncStatus:   syncStatus,
		HealthStatus: healthStatus,
		Revision:     "abc1234567890abcdef1234567890abcdef123456",
		Images:       images,
		StartedAt:    time.Now().Add(-30 * time.Second).Format(time.RFC3339),
		FinishedAt:   time.Now().Format(time.RFC3339),
	}
}

// --- Test Setup ---

// newTestService creates a types.Service with sensible defaults.
func newTestService(name, gitRepo string) *types.Service {
	return &types.Service{
		ID:              uuid.New(),
		ProjectID:       uuid.New(),
		Name:            name,
		GitRepo:         gitRepo,
		AutoDeploy:      true,
		AutoDeployEnv:   "production",
		Status:          "running",
		Health:          types.HealthStatusHealthy,
		DesiredReplicas: 1,
		ReadyReplicas:   1,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
}

// newTestServiceWithWatchPaths creates a service with watch path filtering.
func newTestServiceWithWatchPaths(name, gitRepo string, watchPaths []string) *types.Service {
	svc := newTestService(name, gitRepo)
	svc.WatchPaths = watchPaths
	return svc
}

// newTestEnvironment creates a types.Environment for testing.
func newTestEnvironment(projectID uuid.UUID, name string) *types.Environment {
	return &types.Environment{
		ID:        uuid.New(),
		ProjectID: projectID,
		Name:      name,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

// --- HTTP Helpers ---

// signPayload computes the GitHub webhook HMAC-SHA256 signature.
func signPayload(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// sendWebhook sends a signed POST to the webhook endpoint and returns the response.
func sendWebhook(engine *gin.Engine, eventType string, body []byte, secret string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/webhooks/github", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", eventType)
	req.Header.Set("X-GitHub-Delivery", uuid.New().String())
	req.Header.Set("X-Hub-Signature-256", signPayload(body, secret))
	engine.ServeHTTP(w, req)
	return w
}

// sendBuildCallback sends a build callback request.
func sendBuildCallback(engine *gin.Engine, callback BuildCallbackRequest, apiKey string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(callback)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/callbacks/build-complete", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	engine.ServeHTTP(w, req)
	return w
}

// sendArgocdCallback sends an ArgoCD sync callback request.
func sendArgocdCallback(engine *gin.Engine, callback ArgocdSyncRequest, secret string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(callback)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/callbacks/argocd-sync", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	engine.ServeHTTP(w, req)
	return w
}

// --- Response Parsing ---

type webhookResponse struct {
	Message        string `json:"message"`
	Repo           string `json:"repo"`
	GitSHA         string `json:"git_sha"`
	Branch         string `json:"branch"`
	ServiceCount   int    `json:"service_count"`
	TriggeredCount int    `json:"triggered_count"`
	SkippedCount   int    `json:"skipped_count"`
	ChangedFiles   int    `json:"changed_files"`
	Builds         []struct {
		Service   string `json:"service"`
		ReleaseID string `json:"release_id"`
		Status    string `json:"status"`
		Skipped   bool   `json:"skipped"`
		Reason    string `json:"reason"`
	} `json:"builds"`
}

func parseWebhookResponse(t *testing.T, w *httptest.ResponseRecorder) webhookResponse {
	t.Helper()
	var resp webhookResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse webhook response: %v\nBody: %s", err, w.Body.String())
	}
	return resp
}

type callbackResponse struct {
	Status  string `json:"status"`
	Error   string `json:"error"`
}

func parseCallbackResponse(t *testing.T, w *httptest.ResponseRecorder) callbackResponse {
	t.Helper()
	var resp callbackResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse callback response: %v\nBody: %s", err, w.Body.String())
	}
	return resp
}

type argocdResponse struct {
	Status             string `json:"status"`
	DeploymentsCreated int    `json:"deployments_created"`
	Error              string `json:"error"`
}

func parseArgocdResponse(t *testing.T, w *httptest.ResponseRecorder) argocdResponse {
	t.Helper()
	var resp argocdResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse ArgoCD response: %v\nBody: %s", err, w.Body.String())
	}
	return resp
}

// --- Utility ---

func repoName(fullName string) string {
	parts := strings.Split(fullName, "/")
	if len(parts) >= 2 {
		return parts[1]
	}
	return fullName
}

func testSHA() string {
	return fmt.Sprintf("abc1234%s", uuid.New().String()[:33])
}

// newTestLogger creates a structured logger for tests.
func newTestLogger(t *testing.T) logging.Logger {
	t.Helper()
	cfg := &logging.LogConfig{
		Level:  "error", // Suppress noise in tests
		Format: "text",
	}
	logger, err := logging.NewStructuredLogger(cfg)
	if err != nil {
		t.Fatalf("Failed to create test logger: %v", err)
	}
	return logger
}

// newTestConfig creates a config suitable for pipeline testing.
func newTestConfig() *config.Config {
	return &config.Config{
		Environment:         "test",
		Port:                "8080",
		Registry:            testRegistry,
		GitHubWebhookSecret: testWebhookSecret,
		RoundhouseAPIKey:    testRoundhouseAPIKey,
		ArgocdWebhookSecret: testArgocdSecret,
		GitHubToken:         testGitHubToken,
		BuildMode:           "roundhouse",
		SelfURL:             "http://localhost:8080",
		AuthMode:            "local",
	}
}
