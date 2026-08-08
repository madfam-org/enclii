package api

import (
	"strings"
	"testing"
)

func TestRegistrableDomain(t *testing.T) {
	tests := []struct {
		domain string
		want   string
	}{
		{"madfam.io", "madfam.io"},
		{"api.madfam.io", "madfam.io"},
		{"a.b.madfam.io", "madfam.io"},
		{"API.MadFam.IO", "madfam.io"},
		// Two-label public suffixes must not be mistaken for a subdomain.
		{"example.com.mx", "example.com.mx"},
		{"app.example.com.mx", "example.com.mx"},
		{"a.b.example.com.mx", "example.com.mx"},
		{"domain.co.uk", "domain.co.uk"},
		{"sub.domain.co.uk", "domain.co.uk"},
		// Unknown two-label suffixes fall back to the last two labels.
		{"sub.example.xyz", "example.xyz"},
		{"single", "single"},
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			if got := registrableDomain(tt.domain); got != tt.want {
				t.Errorf("registrableDomain(%q) = %q, want %q", tt.domain, got, tt.want)
			}
		})
	}
}

func TestIsNestedSubdomain(t *testing.T) {
	tests := []struct {
		domain string
		want   bool
	}{
		{"madfam.io", false},
		{"api.madfam.io", false},
		{"a.b.madfam.io", true},
		{"a.b.c.madfam.io", true},
		{"example.com.mx", false},
		{"app.example.com.mx", false},
		{"a.b.example.com.mx", true},
		{"sub.domain.co.uk", false},
		{"a.sub.domain.co.uk", true},
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			if got := isNestedSubdomain(tt.domain); got != tt.want {
				t.Errorf("isNestedSubdomain(%q) = %v, want %v", tt.domain, got, tt.want)
			}
		})
	}
}

func TestValidateDomain(t *testing.T) {
	tests := []struct {
		name        string
		domain      string
		allowNested bool
		wantErr     bool
		errContains string
	}{
		{name: "apex", domain: "madfam.io"},
		{name: "single level", domain: "api.madfam.io"},
		{name: "single level under a two-label suffix", domain: "app.example.com.mx"},
		{
			name:        "empty",
			domain:      "",
			wantErr:     true,
			errContains: "required",
		},
		{
			name: "too long",
			domain: strings.Repeat("a", 60) + "." + strings.Repeat("b", 60) + "." +
				strings.Repeat("c", 60) + "." + strings.Repeat("d", 60) + "." +
				strings.Repeat("e", 60) + ".io",
			wantErr:     true,
			errContains: "253",
		},
		{
			name:        "leading dot",
			domain:      ".madfam.io",
			wantErr:     true,
			errContains: "start or end with a dot",
		},
		{
			name:        "trailing dot",
			domain:      "madfam.io.",
			wantErr:     true,
			errContains: "start or end with a dot",
		},
		{
			name:        "no dot",
			domain:      "madfam",
			wantErr:     true,
			errContains: "at least one dot",
		},
		{
			name:        "empty label",
			domain:      "api..madfam.io",
			wantErr:     true,
			errContains: "empty label",
		},
		{
			name:        "label too long",
			domain:      strings.Repeat("a", 64) + ".madfam.io",
			wantErr:     true,
			errContains: "63 character maximum",
		},
		{
			name:        "leading hyphen",
			domain:      "-api.madfam.io",
			wantErr:     true,
			errContains: "start and end with a letter or digit",
		},
		{
			name:        "trailing hyphen",
			domain:      "api-.madfam.io",
			wantErr:     true,
			errContains: "start and end with a letter or digit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDomain(tt.domain, tt.allowNested)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("validateDomain(%q) = nil, want error", tt.domain)
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateDomain(%q) = %v, want nil", tt.domain, err)
			}
		})
	}
}

func TestValidateDomainNestedSubdomains(t *testing.T) {
	nested := []string{"a.b.madfam.io", "deep.nested.client.com", "a.b.example.com.mx"}

	for _, domain := range nested {
		t.Run(domain+"/rejected by default", func(t *testing.T) {
			err := validateDomain(domain, false)
			if err == nil {
				t.Fatalf("validateDomain(%q, false) = nil, want a nested-subdomain error", domain)
			}
			// The message has to tell the operator both why it failed and
			// what to do instead.
			for _, want := range []string{"Universal SSL", "external: true"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to mention %q", err.Error(), want)
				}
			}
		})

		t.Run(domain+"/allowed on the custom hostname path", func(t *testing.T) {
			// Cloudflare for SaaS issues a certificate for the exact
			// hostname, so nesting is fine there.
			if err := validateDomain(domain, true); err != nil {
				t.Fatalf("validateDomain(%q, true) = %v, want nil", domain, err)
			}
		})
	}
}

func TestIsValidDomainMatchesValidateDomain(t *testing.T) {
	domains := []string{"madfam.io", "api.madfam.io", "a.b.madfam.io", "", "no-tld"}

	for _, domain := range domains {
		t.Run(domain, func(t *testing.T) {
			want := validateDomain(domain, false) == nil
			if got := isValidDomain(domain); got != want {
				t.Errorf("isValidDomain(%q) = %v, want %v", domain, got, want)
			}
		})
	}
}
