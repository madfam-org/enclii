package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestClient creates a Client pointed at a test server.
func newTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	return &Client{
		httpClient: server.Client(),
		baseURL:    server.URL,
		apiToken:   "test-token",
		accountID:  "test-account",
		zoneID:     "test-zone-id",
		tunnelID:   "test-tunnel-id",
	}
}

func TestCreateDNSRecord(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
		checkFn func(t *testing.T, rec *DNSRecord)
	}{
		{
			name: "successful creation",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify method and path
				if r.Method != http.MethodPost {
					t.Errorf("method = %s, want POST", r.Method)
				}
				if !strings.Contains(r.URL.Path, "/zones/test-zone-id/dns_records") {
					t.Errorf("path = %s, want /zones/test-zone-id/dns_records", r.URL.Path)
				}

				// Verify request body
				body, _ := io.ReadAll(r.Body)
				var payload struct {
					Type    string `json:"type"`
					Name    string `json:"name"`
					Content string `json:"content"`
					Proxied bool   `json:"proxied"`
					TTL     int    `json:"ttl"`
				}
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Fatalf("failed to parse request body: %v", err)
				}
				if payload.Type != "CNAME" {
					t.Errorf("type = %q, want CNAME", payload.Type)
				}
				if payload.Name != "api.example.com" {
					t.Errorf("name = %q, want api.example.com", payload.Name)
				}
				if payload.Content != "tunnel.enclii.dev" {
					t.Errorf("content = %q, want tunnel.enclii.dev", payload.Content)
				}
				if !payload.Proxied {
					t.Error("proxied = false, want true")
				}
				if payload.TTL != 1 {
					t.Errorf("ttl = %d, want 1", payload.TTL)
				}

				resp := APIResponse[DNSRecord]{
					Success: true,
					Result: DNSRecord{
						ID:      "rec-123",
						ZoneID:  "test-zone-id",
						Name:    "api.example.com",
						Type:    "CNAME",
						Content: "tunnel.enclii.dev",
						Proxied: true,
						TTL:     1,
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			}),
			wantErr: false,
			checkFn: func(t *testing.T, rec *DNSRecord) {
				if rec.ID != "rec-123" {
					t.Errorf("ID = %q, want rec-123", rec.ID)
				}
				if rec.Name != "api.example.com" {
					t.Errorf("Name = %q, want api.example.com", rec.Name)
				}
				if rec.Content != "tunnel.enclii.dev" {
					t.Errorf("Content = %q, want tunnel.enclii.dev", rec.Content)
				}
			},
		},
		{
			name: "API error response",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				resp := APIResponse[DNSRecord]{
					Success: false,
					Errors:  []APIError{{Code: 1004, Message: "DNS Validation Error"}},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			}),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			rec, err := client.CreateDNSRecord(context.Background(), "api.example.com", "CNAME", "tunnel.enclii.dev", true)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.checkFn != nil {
				tt.checkFn(t, rec)
			}
		})
	}
}

