package cloudflare

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateCustomHostname(t *testing.T) {
	tests := []struct {
		name    string
		zoneID  string
		host    string
		opts    *CreateCustomHostnameOptions
		handler http.HandlerFunc
		wantErr bool
		checkFn func(t *testing.T, ch *CustomHostname)
	}{
		{
			name:   "successful creation with defaults",
			zoneID: "fallback-zone",
			host:   "cto.creatumundo.mx",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %s, want POST", r.Method)
				}
				if r.URL.Path != "/zones/fallback-zone/custom_hostnames" {
					t.Errorf("path = %s, want /zones/fallback-zone/custom_hostnames", r.URL.Path)
				}

				body, _ := io.ReadAll(r.Body)
				var payload struct {
					Hostname string `json:"hostname"`
					SSL      struct {
						Method   string `json:"method"`
						Type     string `json:"type"`
						Settings struct {
							MinTLSVersion string `json:"min_tls_version"`
						} `json:"settings"`
					} `json:"ssl"`
					CustomOriginServer string `json:"custom_origin_server"`
				}
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Fatalf("failed to parse request body: %v", err)
				}
				if payload.Hostname != "cto.creatumundo.mx" {
					t.Errorf("hostname = %q, want cto.creatumundo.mx", payload.Hostname)
				}
				if payload.SSL.Method != SSLMethodTXT {
					t.Errorf("ssl.method = %q, want %q", payload.SSL.Method, SSLMethodTXT)
				}
				if payload.SSL.Type != "dv" {
					t.Errorf("ssl.type = %q, want dv", payload.SSL.Type)
				}
				if payload.SSL.Settings.MinTLSVersion != "1.2" {
					t.Errorf("ssl.settings.min_tls_version = %q, want 1.2", payload.SSL.Settings.MinTLSVersion)
				}
				if payload.CustomOriginServer != "" {
					t.Errorf("custom_origin_server = %q, want empty", payload.CustomOriginServer)
				}

				writeJSON(t, w, http.StatusCreated, APIResponse[CustomHostname]{
					Success: true,
					Result: CustomHostname{
						ID:       "ch-123",
						Hostname: "cto.creatumundo.mx",
						Status:   CustomHostnameStatusPending,
						SSL: CustomHostnameSSL{
							Status: CustomHostnameSSLPendingValidation,
							Method: SSLMethodTXT,
							ValidationRecords: []SSLValidationRecord{{
								TxtName:  "_acme-challenge.cto.creatumundo.mx",
								TxtValue: "dcv-token",
							}},
						},
						OwnershipVerification: OwnershipVerification{
							Type:  "txt",
							Name:  "_cf-custom-hostname.cto.creatumundo.mx",
							Value: "ownership-token",
						},
					},
				})
			},
			checkFn: func(t *testing.T, ch *CustomHostname) {
				if ch.ID != "ch-123" {
					t.Errorf("ID = %q, want ch-123", ch.ID)
				}
				if ch.Status != CustomHostnameStatusPending {
					t.Errorf("Status = %q, want pending", ch.Status)
				}
				if ch.IsActive() {
					t.Error("IsActive() = true for a pending hostname")
				}
			},
		},
		{
			name:   "options are honoured",
			zoneID: "fallback-zone",
			host:   "app.client.com",
			opts: &CreateCustomHostnameOptions{
				SSLMethod:          SSLMethodHTTP,
				MinTLSVersion:      "1.3",
				CustomOriginServer: "origin.enclii.dev",
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				var payload struct {
					SSL struct {
						Method   string `json:"method"`
						Settings struct {
							MinTLSVersion string `json:"min_tls_version"`
						} `json:"settings"`
					} `json:"ssl"`
					CustomOriginServer string `json:"custom_origin_server"`
				}
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Fatalf("failed to parse request body: %v", err)
				}
				if payload.SSL.Method != SSLMethodHTTP {
					t.Errorf("ssl.method = %q, want %q", payload.SSL.Method, SSLMethodHTTP)
				}
				if payload.SSL.Settings.MinTLSVersion != "1.3" {
					t.Errorf("min_tls_version = %q, want 1.3", payload.SSL.Settings.MinTLSVersion)
				}
				if payload.CustomOriginServer != "origin.enclii.dev" {
					t.Errorf("custom_origin_server = %q, want origin.enclii.dev", payload.CustomOriginServer)
				}

				writeJSON(t, w, http.StatusOK, APIResponse[CustomHostname]{
					Success: true,
					Result:  CustomHostname{ID: "ch-456", Hostname: "app.client.com"},
				})
			},
			checkFn: func(t *testing.T, ch *CustomHostname) {
				if ch.ID != "ch-456" {
					t.Errorf("ID = %q, want ch-456", ch.ID)
				}
			},
		},
		{
			name:   "API reports failure",
			zoneID: "fallback-zone",
			host:   "app.client.com",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, http.StatusOK, APIResponse[CustomHostname]{
					Success: false,
					Errors:  []APIError{{Code: 1406, Message: "custom hostname already exists"}},
				})
			},
			wantErr: true,
		},
		{
			name:   "4xx response",
			zoneID: "fallback-zone",
			host:   "app.client.com",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, http.StatusBadRequest, APIResponse[CustomHostname]{
					Success: false,
					Errors:  []APIError{{Code: 1407, Message: "invalid hostname"}},
				})
			},
			wantErr: true,
		},
		{
			name:   "5xx response",
			zoneID: "fallback-zone",
			host:   "app.client.com",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("upstream failure"))
			},
			wantErr: true,
		},
		{
			name:   "malformed body",
			zoneID: "fallback-zone",
			host:   "app.client.com",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("{not json"))
			},
			wantErr: true,
		},
		{
			name:   "success without an id is rejected",
			zoneID: "fallback-zone",
			host:   "app.client.com",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, http.StatusOK, APIResponse[CustomHostname]{
					Success: true,
					Result:  CustomHostname{Hostname: "app.client.com"},
				})
			},
			wantErr: true,
		},
		{
			name:    "missing zone id fails before any request",
			zoneID:  "",
			host:    "app.client.com",
			handler: func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected request") },
			wantErr: true,
		},
		{
			name:    "missing hostname fails before any request",
			zoneID:  "fallback-zone",
			host:    "",
			handler: func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected request") },
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			ch, err := client.CreateCustomHostname(context.Background(), tt.zoneID, tt.host, tt.opts)
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
				tt.checkFn(t, ch)
			}
		})
	}
}

