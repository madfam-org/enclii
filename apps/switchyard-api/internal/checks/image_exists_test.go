package checks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newGHCRTestServer returns a server that answers the GHCR versions
// endpoint with the given status code and body. Any other path 404s
// so a wrong-path bug in the client would fail the test loudly.
func newGHCRTestServer(t *testing.T, wantPath string, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Compare against RequestURI so we verify the client actually sent
		// %2F on the wire (net/http decodes r.URL.Path).
		gotPath := r.RequestURI
		if idx := strings.Index(gotPath, "?"); idx >= 0 {
			gotPath = gotPath[:idx]
		}
		if gotPath != wantPath {
			t.Errorf("unexpected path: got %s, want %s", gotPath, wantPath)
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") == "" {
			t.Errorf("missing Authorization header")
		}
		if r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Errorf("missing/wrong Accept header: %q", r.Header.Get("Accept"))
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func TestCheckImageExists_Found(t *testing.T) {
	ts := newGHCRTestServer(t,
		"/orgs/madfam-org/packages/container/enclii%2Fswitchyard-api/versions",
		http.StatusOK,
		`[{"id":1,"name":"sha256:aaa"},{"id":2,"name":"sha256:bbb"}]`,
	)
	defer ts.Close()

	c := &GHCRClient{Token: "dummy", BaseURL: ts.URL}
	res, err := c.CheckImageExists(context.Background(), "madfam-org", "enclii/switchyard-api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Exists {
		t.Fatalf("expected Exists=true")
	}
	if res.VersionCount != 2 {
		t.Fatalf("expected VersionCount=2, got %d", res.VersionCount)
	}
}

func TestCheckImageExists_NotFound_IsBlockerNotError(t *testing.T) {
	// The blocker path — package doesn't exist yet in GHCR. This was the
	// "Avala, Forj, Bloom Scroll, Blueprint Harvester never pushed" mode
	// that motivated the whole change.
	ts := newGHCRTestServer(t,
		"/orgs/madfam-org/packages/container/avala%2Favala-web/versions",
		http.StatusNotFound,
		`{"message":"Not Found"}`,
	)
	defer ts.Close()

	c := &GHCRClient{Token: "dummy", BaseURL: ts.URL}
	res, err := c.CheckImageExists(context.Background(), "madfam-org", "avala/avala-web")
	if err != nil {
		t.Fatalf("404 must not surface as an error, got %v", err)
	}
	if res.Exists {
		t.Fatalf("expected Exists=false for missing package")
	}
	if !strings.Contains(res.Message, "run CI to build and push") {
		t.Fatalf("expected actionable message, got %q", res.Message)
	}
}

func TestCheckImageExists_EmptyVersionsIsBlocker(t *testing.T) {
	// The "package row exists but has no versions" edge case. Shouldn't
	// normally happen, but the GHCR API is eventually consistent and we
	// don't want to let an empty package through.
	ts := newGHCRTestServer(t,
		"/orgs/madfam-org/packages/container/blueprint/versions",
		http.StatusOK,
		`[]`,
	)
	defer ts.Close()

	c := &GHCRClient{Token: "dummy", BaseURL: ts.URL}
	res, err := c.CheckImageExists(context.Background(), "madfam-org", "blueprint")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Exists {
		t.Fatalf("expected Exists=false for empty version list")
	}
}

func TestCheckImageExists_UpstreamErrorSurfacesAsError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer ts.Close()

	c := &GHCRClient{Token: "dummy", BaseURL: ts.URL}
	_, err := c.CheckImageExists(context.Background(), "madfam-org", "anything")
	if err == nil {
		t.Fatalf("expected error on 500")
	}
	if !strings.Contains(err.Error(), "ghcr api 500") {
		t.Fatalf("expected ghcr api 500 in error, got %v", err)
	}
}

func TestCheckImageExists_RequiresArgs(t *testing.T) {
	c := &GHCRClient{Token: "dummy"}
	_, err := c.CheckImageExists(context.Background(), "", "pkg")
	if err == nil {
		t.Fatal("expected error for empty org")
	}
	_, err = c.CheckImageExists(context.Background(), "org", "")
	if err == nil {
		t.Fatal("expected error for empty packageName")
	}
}