func TestDeleteDNSRecord(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "successful deletion",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete {
					t.Errorf("method = %s, want DELETE", r.Method)
				}
				expected := "/zones/test-zone-id/dns_records/rec-456"
				if r.URL.Path != expected {
					t.Errorf("path = %s, want %s", r.URL.Path, expected)
				}
				resp := APIResponse[struct {
					ID string `json:"id"`
				}]{
					Success: true,
					Result: struct {
						ID string `json:"id"`
					}{ID: "rec-456"},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			}),
			wantErr: false,
		},
		{
			name: "API error",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				resp := APIResponse[struct {
					ID string `json:"id"`
				}]{
					Success: false,
					Errors:  []APIError{{Code: 1001, Message: "Record not found"}},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			}),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			err := client.DeleteDNSRecord(context.Background(), "rec-456")
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestListZones(t *testing.T) {
	tests := []struct {
		name    string
		pages   int // number of pages to return
		wantLen int
		wantErr bool
	}{
		{
			name:    "single page",
			pages:   1,
			wantLen: 2,
		},
		{
			name:    "multiple pages",
			pages:   3,
			wantLen: 6, // 2 zones per page * 3 pages
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				callCount++
				page := callCount

				zones := []Zone{
					{ID: fmt.Sprintf("zone-%d-a", page), Name: fmt.Sprintf("zone%da.com", page), Status: "active"},
					{ID: fmt.Sprintf("zone-%d-b", page), Name: fmt.Sprintf("zone%db.com", page), Status: "active"},
				}

				resp := APIResponse[[]Zone]{
					Success: true,
					Result:  zones,
					ResultInfo: &ResultInfo{
						Page:       page,
						PerPage:    50,
						TotalPages: tt.pages,
						Count:      2,
						TotalCount: tt.pages * 2,
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			client := newTestClient(t, server)
			zones, err := client.ListZones(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(zones) != tt.wantLen {
				t.Errorf("len(zones) = %d, want %d", len(zones), tt.wantLen)
			}
		})
	}

	t.Run("API error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := APIResponse[[]Zone]{
				Success: false,
				Errors:  []APIError{{Code: 9000, Message: "auth error"}},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := newTestClient(t, server)
		_, err := client.ListZones(context.Background())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestFindZoneForDomain(t *testing.T) {
	// Set up a server that returns 3 zones
	zones := []Zone{
		{ID: "z-enclii", Name: "enclii.dev", Status: "active"},
		{ID: "z-qubic", Name: "qubic.quest", Status: "active"},
		{ID: "z-quest", Name: "quest", Status: "active"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := APIResponse[[]Zone]{
			Success: true,
			Result:  zones,
			ResultInfo: &ResultInfo{
				Page:       1,
				TotalPages: 1,
				Count:      len(zones),
				TotalCount: len(zones),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := newTestClient(t, server)

	tests := []struct {
		name     string
		domain   string
		wantZone string
		wantErr  bool
	}{
		{
			name:     "exact domain match",
			domain:   "enclii.dev",
			wantZone: "enclii.dev",
		},
		{
			name:     "subdomain match",
			domain:   "api.enclii.dev",
			wantZone: "enclii.dev",
		},
		{
			name:     "longest suffix match prefers qubic.quest over quest",
			domain:   "api.qubic.quest",
			wantZone: "qubic.quest",
		},
		{
			name:    "no match returns error",
			domain:  "unknown.org",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			zone, err := client.FindZoneForDomain(context.Background(), tt.domain)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if zone.Name != tt.wantZone {
				t.Errorf("zone.Name = %q, want %q", zone.Name, tt.wantZone)
			}
		})
	}
}

func TestCreateZone(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
		checkFn func(t *testing.T, zone *Zone)
	}{
		{
			name: "successful creation",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %s, want POST", r.Method)
				}
				if r.URL.Path != "/zones" {
					t.Errorf("path = %s, want /zones", r.URL.Path)
				}

				body, _ := io.ReadAll(r.Body)
				var payload struct {
					Name    string `json:"name"`
					Account struct {
						ID string `json:"id"`
					} `json:"account"`
					JumpStart bool `json:"jump_start"`
				}
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Fatalf("failed to parse request body: %v", err)
				}
				if payload.Name != "tezca.mx" {
					t.Errorf("name = %q, want tezca.mx", payload.Name)
				}
				if payload.Account.ID != "test-account" {
					t.Errorf("account.id = %q, want test-account", payload.Account.ID)
				}
				if !payload.JumpStart {
					t.Error("jump_start = false, want true")
				}

				resp := APIResponse[Zone]{
					Success: true,
					Result: Zone{
						ID:          "zone-tezca",
						Name:        "tezca.mx",
						Status:      "pending",
						NameServers: []string{"ns1.cloudflare.com", "ns2.cloudflare.com"},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			}),
			wantErr: false,
			checkFn: func(t *testing.T, zone *Zone) {
				if zone.ID != "zone-tezca" {
					t.Errorf("ID = %q, want zone-tezca", zone.ID)
				}
				if zone.Name != "tezca.mx" {
					t.Errorf("Name = %q, want tezca.mx", zone.Name)
				}
				if zone.Status != "pending" {
					t.Errorf("Status = %q, want pending", zone.Status)
				}
			},
		},
		{
			name: "API error response",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				resp := APIResponse[Zone]{
					Success: false,
					Errors:  []APIError{{Code: 1061, Message: "A zone with that name already exists"}},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			}),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			zone, err := client.CreateZone(context.Background(), "tezca.mx")
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.checkFn != nil {
				tt.checkFn(t, zone)
			}
		})
	}
}

func TestEnsureZoneForDomain(t *testing.T) {
	tests := []struct {
		name       string
		domain     string
		wantZone   string
		wantErr    bool
		zoneExists bool // whether FindZoneForDomain should find an existing zone
	}{
		{
			name:       "zone already exists",
			domain:     "api.enclii.dev",
			wantZone:   "enclii.dev",
			zoneExists: true,
		},
		{
			name:       "zone does not exist - creates apex",
			domain:     "api.tezca.mx",
			wantZone:   "tezca.mx",
			zoneExists: false,
		},
		{
			name:       "bare apex domain - creates it",
			domain:     "tezca.mx",
			wantZone:   "tezca.mx",
			zoneExists: false,
		},
		{
			name:    "single segment domain - error",
			domain:  "localhost",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")

				// ListZones (GET /zones) — used by FindZoneForDomain
				if r.Method == http.MethodGet && r.URL.Path == "/zones" {
					var zones []Zone
					if tt.zoneExists {
						zones = []Zone{{ID: "z-existing", Name: tt.wantZone, Status: "active"}}
					}
					resp := APIResponse[[]Zone]{
						Success:    true,
						Result:     zones,
						ResultInfo: &ResultInfo{Page: 1, TotalPages: 1, Count: len(zones), TotalCount: len(zones)},
					}
					json.NewEncoder(w).Encode(resp)
					return
				}

				// CreateZone (POST /zones)
				if r.Method == http.MethodPost && r.URL.Path == "/zones" {
					body, _ := io.ReadAll(r.Body)
					var payload struct {
						Name string `json:"name"`
					}
					json.Unmarshal(body, &payload)

					resp := APIResponse[Zone]{
						Success: true,
						Result: Zone{
							ID:     "z-new",
							Name:   payload.Name,
							Status: "pending",
						},
					}
					json.NewEncoder(w).Encode(resp)
					return
				}
			}))
			defer server.Close()

			client := newTestClient(t, server)
			zone, err := client.EnsureZoneForDomain(context.Background(), tt.domain)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if zone.Name != tt.wantZone {
				t.Errorf("zone.Name = %q, want %q", zone.Name, tt.wantZone)
			}
		})
	}
}