func TestGetCustomHostname(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		handler http.HandlerFunc
		wantErr bool
		checkFn func(t *testing.T, ch *CustomHostname)
	}{
		{
			name: "active hostname",
			id:   "ch-123",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("method = %s, want GET", r.Method)
				}
				if r.URL.Path != "/zones/fallback-zone/custom_hostnames/ch-123" {
					t.Errorf("path = %s, unexpected", r.URL.Path)
				}
				writeJSON(t, w, http.StatusOK, APIResponse[CustomHostname]{
					Success: true,
					Result: CustomHostname{
						ID:       "ch-123",
						Hostname: "cto.creatumundo.mx",
						Status:   CustomHostnameStatusActive,
						SSL:      CustomHostnameSSL{Status: CustomHostnameSSLActive},
					},
				})
			},
			checkFn: func(t *testing.T, ch *CustomHostname) {
				if !ch.IsActive() {
					t.Error("IsActive() = false for an active hostname with an active certificate")
				}
			},
		},
		{
			name: "hostname active but certificate is not",
			id:   "ch-123",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, http.StatusOK, APIResponse[CustomHostname]{
					Success: true,
					Result: CustomHostname{
						ID:     "ch-123",
						Status: CustomHostnameStatusActive,
						SSL:    CustomHostnameSSL{Status: CustomHostnameSSLPendingIssuance},
					},
				})
			},
			checkFn: func(t *testing.T, ch *CustomHostname) {
				if ch.IsActive() {
					t.Error("IsActive() = true while the certificate is still pending issuance")
				}
			},
		},
		{
			name: "not found",
			id:   "ch-missing",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, http.StatusNotFound, APIResponse[CustomHostname]{
					Success: false,
					Errors:  []APIError{{Code: 1436, Message: "custom hostname not found"}},
				})
			},
			wantErr: true,
		},
		{
			name: "success false with no errors",
			id:   "ch-123",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, http.StatusOK, APIResponse[CustomHostname]{Success: false})
			},
			wantErr: true,
		},
		{
			name:    "missing id fails before any request",
			id:      "",
			handler: func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected request") },
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			ch, err := client.GetCustomHostname(context.Background(), "fallback-zone", tt.id)
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
				tt.checkFn(t, ch)
			}
		})
	}
}

