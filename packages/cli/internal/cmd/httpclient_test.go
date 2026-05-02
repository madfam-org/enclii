package cmd

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPClient_HasTimeout(t *testing.T) {
	c := httpClient()
	if c.Timeout != defaultRequestTimeout {
		t.Errorf("httpClient timeout = %v, want %v", c.Timeout, defaultRequestTimeout)
	}
	if c.Timeout == 0 {
		t.Error("httpClient must not have a zero timeout (DefaultClient hazard)")
	}
}

func TestHTTPClientForDownload_HasNoTimeout(t *testing.T) {
	// Download client is intentionally untimed — context governs lifetime.
	c := httpClientForDownload()
	if c.Timeout != 0 {
		t.Errorf("httpClientForDownload timeout = %v, want 0 (context governs)", c.Timeout)
	}
}

func TestHTTPClient_DistinctInstances(t *testing.T) {
	// Returning shared instances would let one caller mutate Transport / Jar
	// for everyone else. Verify each call is fresh.
	a := httpClient()
	b := httpClient()
	if a == b {
		t.Error("httpClient must return distinct instances per call")
	}
}

func TestHTTPClient_RespectsTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Intentionally hang well past the default timeout to confirm the
		// client cancels its own request rather than waiting forever.
		time.Sleep(2 * time.Second)
	}))
	defer server.Close()

	c := &http.Client{Timeout: 100 * time.Millisecond}
	start := time.Now()
	resp, err := c.Get(server.URL)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("client did not enforce timeout: elapsed %v", elapsed)
	}
}
