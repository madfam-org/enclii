package signup

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHTTPJanuaClient_BuildGithubAuthorizeURL_FailsFastWithoutAdminToken
// asserts the GitHub-link step refuses to make an unauthenticated call to
// Janua's admin-only on-behalf endpoint when no admin token is configured —
// it must fail loudly with ErrAdminTokenMissing instead of dead-ending on a
// silent 401 from Janua. config.Load() should never let this path be
// reached with SignupEnabled=true in practice; this is defense-in-depth.
func TestHTTPJanuaClient_BuildGithubAuthorizeURL_FailsFastWithoutAdminToken(t *testing.T) {
	var hit bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	c := NewHTTPJanuaClient(server.URL, "" /* no admin token */)
	_, err := c.BuildGithubAuthorizeURL(context.Background(), "janua-sub-1", "state", "https://api.enclii.dev/callback")

	if err == nil {
		t.Fatal("expected error when admin token is empty")
	}
	if !errors.Is(err, ErrAdminTokenMissing) {
		t.Fatalf("expected ErrAdminTokenMissing, got: %v", err)
	}
	if hit {
		t.Fatal("must not make an unauthenticated HTTP call to Janua when the admin token is missing")
	}
}

// TestHTTPJanuaClient_CompleteGithubOAuth_FailsFastWithoutAdminToken mirrors
// the above for the OAuth completion call.
func TestHTTPJanuaClient_CompleteGithubOAuth_FailsFastWithoutAdminToken(t *testing.T) {
	var hit bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	c := NewHTTPJanuaClient(server.URL, "")
	_, _, err := c.CompleteGithubOAuth(context.Background(), "janua-sub-1", "oauth-code")

	if err == nil {
		t.Fatal("expected error when admin token is empty")
	}
	if !errors.Is(err, ErrAdminTokenMissing) {
		t.Fatalf("expected ErrAdminTokenMissing, got: %v", err)
	}
	if hit {
		t.Fatal("must not make an unauthenticated HTTP call to Janua when the admin token is missing")
	}
}

// TestHTTPJanuaClient_BuildGithubAuthorizeURL_SendsBearerWithAdminToken
// asserts the happy path still authenticates correctly once a token is set.
func TestHTTPJanuaClient_BuildGithubAuthorizeURL_SendsBearerWithAdminToken(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"authorization_url":"https://github.com/login/oauth/authorize?client_id=test"}`))
	}))
	defer server.Close()

	c := NewHTTPJanuaClient(server.URL, "test-admin-token")
	url, err := c.BuildGithubAuthorizeURL(context.Background(), "janua-sub-1", "state", "https://api.enclii.dev/callback")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer test-admin-token" {
		t.Fatalf("expected Bearer test-admin-token, got %q", gotAuth)
	}
	if url == "" {
		t.Fatal("expected a non-empty authorization URL")
	}
}
