package api

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/manifest"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func TestRegistrableDomain(t *testing.T) {
	tests := []struct {
		domain string
		want   string
		wantOK bool
	}{
		{domain: "madfam.io", want: "madfam.io", wantOK: true},
		{domain: "api.madfam.io", want: "madfam.io", wantOK: true},
		{domain: "a.b.madfam.io", want: "madfam.io", wantOK: true},
		{domain: "API.MadFam.IO", want: "madfam.io", wantOK: true},
		// Two-label public suffixes must not be mistaken for a subdomain.
		{domain: "example.com.mx", want: "example.com.mx", wantOK: true},
		{domain: "app.example.com.mx", want: "example.com.mx", wantOK: true},
		{domain: "a.b.example.com.mx", want: "example.com.mx", wantOK: true},
		{domain: "domain.co.uk", want: "domain.co.uk", wantOK: true},
		{domain: "sub.domain.co.uk", want: "domain.co.uk", wantOK: true},
		{domain: "sub.example.xyz", want: "example.xyz", wantOK: true},

		// MEDIUM-3: two-label suffixes the old hand-rolled table did not
		// carry. Guessing "the last two labels" derived "com.pe" as the apex
		// of "example.com.pe", which made every host under it read as nested
		// and rejected valid client domains at declaration time.
		{domain: "example.com.pe", want: "example.com.pe", wantOK: true},
		{domain: "app.example.com.pe", want: "example.com.pe", wantOK: true},
		{domain: "app.example.co.in", want: "example.co.in", wantOK: true},
		{domain: "app.example.com.tr", want: "example.com.tr", wantOK: true},
		{domain: "app.example.co.il", want: "example.co.il", wantOK: true},
		{domain: "app.example.com.hk", want: "example.com.hk", wantOK: true},
		{domain: "app.example.or.jp", want: "example.or.jp", wantOK: true},
		{domain: "app.example.edu.au", want: "example.edu.au", wantOK: true},
		{domain: "app.example.sch.uk", want: "example.sch.uk", wantOK: true},

		// Three-label suffixes: adding "edu.au" alone would have mis-derived
		// these in the other direction.
		{domain: "school.vic.edu.au", want: "school.vic.edu.au", wantOK: true},
		{domain: "www.school.vic.edu.au", want: "school.vic.edu.au", wantOK: true},

		// An unrecognised suffix is "cannot determine", never a guess.
		{domain: "sub.example.madeuptld", wantOK: false},
		{domain: "single", want: "single", wantOK: false},
		// A hostname that is itself a public suffix has no registrable domain.
		{domain: "com.mx", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			got, ok := registrableDomain(tt.domain)
			if ok != tt.wantOK {
				t.Fatalf("registrableDomain(%q) ok = %v, want %v (apex %q)", tt.domain, ok, tt.wantOK, got)
			}
			if ok && got != tt.want {
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

		// MEDIUM-3: these were all reported as nested, which rejected them.
		{"app.example.com.pe", false},
		{"app.example.co.in", false},
		{"app.example.com.tr", false},
		{"app.example.co.il", false},
		{"app.example.com.hk", false},
		{"app.example.or.jp", false},
		{"app.example.edu.au", false},
		{"app.example.sch.uk", false},
		{"www.school.vic.edu.au", false},
		{"a.b.school.vic.edu.au", true},

		// Unknown suffix: cannot determine, so not claimed as nested. A
		// genuinely nested host still fails visibly at the TLS handshake,
		// which is cheaper than refusing to deploy a correct domain.
		{"a.b.example.madeuptld", false},
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

// MEDIUM-2: the stricter validator rejects two hostnames that are already
// declared in a shipped manifest (pravara-mes/enclii.yaml). Rejecting them at
// DECLARATION time is right — a human sees the error and can act on it —
// but the DEPLOY path must keep reconciling them, because silently dropping a
// live domain from reconciliation is a worse outcome than a certificate that
// was already broken.
func TestShippedNestedProductionHostnames(t *testing.T) {
	shipped := []string{"api.pravara.madfam.io", "app.pravara.madfam.io"}

	for _, domain := range shipped {
		t.Run(domain, func(t *testing.T) {
			if !isNestedSubdomain(domain) {
				t.Fatalf("%q should still be recognised as nested", domain)
			}
			// Declaration-time entry points reject it, with a remedy.
			if err := validateDomain(domain, false); err == nil {
				t.Errorf("validateDomain(%q, false) = nil; declaration must still reject it", domain)
			}
			// The deploy path validates with allowNested=true, so the domain
			// is never skipped there.
			if err := validateDomain(domain, true); err != nil {
				t.Errorf("validateDomain(%q, true) = %v; the deploy path must keep reconciling it", domain, err)
			}
		})
	}
}

// The deploy path itself must not skip a nested host: provisionSingleDomain
// reaching the existence check proves the domain was not dropped by
// validation.
func TestProvisionSingleDomainDoesNotSkipShippedNestedHost(t *testing.T) {
	h, mock, cleanup := newSQLMockHandler(t)
	defer cleanup()

	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM custom_domains WHERE domain = \$1\)`).
		WithArgs("api.pravara.madfam.io").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	h.provisionSingleDomain(context.Background(), &types.Service{
		ID:        uuid.New(),
		ProjectID: uuid.New(),
		Name:      "pravara-mes-api",
	}, manifest.EncliiYAMLDomain{
		Name:        "api.pravara.madfam.io",
		Environment: "production",
	}, 80)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("the nested domain was skipped before the existence check: %v", err)
	}
}

// A domain whose `external:` value could not be read is the one thing the
// deploy path does skip, by name.
func TestProvisionSingleDomainSkipsUnreadableExternalValue(t *testing.T) {
	h, mock, cleanup := newSQLMockHandler(t)
	defer cleanup()

	h.provisionSingleDomain(context.Background(), &types.Service{
		ID:        uuid.New(),
		ProjectID: uuid.New(),
		Name:      "svc",
	}, manifest.EncliiYAMLDomain{
		Name:        "app.client.com",
		Environment: "production",
		External:    &manifest.ExternalFlag{Invalid: "perhaps"},
	}, 80)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected database activity for a skipped domain: %v", err)
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