func TestEnsureDNSRecord(t *testing.T) {
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		wantCreated bool
		wantErr     bool
	}{
		{
			name: "record does not exist - creates it",
			handler: func() http.HandlerFunc {
				callNum := 0
				return func(w http.ResponseWriter, r *http.Request) {
					callNum++
					w.Header().Set("Content-Type", "application/json")

					// Call 1: ListZones
					if r.URL.Path == "/zones" {
						resp := APIResponse[[]Zone]{
							Success:    true,
							Result:     []Zone{{ID: "z1", Name: "example.com", Status: "active"}},
							ResultInfo: &ResultInfo{Page: 1, TotalPages: 1, Count: 1, TotalCount: 1},
						}
						json.NewEncoder(w).Encode(resp)
						return
					}

					// Call 2: Check existing (GET dns_records with name query)
					if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/dns_records") {
						resp := APIResponse[[]DNSRecord]{
							Success: true,
							Result:  []DNSRecord{}, // empty — doesn't exist
						}
						json.NewEncoder(w).Encode(resp)
						return
					}

					// Call 3: Create record (POST)
					if r.Method == http.MethodPost {
						resp := APIResponse[DNSRecord]{
							Success: true,
							Result: DNSRecord{
								ID:      "new-rec",
								Name:    "api.example.com",
								Type:    "CNAME",
								Content: "tunnel.enclii.dev",
								Proxied: true,
							},
						}
						json.NewEncoder(w).Encode(resp)
						return
					}
				}
			}(),
			wantCreated: true,
			wantErr:     false,
		},
		{
			name: "record already exists - returns existing",
			handler: func() http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")

					// ListZones
					if r.URL.Path == "/zones" {
						resp := APIResponse[[]Zone]{
							Success:    true,
							Result:     []Zone{{ID: "z1", Name: "example.com", Status: "active"}},
							ResultInfo: &ResultInfo{Page: 1, TotalPages: 1, Count: 1, TotalCount: 1},
						}
						json.NewEncoder(w).Encode(resp)
						return
					}

					// Check existing — record found
					if r.Method == http.MethodGet {
						resp := APIResponse[[]DNSRecord]{
							Success: true,
							Result: []DNSRecord{{
								ID:      "existing-rec",
								Name:    "api.example.com",
								Type:    "CNAME",
								Content: "tunnel.enclii.dev",
								Proxied: true,
							}},
						}
						json.NewEncoder(w).Encode(resp)
						return
					}
				}
			}(),
			wantCreated: false,
			wantErr:     false,
		},
		{
			name: "zone not found returns error",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				resp := APIResponse[[]Zone]{
					Success:    true,
					Result:     []Zone{}, // no zones
					ResultInfo: &ResultInfo{Page: 1, TotalPages: 1, Count: 0, TotalCount: 0},
				}
				json.NewEncoder(w).Encode(resp)
			}),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			rec, created, err := client.EnsureDNSRecord(context.Background(), "api.example.com", "tunnel.enclii.dev")
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if created != tt.wantCreated {
				t.Errorf("created = %v, want %v", created, tt.wantCreated)
			}
			if rec == nil {
				t.Fatal("expected non-nil record")
			}
		})
	}
}

