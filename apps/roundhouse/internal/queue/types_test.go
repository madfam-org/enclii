package queue

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- BuildJob serialization ---

func TestBuildJob_JSONRoundtrip(t *testing.T) {
	jobID := uuid.New()
	releaseID := uuid.New()
	serviceID := uuid.New()
	projectID := uuid.New()
	now := time.Now().Truncate(time.Millisecond)

	original := &BuildJob{
		ID:          jobID,
		ReleaseID:   releaseID,
		ServiceID:   serviceID,
		ServiceName: "my-service",
		ProjectID:   projectID,
		ProjectSlug: "my-project",
		GitRepo:     "https://github.com/org/repo.git",
		GitSHA:      "abc1234567890123456789012345678901234567",
		GitBranch:   "main",
		BuildConfig: BuildConfig{
			Type:       "dockerfile",
			Dockerfile: "Dockerfile.prod",
			Buildpack:  "",
			Context:    "src",
			BuildArgs:  map[string]string{"GO_VERSION": "1.22", "NODE_ENV": "production"},
			Target:     "production",
		},
		CallbackURL: "https://api.example.com/callback",
		CreatedAt:   now,
		Priority:    5,
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded BuildJob
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, original.ID, decoded.ID)
	assert.Equal(t, original.ReleaseID, decoded.ReleaseID)
	assert.Equal(t, original.ServiceID, decoded.ServiceID)
	assert.Equal(t, original.ServiceName, decoded.ServiceName)
	assert.Equal(t, original.ProjectID, decoded.ProjectID)
	assert.Equal(t, original.ProjectSlug, decoded.ProjectSlug)
	assert.Equal(t, original.GitRepo, decoded.GitRepo)
	assert.Equal(t, original.GitSHA, decoded.GitSHA)
	assert.Equal(t, original.GitBranch, decoded.GitBranch)
	assert.Equal(t, original.CallbackURL, decoded.CallbackURL)
	assert.Equal(t, original.Priority, decoded.Priority)
	assert.WithinDuration(t, original.CreatedAt, decoded.CreatedAt, time.Millisecond)
}

func TestBuildJob_JSONFieldNames(t *testing.T) {
	job := &BuildJob{
		ID:          uuid.New(),
		ServiceName: "test-svc",
		ProjectSlug: "test-proj",
		GitRepo:     "https://github.com/org/repo.git",
		GitSHA:      "abc123",
		Priority:    3,
	}

	data, err := json.Marshal(job)
	require.NoError(t, err)

	var raw map[string]interface{}
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)

	// Verify JSON field names match struct tags
	assert.Contains(t, raw, "service_name")
	assert.Contains(t, raw, "project_slug")
	assert.Contains(t, raw, "git_repo")
	assert.Contains(t, raw, "git_sha")
	assert.Contains(t, raw, "git_branch")
	assert.Contains(t, raw, "callback_url")
	assert.Contains(t, raw, "build_config")
	assert.Contains(t, raw, "priority")
}

func TestBuildJob_EmptyFields(t *testing.T) {
	job := &BuildJob{}

	data, err := json.Marshal(job)
	require.NoError(t, err)

	var decoded BuildJob
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, uuid.Nil, decoded.ID)
	assert.Equal(t, "", decoded.ServiceName)
	assert.Equal(t, "", decoded.GitRepo)
	assert.Equal(t, 0, decoded.Priority)
}

// --- BuildConfig serialization ---

func TestBuildConfig_JSONRoundtrip(t *testing.T) {
	tests := []struct {
		name   string
		config BuildConfig
	}{
		{
			name: "dockerfile_config",
			config: BuildConfig{
				Type:       "dockerfile",
				Dockerfile: "Dockerfile.prod",
				Context:    "apps/api",
				BuildArgs:  map[string]string{"VERSION": "1.0"},
				Target:     "release",
			},
		},
		{
			name: "buildpack_config",
			config: BuildConfig{
				Type:      "buildpack",
				Buildpack: "heroku/builder:22",
				Context:   ".",
			},
		},
		{
			name: "auto_config",
			config: BuildConfig{
				Type: "auto",
			},
		},
		{
			name:   "empty_config",
			config: BuildConfig{},
		},
		{
			name: "multiple_build_args",
			config: BuildConfig{
				Type: "dockerfile",
				BuildArgs: map[string]string{
					"GO_VERSION":   "1.22",
					"ALPINE_VER":   "3.19",
					"CGO_ENABLED":  "0",
					"BUILD_TARGET": "production",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.config)
			require.NoError(t, err)

			var decoded BuildConfig
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)

			assert.Equal(t, tt.config.Type, decoded.Type)
			assert.Equal(t, tt.config.Dockerfile, decoded.Dockerfile)
			assert.Equal(t, tt.config.Buildpack, decoded.Buildpack)
			assert.Equal(t, tt.config.Context, decoded.Context)
			assert.Equal(t, tt.config.Target, decoded.Target)

			if tt.config.BuildArgs != nil {
				assert.Equal(t, len(tt.config.BuildArgs), len(decoded.BuildArgs))
				for k, v := range tt.config.BuildArgs {
					assert.Equal(t, v, decoded.BuildArgs[k])
				}
			}
		})
	}
}