func TestListCustomHostnames(t *testing.T) {
	t.Run("follows pagination and applies filters", func(t *testing.T) {
		var seenHostnameFilter string
		var pages []string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/zones/fallback-zone/custom_hostnames" {
				t.Errorf("path = %s, unexpected", r.URL.Path)
			}
			seenHostnameFilter = r.URL.Query().Get("hostname")
			page := r.URL.Query().Get("page")
			pages = append(pages, page)

			if page == "1" {
				writeJSON(t, w, http.StatusOK, APIResponse[[]CustomHostname]{
					Success:    true,
					Result:     []CustomHostname{{ID: "ch-1", Hostname: "a.client.com"}},
					ResultInfo: &ResultInfo{Page: 1, TotalPages: 2},
				})
				return
			}
			writeJSON(t, w, http.StatusOK, APIResponse[[]CustomHostname]{
				Success:    true,
				Result:     []CustomHostname{{ID: "ch-2", Hostname: "b.client.com"}},
				ResultInfo: &ResultInfo{Page: 2, TotalPages: 2},
			})
		}))
		defer server.Close()

		client := newTestClient(t, server)
		hostnames, err := client.ListCustomHostnames(context.Background(), "fallback-zone",
			&ListCustomHostnamesFilter{Hostname: "a.client.com"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(hostnames) != 2 {
			t.Fatalf("len(hostnames) = %d, want 2", len(hostnames))
		}
		if seenHostnameFilter != "a.client.com" {
			t.Errorf("hostname filter = %q, want a.client.com", seenHostnameFilter)
		}
		if strings.Join(pages, ",") != "1,2" {
			t.Errorf("pages = %v, want [1 2]", pages)
		}
	})

	t.Run("nil filter is allowed", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("hostname"); got != "" {
				t.Errorf("hostname filter = %q, want empty", got)
			}
			writeJSON(t, w, http.StatusOK, APIResponse[[]CustomHostname]{Success: true})
		}))
		defer server.Close()

		client := newTestClient(t, server)
		if _, err := client.ListCustomHostnames(context.Background(), "fallback-zone", nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("API error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, http.StatusOK, APIResponse[[]CustomHostname]{
				Success: false,
				Errors:  []APIError{{Code: 9109, Message: "invalid access"}},
			})
		}))
		defer server.Close()

		client := newTestClient(t, server)
		if _, err := client.ListCustomHostnames(context.Background(), "fallback-zone", nil); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("malformed body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("<html>gateway</html>"))
		}))
		defer server.Close()

		client := newTestClient(t, server)
		if _, err := client.ListCustomHostnames(context.Background(), "fallback-zone", nil); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("missing zone id", func(t *testing.T) {
		client := newTestClient(t, httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("unexpected request")
		})))
		if _, err := client.ListCustomHostnames(context.Background(), "", nil); err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestDeleteCustomHostname(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "standard envelope",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete {
					t.Errorf("method = %s, want DELETE", r.Method)
				}
				if r.URL.Path != "/zones/fallback-zone/custom_hostnames/ch-123" {
					t.Errorf("path = %s, unexpected", r.URL.Path)
				}
				writeJSON(t, w, http.StatusOK, map[string]interface{}{
					"success": true,
					"result":  map[string]string{"id": "ch-123"},
				})
			},
		},
		{
			name: "bare result without a success field is accepted",
			handler: func(w http.ResponseWriter, r *http.Request) {
				// Cloudflare's custom hostname delete returns the bare object.
				writeJSON(t, w, http.StatusOK, map[string]string{"id": "ch-123"})
			},
		},
		{
			name: "explicit failure",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, http.StatusOK, map[string]interface{}{
					"success": false,
					"errors":  []map[string]interface{}{{"code": 1436, "message": "not found"}},
				})
			},
			wantErr: true,
		},
		{
			name: "4xx response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, http.StatusForbidden, APIResponse[interface{}]{
					Success: false,
					Errors:  []APIError{{Code: 9109, Message: "invalid access"}},
				})
			},
			wantErr: true,
		},
		{
			name: "5xx response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte("bad gateway"))
			},
			wantErr: true,
		},
		{
			name: "malformed body",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("{"))
			},
			wantErr: true,
		},
		{
			// LOW-3: an empty object confirms nothing. Reporting it as a
			// successful delete would let teardown clear the stored hostname
			// id while the registration is still live at the edge.
			name: "empty object confirms nothing",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, http.StatusOK, map[string]interface{}{})
			},
			wantErr: true,
		},
		{
			name: "null body confirms nothing",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("null"))
			},
			wantErr: true,
		},
		{
			name: "success envelope with an empty result confirms nothing",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, http.StatusOK, map[string]interface{}{
					"errors": []map[string]interface{}{},
				})
			},
			wantErr: true,
		},
		{
			name: "explicit success is a proper confirmation",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, http.StatusOK, map[string]interface{}{"success": true})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			err := client.DeleteCustomHostname(context.Background(), "fallback-zone", "ch-123")
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

	t.Run("missing ids fail before any request", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("unexpected request")
		}))
		defer server.Close()

		client := newTestClient(t, server)
		if err := client.DeleteCustomHostname(context.Background(), "", "ch-123"); err == nil {
			t.Error("expected error for empty zone id")
		}
		if err := client.DeleteCustomHostname(context.Background(), "fallback-zone", ""); err == nil {
			t.Error("expected error for empty hostname id")
		}
	})
}

