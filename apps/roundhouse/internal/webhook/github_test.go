package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"go.uber.org/zap"
)

// computeHMAC generates a valid GitHub webhook HMAC-SHA256 signature.
func computeHMAC(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// --- validateSignature tests ---

func TestValidateSignature(t *testing.T) {
	secret := "test-webhook-secret-2026"
	handler := NewGitHubHandler(secret, zap.NewNop())

	tests := []struct {
		name      string
		body      []byte
		signature string
		want      bool
	}{
		{
			name:      "valid_signature",
			body:      []byte(`{"action":"push"}`),
			signature: computeHMAC([]byte(`{"action":"push"}`), secret),
			want:      true,
		},
		{
			name:      "invalid_signature",
			body:      []byte(`{"action":"push"}`),
			signature: "sha256=0000000000000000000000000000000000000000000000000000000000000000",
			want:      false,
		},
		{
			name:      "wrong_secret",
			body:      []byte(`{"action":"push"}`),
			signature: computeHMAC([]byte(`{"action":"push"}`), "wrong-secret"),
			want:      false,
		},
		{
			name:      "empty_signature",
			body:      []byte(`{"action":"push"}`),
			signature: "",
			want:      false,
		},
		{
			name:      "malformed_signature_no_prefix",
			body:      []byte(`{"action":"push"}`),
			signature: "not-a-valid-sig",
			want:      false,
		},
		{
			name:      "empty_body_valid_sig",
			body:      []byte{},
			signature: computeHMAC([]byte{}, secret),
			want:      true,
		},
		{
			name:      "tampered_body",
			body:      []byte(`{"action":"push","tampered":true}`),
			signature: computeHMAC([]byte(`{"action":"push"}`), secret),
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := handler.validateSignature(tt.body, tt.signature)
			if got != tt.want {
				t.Errorf("validateSignature() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- handlePush tests ---

func TestHandlePush_ValidPayload(t *testing.T) {
	handler := NewGitHubHandler("secret", zap.NewNop())

	payload := GitHubPushPayload{
		Ref:    "refs/heads/main",
		Before: "aaa1111111111111111111111111111111111111",
		After:  "bbb2222222222222222222222222222222222222",
	}
	payload.Repository.CloneURL = "https://github.com/test/repo.git"
	payload.Repository.FullName = "test/repo"
	payload.Pusher.Name = "testuser"
	payload.Pusher.Email = "test@example.com"
	payload.HeadCommit.ID = "bbb2222222222222222222222222222222222222"
	payload.HeadCommit.Message = "feat: add new feature"

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	result, err := handler.handlePush(body)
	if err != nil {
		t.Fatalf("handlePush() error: %v", err)
	}

	if result.Provider != "github" {
		t.Errorf("Provider: got %q, want %q", result.Provider, "github")
	}
	if result.Event != "push" {
		t.Errorf("Event: got %q, want %q", result.Event, "push")
	}
	if result.Branch != "main" {
		t.Errorf("Branch: got %q, want %q", result.Branch, "main")
	}
	if result.CommitSHA != "bbb2222222222222222222222222222222222222" {
		t.Errorf("CommitSHA: got %q, want full SHA", result.CommitSHA)
	}
	if result.Repository != "https://github.com/test/repo.git" {
		t.Errorf("Repository: got %q, want clone URL", result.Repository)
	}
	if result.Author != "testuser" {
		t.Errorf("Author: got %q, want %q", result.Author, "testuser")
	}
	if result.Message != "feat: add new feature" {
		t.Errorf("Message: got %q, want commit message", result.Message)
	}
}

func TestHandlePush_BranchExtraction(t *testing.T) {
	handler := NewGitHubHandler("secret", zap.NewNop())

	tests := []struct {
		name       string
		ref        string
		wantBranch string
	}{
		{"main_branch", "refs/heads/main", "main"},
		{"feature_branch", "refs/heads/feature/auth", "feature/auth"},
		{"develop_branch", "refs/heads/develop", "develop"},
		{"nested_branch", "refs/heads/fix/bug/critical", "fix/bug/critical"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := GitHubPushPayload{
				Ref:   tt.ref,
				After: "abc1234567890123456789012345678901234567",
			}
			payload.Repository.CloneURL = "https://github.com/test/repo.git"
			payload.Pusher.Name = "testuser"

			body, _ := json.Marshal(payload)
			result, err := handler.handlePush(body)
			if err != nil {
				t.Fatalf("handlePush() error: %v", err)
			}
			if result.Branch != tt.wantBranch {
				t.Errorf("Branch: got %q, want %q", result.Branch, tt.wantBranch)
			}
		})
	}
}

func TestHandlePush_BranchDeletion(t *testing.T) {
	handler := NewGitHubHandler("secret", zap.NewNop())

	payload := GitHubPushPayload{
		Ref:   "refs/heads/feature/old",
		After: "0000000000000000000000000000000000000000",
	}
	payload.Repository.CloneURL = "https://github.com/test/repo.git"
	payload.Pusher.Name = "testuser"

	body, _ := json.Marshal(payload)
	_, err := handler.handlePush(body)
	if err == nil {
		t.Error("expected error for branch deletion, got nil")
	}
}

func TestHandlePush_InvalidJSON(t *testing.T) {
	handler := NewGitHubHandler("secret", zap.NewNop())

	_, err := handler.handlePush([]byte(`{invalid json`))
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

// --- handlePullRequest tests ---

func TestHandlePullRequest_Opened(t *testing.T) {
	handler := NewGitHubHandler("secret", zap.NewNop())

	payload := GitHubPRPayload{
		Action: "opened",
		Number: 42,
	}
	payload.PullRequest.Number = 42
	payload.PullRequest.Title = "feat: add authentication"
	payload.PullRequest.Head.Ref = "feature/auth"
	payload.PullRequest.Head.SHA = "abc1234567890123456789012345678901234567"
	payload.PullRequest.Base.Ref = "main"
	payload.PullRequest.HTMLURL = "https://github.com/test/repo/pull/42"
	payload.PullRequest.User.Login = "developer1"
	payload.Repository.FullName = "test/repo"
	payload.Repository.CloneURL = "https://github.com/test/repo.git"

	body, _ := json.Marshal(payload)
	result, err := handler.handlePullRequest(body)
	if err != nil {
		t.Fatalf("handlePullRequest() error: %v", err)
	}

	if result.Provider != "github" {
		t.Errorf("Provider: got %q, want %q", result.Provider, "github")
	}
	if result.Event != "pull_request" {
		t.Errorf("Event: got %q, want %q", result.Event, "pull_request")
	}
	if result.Branch != "feature/auth" {
		t.Errorf("Branch: got %q, want %q", result.Branch, "feature/auth")
	}
	if result.CommitSHA != "abc1234567890123456789012345678901234567" {
		t.Errorf("CommitSHA: got %q, want full SHA", result.CommitSHA)
	}
	if result.Author != "developer1" {
		t.Errorf("Author: got %q, want %q", result.Author, "developer1")
	}
	if result.Message != "feat: add authentication" {
		t.Errorf("Message: got %q, want PR title", result.Message)
	}
	if result.PRURL != "https://github.com/test/repo/pull/42" {
		t.Errorf("PRURL: got %q, want PR URL", result.PRURL)
	}
	if result.PRNumber != 42 {
		t.Errorf("PRNumber: got %d, want %d", result.PRNumber, 42)
	}
}

func TestHandlePullRequest_BuildActions(t *testing.T) {
	handler := NewGitHubHandler("secret", zap.NewNop())

	buildActions := []string{"opened", "synchronize", "reopened"}

	for _, action := range buildActions {
		t.Run(action, func(t *testing.T) {
			payload := GitHubPRPayload{
				Action: action,
			}
			payload.PullRequest.Head.Ref = "feature/test"
			payload.PullRequest.Head.SHA = "abc1234567890123456789012345678901234567"
			payload.PullRequest.User.Login = "dev"
			payload.PullRequest.Title = "test"
			payload.Repository.CloneURL = "https://github.com/test/repo.git"

			body, _ := json.Marshal(payload)
			result, err := handler.handlePullRequest(body)
			if err != nil {
				t.Fatalf("handlePullRequest() error for action %q: %v", action, err)
			}
			if result.Event != "pull_request" {
				t.Errorf("Event: got %q, want %q", result.Event, "pull_request")
			}
			if result.CommitSHA == "" {
				t.Error("expected CommitSHA for build action, got empty")
			}
		})
	}
}

func TestHandlePullRequest_Closed(t *testing.T) {
	handler := NewGitHubHandler("secret", zap.NewNop())

	payload := GitHubPRPayload{
		Action: "closed",
	}
	payload.PullRequest.Head.Ref = "feature/done"
	payload.PullRequest.Head.SHA = "abc1234567890123456789012345678901234567"
	payload.PullRequest.User.Login = "dev"
	payload.PullRequest.Title = "completed feature"
	payload.Repository.CloneURL = "https://github.com/test/repo.git"

	body, _ := json.Marshal(payload)
	result, err := handler.handlePullRequest(body)
	if err != nil {
		t.Fatalf("handlePullRequest() error: %v", err)
	}

	if result.Event != "pull_request_closed" {
		t.Errorf("Event: got %q, want %q", result.Event, "pull_request_closed")
	}

	if result.CommitSHA != "" {
		t.Errorf("CommitSHA should be empty for closed events, got %q", result.CommitSHA)
	}
}

func TestHandlePullRequest_IgnoredActions(t *testing.T) {
	handler := NewGitHubHandler("secret", zap.NewNop())

	ignored := []string{"labeled", "unlabeled", "assigned", "review_requested", "edited"}

	for _, action := range ignored {
		t.Run(action, func(t *testing.T) {
			payload := GitHubPRPayload{
				Action: action,
			}
			payload.PullRequest.Head.Ref = "feature/test"
			payload.PullRequest.User.Login = "dev"
			payload.Repository.CloneURL = "https://github.com/test/repo.git"

			body, _ := json.Marshal(payload)
			_, err := handler.handlePullRequest(body)
			if err == nil {
				t.Errorf("expected error for ignored action %q, got nil", action)
			}
		})
	}
}

func TestHandlePullRequest_InvalidJSON(t *testing.T) {
	handler := NewGitHubHandler("secret", zap.NewNop())

	_, err := handler.handlePullRequest([]byte(`not-json`))
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

// --- NewGitHubHandler tests ---

func TestNewGitHubHandler(t *testing.T) {
	logger := zap.NewNop()
	handler := NewGitHubHandler("my-secret", logger)

	if handler == nil {
		t.Fatal("expected handler to be created")
	}
	if handler.secret != "my-secret" {
		t.Errorf("secret: got %q, want %q", handler.secret, "my-secret")
	}
	if handler.previewsEnabled {
		t.Error("previewsEnabled should default to false")
	}
	if handler.switchyardClient != nil {
		t.Error("switchyardClient should be nil with basic constructor")
	}
}

func TestNewGitHubHandlerWithConfig(t *testing.T) {
	logger := zap.NewNop()

	tests := []struct {
		name          string
		cfg           *GitHubHandlerConfig
		wantPreviews  bool
		wantHasClient bool
	}{
		{
			name: "previews_disabled",
			cfg: &GitHubHandlerConfig{
				Secret:          "secret",
				SwitchyardURL:   "http://switchyard:8080",
				PreviewsEnabled: false,
			},
			wantPreviews:  false,
			wantHasClient: false,
		},
		{
			name: "previews_enabled_with_url",
			cfg: &GitHubHandlerConfig{
				Secret:          "secret",
				SwitchyardURL:   "http://switchyard:8080",
				PreviewsEnabled: true,
			},
			wantPreviews:  true,
			wantHasClient: true,
		},
		{
			name: "previews_enabled_without_url",
			cfg: &GitHubHandlerConfig{
				Secret:          "secret",
				SwitchyardURL:   "",
				PreviewsEnabled: true,
			},
			wantPreviews:  true,
			wantHasClient: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewGitHubHandlerWithConfig(tt.cfg, logger)
			if handler.previewsEnabled != tt.wantPreviews {
				t.Errorf("previewsEnabled: got %v, want %v", handler.previewsEnabled, tt.wantPreviews)
			}
			hasClient := handler.switchyardClient != nil
			if hasClient != tt.wantHasClient {
				t.Errorf("hasClient: got %v, want %v", hasClient, tt.wantHasClient)
			}
		})
	}
}

// --- Payload struct tests ---

func TestGitHubPushPayload_JSONRoundtrip(t *testing.T) {
	raw := `{
		"ref": "refs/heads/main",
		"before": "aaa1111111111111111111111111111111111111",
		"after": "bbb2222222222222222222222222222222222222",
		"repository": {
			"id": 12345,
			"name": "my-repo",
			"full_name": "org/my-repo",
			"clone_url": "https://github.com/org/my-repo.git",
			"private": true
		},
		"pusher": {"name": "alice", "email": "alice@example.com"},
		"head_commit": {
			"id": "bbb2222222222222222222222222222222222222",
			"message": "fix: resolve auth bug",
			"timestamp": "2026-03-01T12:00:00Z",
			"author": {"name": "alice", "email": "alice@example.com"}
		},
		"commits": [
			{"id": "bbb222", "message": "fix: resolve auth bug"}
		]
	}`

	var payload GitHubPushPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if payload.Ref != "refs/heads/main" {
		t.Errorf("Ref: got %q, want %q", payload.Ref, "refs/heads/main")
	}
	if payload.Repository.ID != 12345 {
		t.Errorf("Repository.ID: got %d, want %d", payload.Repository.ID, 12345)
	}
	if payload.Repository.Private != true {
		t.Error("expected Repository.Private to be true")
	}
	if len(payload.Commits) != 1 {
		t.Errorf("Commits length: got %d, want %d", len(payload.Commits), 1)
	}
}

func TestGitHubPRPayload_JSONRoundtrip(t *testing.T) {
	raw := `{
		"action": "opened",
		"number": 99,
		"pull_request": {
			"id": 5678,
			"number": 99,
			"state": "open",
			"title": "feat: new feature",
			"head": {"ref": "feature/new", "sha": "deadbeef12345678"},
			"base": {"ref": "main"},
			"html_url": "https://github.com/org/repo/pull/99",
			"merged": false,
			"user": {"login": "bob"}
		},
		"repository": {
			"full_name": "org/repo",
			"clone_url": "https://github.com/org/repo.git"
		},
		"sender": {"login": "bob"}
	}`

	var payload GitHubPRPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if payload.Action != "opened" {
		t.Errorf("Action: got %q, want %q", payload.Action, "opened")
	}
	if payload.PullRequest.Number != 99 {
		t.Errorf("PullRequest.Number: got %d, want %d", payload.PullRequest.Number, 99)
	}
	if payload.PullRequest.Head.SHA != "deadbeef12345678" {
		t.Errorf("PullRequest.Head.SHA: got %q, want %q", payload.PullRequest.Head.SHA, "deadbeef12345678")
	}
	if payload.PullRequest.Merged {
		t.Error("expected PullRequest.Merged to be false")
	}
}