// --- JobStatus constants ---

func TestJobStatus_Values(t *testing.T) {
	tests := []struct {
		status JobStatus
		want   string
	}{
		{StatusQueued, "queued"},
		{StatusBuilding, "building"},
		{StatusCompleted, "completed"},
		{StatusFailed, "failed"},
		{StatusCancelled, "cancelled"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, string(tt.status))
		})
	}
}

func TestJobStatus_Uniqueness(t *testing.T) {
	statuses := []JobStatus{
		StatusQueued,
		StatusBuilding,
		StatusCompleted,
		StatusFailed,
		StatusCancelled,
	}

	seen := make(map[JobStatus]bool)
	for _, s := range statuses {
		assert.False(t, seen[s], "duplicate status value: %s", s)
		seen[s] = true
	}
}

// --- BuildResult serialization ---

func TestBuildResult_JSONRoundtrip(t *testing.T) {
	jobID := uuid.New()
	releaseID := uuid.New()

	original := &BuildResult{
		JobID:          jobID,
		ReleaseID:      releaseID,
		Success:        true,
		ImageURI:       "ghcr.io/org/service:abc12345",
		ImageDigest:    "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		ImageSizeMB:    125.5,
		SBOM:           `{"spdx": "content"}`,
		SBOMFormat:     "spdx-json",
		ImageSignature: "cosign-sig-data",
		DurationSecs:   45.23,
		LogsURL:        "https://builds.example.com/logs/123",
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded BuildResult
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, original.JobID, decoded.JobID)
	assert.Equal(t, original.ReleaseID, decoded.ReleaseID)
	assert.True(t, decoded.Success)
	assert.Equal(t, original.ImageURI, decoded.ImageURI)
	assert.Equal(t, original.ImageDigest, decoded.ImageDigest)
	assert.InDelta(t, original.ImageSizeMB, decoded.ImageSizeMB, 0.01)
	assert.Equal(t, original.SBOM, decoded.SBOM)
	assert.Equal(t, original.SBOMFormat, decoded.SBOMFormat)
	assert.Equal(t, original.ImageSignature, decoded.ImageSignature)
	assert.InDelta(t, original.DurationSecs, decoded.DurationSecs, 0.01)
	assert.Equal(t, original.LogsURL, decoded.LogsURL)
}

func TestBuildResult_ErrorMessage_OmitEmpty(t *testing.T) {
	result := &BuildResult{
		JobID:   uuid.New(),
		Success: true,
		// ErrorMessage is intentionally empty
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	var raw map[string]interface{}
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)

	// ErrorMessage should be omitted when empty (omitempty tag)
	_, hasError := raw["error_message"]
	assert.False(t, hasError, "error_message should be omitted when empty")
}

func TestBuildResult_FailedResult(t *testing.T) {
	result := &BuildResult{
		JobID:        uuid.New(),
		Success:      false,
		ErrorMessage: "build failed: Dockerfile syntax error at line 15",
		DurationSecs: 3.2,
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	var decoded BuildResult
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.False(t, decoded.Success)
	assert.Equal(t, result.ErrorMessage, decoded.ErrorMessage)
	assert.Empty(t, decoded.ImageURI)
	assert.Empty(t, decoded.ImageDigest)
}

// --- WebhookPayload serialization ---

func TestWebhookPayload_JSONRoundtrip(t *testing.T) {
	original := &WebhookPayload{
		Provider:   "github",
		Event:      "push",
		Repository: "https://github.com/org/repo.git",
		Branch:     "main",
		CommitSHA:  "abc1234567890123456789012345678901234567",
		Author:     "developer1",
		Message:    "feat: add new endpoint",
		PRURL:      "",
		PRNumber:   0,
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded WebhookPayload
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, original.Provider, decoded.Provider)
	assert.Equal(t, original.Event, decoded.Event)
	assert.Equal(t, original.Repository, decoded.Repository)
	assert.Equal(t, original.Branch, decoded.Branch)
	assert.Equal(t, original.CommitSHA, decoded.CommitSHA)
	assert.Equal(t, original.Author, decoded.Author)
	assert.Equal(t, original.Message, decoded.Message)
}

func TestWebhookPayload_PRFields_OmitEmpty(t *testing.T) {
	payload := &WebhookPayload{
		Provider:   "github",
		Event:      "push",
		Repository: "https://github.com/org/repo.git",
		Branch:     "main",
		CommitSHA:  "abc123",
		// PR fields are zero values
	}

	data, err := json.Marshal(payload)
	require.NoError(t, err)

	var raw map[string]interface{}
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)

	// pr_url and pr_number have omitempty tags
	_, hasPRURL := raw["pr_url"]
	_, hasPRNumber := raw["pr_number"]
	assert.False(t, hasPRURL, "pr_url should be omitted when empty")
	assert.False(t, hasPRNumber, "pr_number should be omitted when 0")
}

