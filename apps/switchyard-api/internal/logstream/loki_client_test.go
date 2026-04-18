package logstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestQueryRange_HappyPath asserts the client sends the expected URL
// params and decodes Loki's standard streams response into Entry slice
// sorted ascending by timestamp.
func TestQueryRange_HappyPath(t *testing.T) {
	var gotQuery, gotStart, gotEnd, gotLimit, gotDirection string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/loki/api/v1/query_range" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		gotQuery = r.URL.Query().Get("query")
		gotStart = r.URL.Query().Get("start")
		gotEnd = r.URL.Query().Get("end")
		gotLimit = r.URL.Query().Get("limit")
		gotDirection = r.URL.Query().Get("direction")

		// Two streams with interleaved timestamps to exercise the sort.
		resp := map[string]any{
			"status": "success",
			"data": map[string]any{
				"resultType": "streams",
				"result": []map[string]any{
					{
						"stream": map[string]string{"pod": "svc-0", "level": "info"},
						"values": [][]string{
							{"1700000002000000000", "second line"},
							{"1700000000000000000", "first line"},
						},
					},
					{
						"stream": map[string]string{"pod": "svc-1"},
						"values": [][]string{
							{"1700000001000000000", "middle line"},
						},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewLokiClient(srv.URL)
	start := time.Unix(0, 1_700_000_000_000_000_000)
	end := time.Unix(0, 1_700_000_003_000_000_000)

	entries, err := c.QueryRange(context.Background(), `{ns="x"}`, start, end, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("want 3 entries, got %d", len(entries))
	}
	// Must be in ascending timestamp order.
	for i := 1; i < len(entries); i++ {
		if entries[i].Timestamp.Before(entries[i-1].Timestamp) {
			t.Errorf("entries not sorted asc at index %d", i)
		}
	}
	// URL-param assertions.
	if gotQuery != `{ns="x"}` {
		t.Errorf("wrong query: %s", gotQuery)
	}
	if gotDirection != "backward" {
		t.Errorf("wrong direction: %s", gotDirection)
	}
	if gotLimit != "500" {
		t.Errorf("wrong limit: %s", gotLimit)
	}
	if gotStart == "" || gotEnd == "" {
		t.Error("start/end should be set")
	}
}

// When Loki returns 5xx, client maps to ErrLokiUnavailable so the
// handler can emit 503 rather than 500.
func TestQueryRange_5xxMapsToUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	c := NewLokiClient(srv.URL)
	_, err := c.QueryRange(context.Background(), "{ns=\"x\"}", time.Now().Add(-time.Hour), time.Now(), 100)
	if err == nil || !errors.Is(err, ErrLokiUnavailable) {
		t.Fatalf("want ErrLokiUnavailable, got %v", err)
	}
}

// 4xx is a programmer error (bad LogQL), not infra — shouldn't map to
// ErrLokiUnavailable so the handler returns 500 instead of misleading
// the client with a 503.
func TestQueryRange_4xxNotUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`parse error: invalid logql`))
	}))
	defer srv.Close()

	c := NewLokiClient(srv.URL)
	_, err := c.QueryRange(context.Background(), "bad", time.Now().Add(-time.Hour), time.Now(), 100)
	if err == nil {
		t.Fatal("want error")
	}
	if errors.Is(err, ErrLokiUnavailable) {
		t.Errorf("4xx should NOT map to Unavailable, got %v", err)
	}
	if !strings.Contains(err.Error(), "4xx") {
		t.Errorf("error should mention 4xx, got %v", err)
	}
}

// Network-level failure (dial failure) maps to ErrLokiUnavailable.
func TestQueryRange_DialFailureMapsToUnavailable(t *testing.T) {
	c := NewLokiClient("http://127.0.0.1:1") // reserved port, will refuse
	_, err := c.QueryRange(context.Background(), "{}", time.Now().Add(-time.Hour), time.Now(), 100)
	if err == nil || !errors.Is(err, ErrLokiUnavailable) {
		t.Fatalf("want ErrLokiUnavailable, got %v", err)
	}
}

// Empty baseURL is a configuration error; we surface it via the same
// Unavailable sentinel so the UI shows the same state.
func TestQueryRange_EmptyBaseURL(t *testing.T) {
	c := NewLokiClient("")
	_, err := c.QueryRange(context.Background(), "{}", time.Now().Add(-time.Hour), time.Now(), 100)
	if !errors.Is(err, ErrLokiUnavailable) {
		t.Fatalf("want ErrLokiUnavailable, got %v", err)
	}
}

// Malformed responses (invalid JSON) should error but not panic.
func TestQueryRange_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "not json")
	}))
	defer srv.Close()
	c := NewLokiClient(srv.URL)
	_, err := c.QueryRange(context.Background(), "{}", time.Now().Add(-time.Hour), time.Now(), 10)
	if err == nil {
		t.Fatal("want error on malformed json")
	}
}

// Entries with bad timestamp strings are silently skipped — not every
// Loki deployment's clock is well-behaved, and a few dropped lines
// beats killing the whole query.
func TestQueryRange_SkipsBadTimestamps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{
				"resultType": "streams",
				"result": []map[string]any{
					{
						"stream": map[string]string{"pod": "x"},
						"values": [][]string{
							{"not-a-number", "bad"},
							{"1700000000000000000", "good"},
						},
					},
				},
			},
		})
	}))
	defer srv.Close()
	c := NewLokiClient(srv.URL)
	entries, err := c.QueryRange(context.Background(), "{}", time.Unix(0, 0), time.Now(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Message != "good" {
		t.Errorf("want just the good entry, got %#v", entries)
	}
}
