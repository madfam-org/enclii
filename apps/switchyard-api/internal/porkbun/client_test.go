package porkbun

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func decodeJSONBody(t *testing.T, r *http.Request, target any) {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
}

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewClient(Config{
		APIKey:       "test-key",
		SecretAPIKey: "test-secret",
		BaseURL:      server.URL,
	})
}

// The end-to-end reproduction of the 2026-08-21 production failure: this is
// the request `enclii providers porkbun domains --json` makes, answered with
// the payload Porkbun actually returned that day. Before the flexible types
// it failed with "decode porkbun response: json: cannot unmarshal string
// into Go struct field Domain.domains.securityLock of type int".
func TestClient_ListDomains_StringFlagsShape(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/domain/listAll" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(listDomainsStringFixture))
	})

	out, err := client.ListDomains(context.Background())
	if err != nil {
		t.Fatalf("ListDomains returned error: %v", err)
	}
	if len(out.Domains) != 1 {
		t.Fatalf("got %d domains, want 1", len(out.Domains))
	}
	if out.Domains[0].Domain.String() != "kalya.app" {
		t.Errorf("Domain = %q, want kalya.app", out.Domains[0].Domain)
	}
	if out.Domains[0].SecurityLock.Int() != 1 {
		t.Errorf("SecurityLock = %d, want 1", out.Domains[0].SecurityLock)
	}
}

func TestClient_ListDomains_NumericFlagsShape(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(listDomainsNumericFixture))
	})

	out, err := client.ListDomains(context.Background())
	if err != nil {
		t.Fatalf("ListDomains returned error: %v", err)
	}
	if len(out.Domains) != 1 || out.Domains[0].SecurityLock.Int() != 1 {
		t.Fatalf("unexpected decode result: %+v", out.Domains)
	}
}

func TestClient_ListDomains_SurfacesAPIError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ERROR","message":"Invalid API key","code":"403"}`))
	})

	if _, err := client.ListDomains(context.Background()); err == nil {
		t.Fatal("expected an error for an ERROR envelope")
	} else if got := err.Error(); got != "porkbun API error: 403: Invalid API key" {
		t.Errorf("error = %q", got)
	}
}

func TestClient_Configured(t *testing.T) {
	if NewClient(Config{}).Configured() {
		t.Error("empty credentials should not report configured")
	}
	if !NewClient(Config{APIKey: "k", SecretAPIKey: "s"}).Configured() {
		t.Error("populated credentials should report configured")
	}
}

// CreateDNSRecord builds its request body from the (now FlexibleString)
// DNSRecord fields; this pins that the outbound body is unchanged.
func TestClient_CreateDNSRecord_SendsStringBody(t *testing.T) {
	var gotBody map[string]any
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"SUCCESS","id":"456"}`))
	})

	out, err := client.CreateDNSRecord(context.Background(), "kalya.app", DNSRecord{
		Name:    "app",
		Type:    "cname",
		Content: "tunnel.cfargotunnel.com",
		TTL:     "600",
	}, "idem-1")
	if err != nil {
		t.Fatalf("CreateDNSRecord returned error: %v", err)
	}
	if out.ID.String() != "456" {
		t.Errorf("ID = %q, want 456", out.ID)
	}

	if gotBody["type"] != "CNAME" {
		t.Errorf("body type = %v, want CNAME (upper-cased)", gotBody["type"])
	}
	if gotBody["name"] != "app" {
		t.Errorf("body name = %v, want app", gotBody["name"])
	}
	if gotBody["content"] != "tunnel.cfargotunnel.com" {
		t.Errorf("body content = %v", gotBody["content"])
	}
	if gotBody["ttl"] != "600" {
		t.Errorf("body ttl = %v, want string \"600\"", gotBody["ttl"])
	}
	if _, present := gotBody["prio"]; present {
		t.Errorf("empty prio should be omitted, got %v", gotBody["prio"])
	}
}