func TestWebhookPayload_WithPRFields(t *testing.T) {
	payload := &WebhookPayload{
		Provider:   "github",
		Event:      "pull_request",
		Repository: "https://github.com/org/repo.git",
		Branch:     "feature/auth",
		CommitSHA:  "def456",
		Author:     "dev",
		Message:    "feat: add auth",
		PRURL:      "https://github.com/org/repo/pull/42",
		PRNumber:   42,
	}

	data, err := json.Marshal(payload)
	require.NoError(t, err)

	var decoded WebhookPayload
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "https://github.com/org/repo/pull/42", decoded.PRURL)
	assert.Equal(t, 42, decoded.PRNumber)
}

// --- EnqueueRequest / EnqueueResponse ---

func TestEnqueueRequest_JSONFieldNames(t *testing.T) {
	req := &EnqueueRequest{
		ReleaseID:   uuid.New(),
		ServiceID:   uuid.New(),
		ServiceName: "test-svc",
		ProjectID:   uuid.New(),
		ProjectSlug: "test-proj",
		GitRepo:     "https://github.com/org/repo.git",
		GitSHA:      "abc123",
		GitBranch:   "main",
		BuildConfig: BuildConfig{Type: "auto"},
		CallbackURL: "https://api.example.com/callback",
		Priority:    3,
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var raw map[string]interface{}
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)

	assert.Contains(t, raw, "release_id")
	assert.Contains(t, raw, "service_id")
	assert.Contains(t, raw, "service_name")
	assert.Contains(t, raw, "project_id")
	assert.Contains(t, raw, "project_slug")
	assert.Contains(t, raw, "git_repo")
	assert.Contains(t, raw, "git_sha")
	assert.Contains(t, raw, "git_branch")
	assert.Contains(t, raw, "build_config")
	assert.Contains(t, raw, "callback_url")
	assert.Contains(t, raw, "priority")
}

func TestEnqueueResponse_JSONRoundtrip(t *testing.T) {
	jobID := uuid.New()
	estimatedStart := time.Now().Add(5 * time.Minute).Truncate(time.Millisecond)

	original := &EnqueueResponse{
		JobID:          jobID,
		Position:       3,
		EstimatedStart: estimatedStart,
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded EnqueueResponse
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, original.JobID, decoded.JobID)
	assert.Equal(t, original.Position, decoded.Position)
	assert.WithinDuration(t, original.EstimatedStart, decoded.EstimatedStart, time.Millisecond)
}

// --- FailedCallback serialization ---

func TestFailedCallback_JSONRoundtrip(t *testing.T) {
	callbackID := uuid.New()
	jobID := uuid.New()
	resultJobID := uuid.New()
	now := time.Now().Truncate(time.Millisecond)
	nextRetry := now.Add(30 * time.Second)

	original := &FailedCallback{
		ID:    callbackID,
		JobID: jobID,
		URL:   "https://api.example.com/v1/callbacks/build-complete",
		Result: &BuildResult{
			JobID:   resultJobID,
			Success: false,
		},
		Attempts:    2,
		MaxAttempts: 5,
		NextRetry:   nextRetry,
		LastError:   "connection refused",
		CreatedAt:   now,
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded FailedCallback
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, original.ID, decoded.ID)
	assert.Equal(t, original.JobID, decoded.JobID)
	assert.Equal(t, original.URL, decoded.URL)
	assert.Equal(t, original.Attempts, decoded.Attempts)
	assert.Equal(t, original.MaxAttempts, decoded.MaxAttempts)
	assert.Equal(t, original.LastError, decoded.LastError)
	assert.WithinDuration(t, original.NextRetry, decoded.NextRetry, time.Millisecond)
	assert.WithinDuration(t, original.CreatedAt, decoded.CreatedAt, time.Millisecond)
	require.NotNil(t, decoded.Result)
	assert.Equal(t, resultJobID, decoded.Result.JobID)
	assert.False(t, decoded.Result.Success)
}

// --- CallbackRetryConfig ---

func TestCallbackRetryConfig_Struct(t *testing.T) {
	cfg := CallbackRetryConfig{
		MaxAttempts:     5,
		InitialInterval: 10 * time.Second,
		MaxInterval:     5 * time.Minute,
		Multiplier:      2.0,
	}

	assert.Equal(t, 5, cfg.MaxAttempts)
	assert.Equal(t, 10*time.Second, cfg.InitialInterval)
	assert.Equal(t, 5*time.Minute, cfg.MaxInterval)
	assert.Equal(t, 2.0, cfg.Multiplier)
}
