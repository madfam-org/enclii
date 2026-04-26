package sentry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeEnv builds an os.Getenv-shaped function from a map, so NewClientFromEnv
// can be exercised without mutating real process env (which would race with
// other parallel tests).
func fakeEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestIsConfigured_BothSet(t *testing.T) {
	c := NewClientFromEnv(fakeEnv(map[string]string{
		"SENTRY_AUTH_TOKEN": "tok",
		"SENTRY_ORG_SLUG":   "innovaciones-madfam-sas-de-cv",
	}))
	if !c.IsConfigured() {
		t.Fatalf("expected IsConfigured()=true, got false")
	}
}

func TestIsConfigured_TokenMissing(t *testing.T) {
	c := NewClientFromEnv(fakeEnv(map[string]string{
		"SENTRY_ORG_SLUG": "innovaciones-madfam-sas-de-cv",
	}))
	if c.IsConfigured() {
		t.Fatalf("expected IsConfigured()=false when token unset")
	}
}

func TestIsConfigured_OrgMissing(t *testing.T) {
	c := NewClientFromEnv(fakeEnv(map[string]string{
		"SENTRY_AUTH_TOKEN": "tok",
	}))
	if c.IsConfigured() {
		t.Fatalf("expected IsConfigured()=false when org slug unset")
	}
}

func TestIsConfigured_NilReceiver(t *testing.T) {
	var c *Client // nil
	if c.IsConfigured() {
		t.Fatalf("nil client must not be configured")
	}
}

// TestGetProjectIssueCount_HappyPath verifies the slug + bearer wiring and
// that we sum the [ts, count] tuples correctly.
func TestGetProjectIssueCount_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Auth header check — proves we send the bearer token.
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("expected Authorization=Bearer tok, got %q", got)
		}
		// Path must include both org and project slug.
		if !strings.Contains(r.URL.Path, "/api/0/projects/innovaciones-madfam-sas-de-cv/switchyard-api/stats/") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		// Should request stat=received.
		if got := r.URL.Query().Get("stat"); got != "received" {
			t.Errorf("expected stat=received, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[[1714032000,4],[1714035600,7],[1714039200,1]]`))
	}))
	defer srv.Close()

	c := NewClientWithConfig(srv.URL, "innovaciones-madfam-sas-de-cv", "tok", srv.Client())
	count, err := c.GetProjectIssueCount(context.Background(), "switchyard-api", "24h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 12 {
		t.Errorf("expected count=12 (4+7+1), got %d", count)
	}
}

func TestGetProjectIssueCount_DefaultsToTwentyFourHoursWhenEmpty(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		// since/until should be ~24h apart.
		since := r.URL.Query().Get("since")
		until := r.URL.Query().Get("until")
		if since == "" || until == "" {
			t.Errorf("missing since/until: since=%q until=%q", since, until)
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := NewClientWithConfig(srv.URL, "org", "tok", srv.Client())
	if _, err := c.GetProjectIssueCount(context.Background(), "any", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatalf("expected the server to be called even with empty statsPeriod")
	}
}

func TestGetProjectIssueCount_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":"invalid token"}`))
	}))
	defer srv.Close()

	c := NewClientWithConfig(srv.URL, "org", "bad-tok", srv.Client())
	_, err := c.GetProjectIssueCount(context.Background(), "x", "24h")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

func TestGetProjectIssueCount_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClientWithConfig(srv.URL, "org", "tok", srv.Client())
	_, err := c.GetProjectIssueCount(context.Background(), "no-such-project", "24h")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetProjectIssueCount_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewClientWithConfig(srv.URL, "org", "tok", srv.Client())
	_, err := c.GetProjectIssueCount(context.Background(), "x", "24h")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
}

func TestGetProjectIssueCount_Unconfigured(t *testing.T) {
	c := NewClientFromEnv(fakeEnv(map[string]string{}))
	_, err := c.GetProjectIssueCount(context.Background(), "x", "24h")
	if !errors.Is(err, ErrUnconfigured) {
		t.Fatalf("expected ErrUnconfigured, got %v", err)
	}
}

func TestGetProjectIssueCount_RequiresProjectSlug(t *testing.T) {
	c := NewClientWithConfig("https://sentry.io", "org", "tok", nil)
	_, err := c.GetProjectIssueCount(context.Background(), "", "24h")
	if err == nil {
		t.Fatalf("expected error when projectSlug is empty")
	}
}

func TestGetProjectErrorRate_ZeroReceived(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := NewClientWithConfig(srv.URL, "org", "tok", srv.Client())
	rate, err := c.GetProjectErrorRate(context.Background(), "x", "24h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rate != 0 {
		t.Errorf("expected rate=0 with no traffic, got %v", rate)
	}
}

func TestGetProjectErrorRate_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stat := r.URL.Query().Get("stat")
		switch stat {
		case "received":
			_, _ = w.Write([]byte(`[[1,100]]`))
		case "rejected":
			_, _ = w.Write([]byte(`[[1,5]]`))
		default:
			t.Errorf("unexpected stat %q", stat)
		}
	}))
	defer srv.Close()

	c := NewClientWithConfig(srv.URL, "org", "tok", srv.Client())
	rate, err := c.GetProjectErrorRate(context.Background(), "x", "24h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rate < 0.04 || rate > 0.06 {
		t.Errorf("expected rate ~0.05, got %v", rate)
	}
}

func TestParseStatsPeriod(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"", 24 * time.Hour, false},
		{"1h", time.Hour, false},
		{"24h", 24 * time.Hour, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"30m", 30 * time.Minute, false},
		{"abc", 0, true},
		{"0h", 0, true},
		{"-1h", 0, true},
		{"5x", 0, true},
	}
	for _, tc := range cases {
		got, err := parseStatsPeriod(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseStatsPeriod(%q): expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseStatsPeriod(%q): unexpected error %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseStatsPeriod(%q): got %v want %v", tc.in, got, tc.want)
		}
	}
}

func TestOrgSlug(t *testing.T) {
	c := NewClientWithConfig("", "innovaciones-madfam-sas-de-cv", "tok", nil)
	if got := c.OrgSlug(); got != "innovaciones-madfam-sas-de-cv" {
		t.Errorf("OrgSlug(): got %q want innovaciones-madfam-sas-de-cv", got)
	}
	var nilC *Client
	if got := nilC.OrgSlug(); got != "" {
		t.Errorf("nil client OrgSlug(): got %q want empty", got)
	}
}
