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

// Reads must be POST with credentials in the JSON body — NOT a GET with
// X-API-Key headers. Over GET, Porkbun returns a misleading INVALID_DOMAIN for a
// domain the account owns (observed live against getNs for ctm.ac), which is the
// 2026-08-24 production failure this fix closes. Pin the method + body-auth
// contract for every read so a refactor cannot silently revert it.
func TestClient_Reads_UsePostWithBodyCredentials(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(c *Client) error
		path string
	}{
		{"ListDomains", func(c *Client) error { _, e := c.ListDomains(context.Background()); return e }, "/domain/listAll"},
		{"GetNameservers", func(c *Client) error { _, e := c.GetNameservers(context.Background(), "ctm.ac"); return e }, "/domain/getNs/ctm.ac"},
		{"GetDomain", func(c *Client) error { _, e := c.GetDomain(context.Background(), "ctm.ac"); return e }, "/domain/get/ctm.ac"},
		{"ListDNSRecords", func(c *Client) error { _, e := c.ListDNSRecords(context.Background(), "ctm.ac"); return e }, "/dns/retrieve/ctm.ac"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotMethod, gotPath string
			var gotBody map[string]any
			var gotHeaderAuth bool
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotPath = r.URL.Path
				if r.Header.Get("X-API-Key") != "" || r.Header.Get("X-Secret-API-Key") != "" {
					gotHeaderAuth = true
				}
				decodeJSONBody(t, r, &gotBody)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"status":"SUCCESS","ns":["ns1.example"],"domains":[],"records":[],"domain":{}}`))
			})

			if err := tc.call(client); err != nil {
				t.Fatalf("%s call failed: %v", tc.name, err)
			}
			if gotMethod != http.MethodPost {
				t.Errorf("%s used %s, want POST (Porkbun reads are POST+body)", tc.name, gotMethod)
			}
			if gotPath != tc.path {
				t.Errorf("%s hit %s, want %s", tc.name, gotPath, tc.path)
			}
			if gotBody["apikey"] != "test-key" || gotBody["secretapikey"] != "test-secret" {
				t.Errorf("%s missing body credentials: apikey=%v secretapikey=%v", tc.name, gotBody["apikey"], gotBody["secretapikey"])
			}
			if gotHeaderAuth {
				t.Errorf("%s sent X-API-Key headers; Porkbun ignores them and the GET+header path is the bug being fixed", tc.name)
			}
		})
	}
}
