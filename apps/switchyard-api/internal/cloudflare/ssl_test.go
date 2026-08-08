package cloudflare

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMatchesDomain(t *testing.T) {
	tests := []struct {
		name     string
		certHost string
		domain   string
		want     bool
	}{
		{
			name:     "exact match",
			certHost: "madfam.io",
			domain:   "madfam.io",
			want:     true,
		},
		{
			name:     "exact match is case insensitive",
			certHost: "MadFam.io",
			domain:   "madfam.io",
			want:     true,
		},
		{
			name:     "wildcard covers one label",
			certHost: "*.madfam.io",
			domain:   "app.madfam.io",
			want:     true,
		},
		{
			name:     "wildcard covers one label case insensitively",
			certHost: "*.MADFAM.io",
			domain:   "App.madfam.IO",
			want:     true,
		},
		{
			// Universal SSL covers a single level. Reporting a match here
			// would claim a certificate for a name whose TLS handshake fails
			// at the Cloudflare edge.
			name:     "wildcard does NOT cover a nested subdomain",
			certHost: "*.madfam.io",
			domain:   "a.b.madfam.io",
			want:     false,
		},
		{
			name:     "wildcard does not cover three levels",
			certHost: "*.madfam.io",
			domain:   "a.b.c.madfam.io",
			want:     false,
		},
		{
			name:     "wildcard does not cover the apex itself",
			certHost: "*.madfam.io",
			domain:   "madfam.io",
			want:     false,
		},
		{
			name:     "wildcard does not match a suffix collision",
			certHost: "*.madfam.io",
			domain:   "app.notmadfam.io",
			want:     false,
		},
		{
			name:     "wildcard does not match an unrelated domain",
			certHost: "*.madfam.io",
			domain:   "app.example.com",
			want:     false,
		},
		{
			name:     "nested wildcard covers its own one level",
			certHost: "*.staging.madfam.io",
			domain:   "app.staging.madfam.io",
			want:     true,
		},
		{
			name:     "nested wildcard does not cover two levels",
			certHost: "*.staging.madfam.io",
			domain:   "a.b.staging.madfam.io",
			want:     false,
		},
		{
			name:     "empty wildcard base never matches",
			certHost: "*.",
			domain:   "madfam.io",
			want:     false,
		},
		{
			name:     "empty label before the base does not match",
			certHost: "*.madfam.io",
			domain:   ".madfam.io",
			want:     false,
		},
		{
			name:     "non-wildcard cert does not match a subdomain",
			certHost: "madfam.io",
			domain:   "app.madfam.io",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesDomain(tt.certHost, tt.domain); got != tt.want {
				t.Errorf("matchesDomain(%q, %q) = %v, want %v", tt.certHost, tt.domain, got, tt.want)
			}
		})
	}
}

func TestGetSSLStatus(t *testing.T) {
	tests := []struct {
		name           string
		domain         string
		packs          []SSLCertificatePack
		wantCert       bool
		wantCertStatus string
	}{
		{
			name:           "wildcard pack covers a single-level subdomain",
			domain:         "app.madfam.io",
			packs:          []SSLCertificatePack{{Hosts: []string{"madfam.io", "*.madfam.io"}, Status: "active", CertificateAuthority: "digicert"}},
			wantCert:       true,
			wantCertStatus: "active",
		},
		{
			// Regression: the old matcher reported HasCertificate=true here,
			// which is a certificate Cloudflare does not actually serve.
			name:           "wildcard pack does not cover a nested subdomain",
			domain:         "a.b.madfam.io",
			packs:          []SSLCertificatePack{{Hosts: []string{"madfam.io", "*.madfam.io"}, Status: "active"}},
			wantCert:       false,
			wantCertStatus: "none",
		},
		{
			name:           "no packs",
			domain:         "app.madfam.io",
			packs:          nil,
			wantCert:       false,
			wantCertStatus: "none",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(APIResponse[[]SSLCertificatePack]{
					Success: true,
					Result:  tt.packs,
				})
			}))
			defer server.Close()

			client := newTestClient(t, server)
			status, err := client.GetSSLStatus(context.Background(), tt.domain)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if status.HasCertificate != tt.wantCert {
				t.Errorf("HasCertificate = %v, want %v", status.HasCertificate, tt.wantCert)
			}
			if status.CertificateStatus != tt.wantCertStatus {
				t.Errorf("CertificateStatus = %q, want %q", status.CertificateStatus, tt.wantCertStatus)
			}
		})
	}
}

func TestVerifyTLS(t *testing.T) {
	tests := []struct {
		name   string
		domain string
		packs  []SSLCertificatePack
		want   bool
	}{
		{
			name:   "active wildcard cert on a single-level host",
			domain: "app.madfam.io",
			packs:  []SSLCertificatePack{{Hosts: []string{"*.madfam.io"}, Status: "active"}},
			want:   true,
		},
		{
			name:   "nested host is not covered",
			domain: "a.b.madfam.io",
			packs:  []SSLCertificatePack{{Hosts: []string{"*.madfam.io"}, Status: "active"}},
			want:   false,
		},
		{
			name:   "pending cert is not verified",
			domain: "app.madfam.io",
			packs:  []SSLCertificatePack{{Hosts: []string{"*.madfam.io"}, Status: "pending_validation"}},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(APIResponse[[]SSLCertificatePack]{
					Success: true,
					Result:  tt.packs,
				})
			}))
			defer server.Close()

			client := newTestClient(t, server)
			ok, err := client.VerifyTLS(context.Background(), tt.domain)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok != tt.want {
				t.Errorf("VerifyTLS(%q) = %v, want %v", tt.domain, ok, tt.want)
			}
		})
	}
}