func TestEnsureCustomHostname(t *testing.T) {
	t.Run("returns the existing registration without creating", func(t *testing.T) {
		var posts int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				posts++
				t.Error("unexpected POST: hostname already exists")
			}
			writeJSON(t, w, http.StatusOK, APIResponse[[]CustomHostname]{
				Success: true,
				Result: []CustomHostname{{
					ID:       "ch-existing",
					Hostname: "cto.creatumundo.mx",
					Status:   CustomHostnameStatusActive,
					SSL:      CustomHostnameSSL{Status: CustomHostnameSSLActive},
				}},
			})
		}))
		defer server.Close()

		client := newTestClient(t, server)
		ch, created, err := client.EnsureCustomHostname(context.Background(), "fallback-zone", "cto.creatumundo.mx", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if created {
			t.Error("created = true, want false for an existing hostname")
		}
		if ch.ID != "ch-existing" {
			t.Errorf("ID = %q, want ch-existing", ch.ID)
		}
		if posts != 0 {
			t.Errorf("posts = %d, want 0", posts)
		}
	})

	t.Run("creates when absent", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				writeJSON(t, w, http.StatusOK, APIResponse[[]CustomHostname]{Success: true})
				return
			}
			writeJSON(t, w, http.StatusOK, APIResponse[CustomHostname]{
				Success: true,
				Result: CustomHostname{
					ID:       "ch-new",
					Hostname: "cto.creatumundo.mx",
					Status:   CustomHostnameStatusPending,
					SSL:      CustomHostnameSSL{Status: CustomHostnameSSLPendingValidation},
				},
			})
		}))
		defer server.Close()

		client := newTestClient(t, server)
		ch, created, err := client.EnsureCustomHostname(context.Background(), "fallback-zone", "cto.creatumundo.mx", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !created {
			t.Error("created = false, want true")
		}
		if ch.ID != "ch-new" {
			t.Errorf("ID = %q, want ch-new", ch.ID)
		}
	})

	t.Run("lost create race resolves to the existing hostname", func(t *testing.T) {
		var listCalls int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				listCalls++
				if listCalls == 1 {
					writeJSON(t, w, http.StatusOK, APIResponse[[]CustomHostname]{Success: true})
					return
				}
				writeJSON(t, w, http.StatusOK, APIResponse[[]CustomHostname]{
					Success: true,
					Result:  []CustomHostname{{ID: "ch-raced", Hostname: "cto.creatumundo.mx"}},
				})
				return
			}
			writeJSON(t, w, http.StatusConflict, APIResponse[CustomHostname]{
				Success: false,
				Errors:  []APIError{{Code: 1406, Message: "custom hostname already exists"}},
			})
		}))
		defer server.Close()

		client := newTestClient(t, server)
		ch, created, err := client.EnsureCustomHostname(context.Background(), "fallback-zone", "cto.creatumundo.mx", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if created {
			t.Error("created = true, want false when the create lost a race")
		}
		if ch.ID != "ch-raced" {
			t.Errorf("ID = %q, want ch-raced", ch.ID)
		}
	})

	t.Run("surfaces a create failure that is not a race", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				writeJSON(t, w, http.StatusOK, APIResponse[[]CustomHostname]{Success: true})
				return
			}
			writeJSON(t, w, http.StatusForbidden, APIResponse[CustomHostname]{
				Success: false,
				Errors:  []APIError{{Code: 9109, Message: "invalid access"}},
			})
		}))
		defer server.Close()

		client := newTestClient(t, server)
		if _, _, err := client.EnsureCustomHostname(context.Background(), "fallback-zone", "cto.creatumundo.mx", nil); err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestFindCustomHostnameNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Cloudflare's hostname filter is a prefix/contains match on some
		// plans, so a non-matching row must not be returned as a match.
		writeJSON(t, w, http.StatusOK, APIResponse[[]CustomHostname]{
			Success: true,
			Result:  []CustomHostname{{ID: "ch-other", Hostname: "other.client.com"}},
		})
	}))
	defer server.Close()

	client := newTestClient(t, server)
	ch, err := client.FindCustomHostname(context.Background(), "fallback-zone", "cto.creatumundo.mx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch != nil {
		t.Fatalf("FindCustomHostname() = %+v, want nil", ch)
	}
}

