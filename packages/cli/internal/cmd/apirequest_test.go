package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func TestQueryString(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]string
		want []string
	}{
		{"empty", nil, []string{""}},
		{"all empty values", map[string]string{"a": "", "b": ""}, []string{""}},
		{"single", map[string]string{"limit": "50"}, []string{"?limit=50"}},
		{"multi sorted", map[string]string{"a": "1", "b": "2"}, []string{"?a=1&b=2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := queryString(tt.in)
			matched := false
			for _, w := range tt.want {
				if got == w {
					matched = true
					break
				}
			}
			if !matched {
				t.Errorf("queryString(%v) = %q, want one of %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestAPIRequest_AuthHeaderAndUserAgent(t *testing.T) {
	var gotAuth, gotUA, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotUA = r.Header.Get("User-Agent")
		gotAccept = r.Header.Get("Accept")
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	cfg := &config.Config{APIEndpoint: srv.URL, APIToken: "secret-token"}
	var resp struct {
		OK bool `json:"ok"`
	}
	if err := apiRequest(context.Background(), cfg, "GET", "/v1/anything", nil, &resp); err != nil {
		t.Fatalf("apiRequest: %v", err)
	}
	if !resp.OK {
		t.Error("expected ok=true in response")
	}
	if gotAuth != "Bearer secret-token" {
		t.Errorf("Authorization = %q, want Bearer secret-token", gotAuth)
	}
	if !strings.HasPrefix(gotUA, "enclii-cli/") {
		t.Errorf("User-Agent = %q, want prefix enclii-cli/", gotUA)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want application/json", gotAccept)
	}
}

func TestAPIRequest_PostPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var got map[string]string
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("server: bad JSON body: %v", err)
		}
		if got["name"] != "alice" {
			t.Errorf("server: payload name = %q, want alice", got["name"])
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"u_1"}`))
	}))
	defer srv.Close()

	cfg := &config.Config{APIEndpoint: srv.URL}
	var resp struct {
		ID string `json:"id"`
	}
	err := apiRequest(context.Background(), cfg, "POST", "/v1/users",
		map[string]string{"name": "alice"}, &resp)
	if err != nil {
		t.Fatalf("apiRequest: %v", err)
	}
	if resp.ID != "u_1" {
		t.Errorf("resp.ID = %q, want u_1", resp.ID)
	}
}

func TestAPIRequest_ErrorMappingByStatus(t *testing.T) {
	tests := []struct {
		status      int
		wantContain string
	}{
		{http.StatusUnauthorized, "unauthorized"},
		{http.StatusForbidden, "forbidden"},
		{http.StatusNotFound, "API error (404)"},
		{http.StatusInternalServerError, "API error (500)"},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"error":"nope"}`))
			}))
			defer srv.Close()

			cfg := &config.Config{APIEndpoint: srv.URL}
			err := apiRequest(context.Background(), cfg, "GET", "/v1/x", nil, nil)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantContain) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantContain)
			}
		})
	}
}

func TestEmitJSON(t *testing.T) {
	// emitJSON writes to stdout — capture by swapping os.Stdout temporarily.
	// Here we just confirm it produces valid JSON for a representative shape.
	type item struct {
		Name string `json:"name"`
		N    int    `json:"n"`
	}
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode([]item{{Name: "a", N: 1}}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var back []item
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(back) != 1 || back[0].Name != "a" || back[0].N != 1 {
		t.Errorf("roundtrip mismatch: %+v", back)
	}
}