func TestEnsureDNSRecord_UpdatesMismatchedCNAME(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/zones" {
			resp := APIResponse[[]Zone]{
				Success:    true,
				Result:     []Zone{{ID: "z1", Name: "example.com", Status: "active"}},
				ResultInfo: &ResultInfo{Page: 1, TotalPages: 1, Count: 1, TotalCount: 1},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}

		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/dns_records") {
			resp := APIResponse[[]DNSRecord]{
				Success: true,
				Result: []DNSRecord{{
					ID:      "existing-rec",
					Name:    "api.example.com",
					Type:    "CNAME",
					Content: "tunnel.enclii.dev",
					Proxied: true,
					TTL:     1,
				}},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}

		if r.Method == http.MethodPut && r.URL.Path == "/zones/z1/dns_records/existing-rec" {
			body, _ := io.ReadAll(r.Body)
			var payload struct {
				Content string `json:"content"`
				Proxied bool   `json:"proxied"`
			}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("failed to parse request body: %v", err)
			}
			if payload.Content != "test-tunnel-id.cfargotunnel.com" {
				t.Fatalf("content = %q, want test-tunnel-id.cfargotunnel.com", payload.Content)
			}
			if !payload.Proxied {
				t.Fatal("proxied = false, want true")
			}

			resp := APIResponse[DNSRecord]{
				Success: true,
				Result: DNSRecord{
					ID:      "existing-rec",
					Name:    "api.example.com",
					Type:    "CNAME",
					Content: payload.Content,
					Proxied: payload.Proxied,
					TTL:     1,
				},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}

		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	rec, created, err := client.EnsureDNSRecord(context.Background(), "api.example.com", "test-tunnel-id.cfargotunnel.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created {
		t.Fatal("created = true, want false for updated record")
	}
	if rec.Content != "test-tunnel-id.cfargotunnel.com" {
		t.Fatalf("Content = %q, want test-tunnel-id.cfargotunnel.com", rec.Content)
	}
}