func TestPendingClientDNSRecords(t *testing.T) {
	tests := []struct {
		name           string
		hostname       *CustomHostname
		fallbackOrigin string
		want           []ClientDNSRecord
	}{
		{
			name: "pending hostname needs routing, ownership and DCV records",
			hostname: &CustomHostname{
				Hostname: "cto.creatumundo.mx",
				Status:   CustomHostnameStatusPending,
				SSL: CustomHostnameSSL{
					Status: CustomHostnameSSLPendingValidation,
					ValidationRecords: []SSLValidationRecord{{
						TxtName:  "_acme-challenge.cto.creatumundo.mx",
						TxtValue: "dcv-token",
					}},
				},
				OwnershipVerification: OwnershipVerification{
					Type:  "txt",
					Name:  "_cf-custom-hostname.cto.creatumundo.mx",
					Value: "ownership-token",
				},
			},
			fallbackOrigin: "proxy.enclii.dev",
			want: []ClientDNSRecord{
				{Purpose: DNSRecordPurposeRouting, Type: "CNAME", Name: "cto.creatumundo.mx", Value: "proxy.enclii.dev"},
				{Purpose: DNSRecordPurposeOwnership, Type: "TXT", Name: "_cf-custom-hostname.cto.creatumundo.mx", Value: "ownership-token"},
				{Purpose: DNSRecordPurposeSSLValidation, Type: "TXT", Name: "_acme-challenge.cto.creatumundo.mx", Value: "dcv-token"},
			},
		},
		{
			name: "fully active hostname owes nothing",
			hostname: &CustomHostname{
				Hostname: "cto.creatumundo.mx",
				Status:   CustomHostnameStatusActive,
				SSL:      CustomHostnameSSL{Status: CustomHostnameSSLActive},
				OwnershipVerification: OwnershipVerification{
					Type: "txt", Name: "_cf-custom-hostname.cto.creatumundo.mx", Value: "ownership-token",
				},
			},
			fallbackOrigin: "proxy.enclii.dev",
			want:           nil,
		},
		{
			name: "active hostname with a pending certificate still owes the DCV record",
			hostname: &CustomHostname{
				Hostname: "cto.creatumundo.mx",
				Status:   CustomHostnameStatusActive,
				SSL: CustomHostnameSSL{
					Status: CustomHostnameSSLPendingValidation,
					ValidationRecords: []SSLValidationRecord{{
						TxtName:  "_acme-challenge.cto.creatumundo.mx",
						TxtValue: "dcv-token",
					}},
				},
			},
			fallbackOrigin: "proxy.enclii.dev",
			want: []ClientDNSRecord{
				{Purpose: DNSRecordPurposeSSLValidation, Type: "TXT", Name: "_acme-challenge.cto.creatumundo.mx", Value: "dcv-token"},
			},
		},
		{
			name: "no fallback origin omits the routing record",
			hostname: &CustomHostname{
				Hostname: "cto.creatumundo.mx",
				Status:   CustomHostnameStatusPending,
				SSL:      CustomHostnameSSL{Status: CustomHostnameSSLActive},
			},
			fallbackOrigin: "",
			want:           nil,
		},
		{
			name: "http-only validation record is not reported as a DNS record",
			hostname: &CustomHostname{
				Hostname: "cto.creatumundo.mx",
				Status:   CustomHostnameStatusActive,
				SSL: CustomHostnameSSL{
					Status:            CustomHostnameSSLPendingValidation,
					ValidationRecords: []SSLValidationRecord{{HTTPURL: "http://cto.creatumundo.mx/.well-known/x"}},
				},
			},
			fallbackOrigin: "proxy.enclii.dev",
			want:           nil,
		},
		{
			name:           "nil hostname",
			hostname:       nil,
			fallbackOrigin: "proxy.enclii.dev",
			want:           nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.hostname.PendingClientDNSRecords(tt.fallbackOrigin)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d records, want %d: %+v", len(got), len(tt.want), got)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("record[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestAPIErrorIsTyped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusConflict, APIResponse[CustomHostname]{
			Success: false,
			Errors:  []APIError{{Code: 1406, Message: "custom hostname already exists"}},
		})
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.CreateCustomHostname(context.Background(), "fallback-zone", "app.client.com", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	apiErr, ok := IsAPIError(err)
	if !ok {
		t.Fatalf("IsAPIError() = false for %v", err)
	}
	if apiErr.Code != 1406 {
		t.Errorf("code = %d, want 1406", apiErr.Code)
	}
	if !strings.Contains(err.Error(), "custom hostname already exists") {
		t.Errorf("error = %q, want it to carry the Cloudflare message", err.Error())
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, body interface{}) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Fatalf("failed to encode test response: %v", err)
	}
}
