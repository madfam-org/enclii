package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/manifest"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// BROKEN-1: the mechanism decision was case-sensitive end to end.
//
// Cloudflare zone names are always lowercase and nothing lowercased the
// declared hostname, so the same host declared with different capitalisation
// took DIFFERENT provisioning paths:
//
//	api.madfam.io  → zone-cname       (correct)
//	api.Madfam.io  → custom-hostname  (a live MADFAM domain rerouted)
//	API.MADFAM.IO  → custom-hostname
//
// End to end that registered a real custom hostname, flipped the row to
// tls_provider=cloudflare-for-saas / status=pending / verified=false and never
// ensured the zone CNAME — the exact outcome HIGH-1 exists to prevent, reached
// with no Cloudflare failure at all.
func TestDomainMechanismIsCaseInsensitive(t *testing.T) {
	declared := []struct {
		domain string
		want   domainProvisioningMechanism
	}{
		{"api.madfam.io", mechanismZoneCNAME},
		{"api.Madfam.io", mechanismZoneCNAME},
		{"API.MADFAM.IO", mechanismZoneCNAME},
		{"Api.MadFam.Io", mechanismZoneCNAME},
		// A genuinely client-owned domain still takes the custom-hostname path,
		// in any spelling.
		{"cto.creatumundo.mx", mechanismCustomHostname},
		{"CTO.CreatuMundo.MX", mechanismCustomHostname},
	}

	for _, tt := range declared {
		t.Run(tt.domain, func(t *testing.T) {
			h := saasConfiguredHandler()
			zones := &stubZoneResolver{zonesWeControl: map[string]bool{"madfam.io": true, "api.madfam.io": true}}

			got, err := h.resolveDomainMechanism(
				context.Background(), zones, canonicalDomain(tt.domain), nil)
			if err != nil {
				t.Fatalf("resolveDomainMechanism(%q) error = %v", tt.domain, err)
			}
			if got != tt.want {
				t.Errorf("mechanism for %q = %q, want %q: capitalisation must not change how a domain reaches the edge",
					tt.domain, got, tt.want)
			}
		})
	}
}

func TestCanonicalDomain(t *testing.T) {
	tests := []struct{ in, want string }{
		{"api.madfam.io", "api.madfam.io"},
		{"API.MADFAM.IO", "api.madfam.io"},
		{"Api.MadFam.Io", "api.madfam.io"},
		{"  app.client.com  ", "app.client.com"},
		{"App.Victim.com", "app.victim.com"},
		{"", ""},
		// The root dot is deliberately preserved so validateDomain can still
		// reject it as a malformed declaration.
		{"example.com.", "example.com."},
	}

	for _, tt := range tests {
		if got := canonicalDomain(tt.in); got != tt.want {
			t.Errorf("canonicalDomain(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// The manifest parse boundary canonicalises too, so every consumer of
// Spec.Domains — provisioning, status pages, onboarding — sees one spelling.
func TestManifestParseCanonicalisesDeclaredHostnames(t *testing.T) {
	cfg, err := manifest.ParseEncliiYAML([]byte(`
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: web
spec:
  domains:
    - name: API.MADFAM.IO
      environment: production
    - name: "  App.Client.com  "
      environment: production
`))
	if err != nil {
		t.Fatalf("ParseEncliiYAML() error = %v", err)
	}
	want := []string{"api.madfam.io", "app.client.com"}
	if len(cfg.Spec.Domains) != len(want) {
		t.Fatalf("parsed %d domains, want %d", len(cfg.Spec.Domains), len(want))
	}
	for i, expected := range want {
		if cfg.Spec.Domains[i].Name != expected {
			t.Errorf("Domains[%d].Name = %q, want %q", i, cfg.Spec.Domains[i].Name, expected)
		}
	}
}

// The deploy path canonicalises before it touches storage: an uppercase
// manifest value must reach the existence check — and therefore every
// subsequent lookup and comparison — lowercased.
func TestProvisionSingleDomainCanonicalisesBeforeStorage(t *testing.T) {
	h, mock, cleanup := newSQLMockHandler(t)
	defer cleanup()

	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM custom_domains WHERE lower\(domain\) = lower\(\$1\)\)`).
		WithArgs("api.madfam.io").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	h.provisionSingleDomain(context.Background(), &types.Service{
		ID:        uuid.New(),
		ProjectID: uuid.New(),
		Name:      "madfam-web",
	}, manifest.EncliiYAMLDomain{
		Name:        "API.Madfam.IO",
		Environment: "production",
	}, 80)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("the declared hostname did not reach storage canonicalised: %v", err)
	}
}

// And the whole edge pass agrees: the same host in two spellings resolves to
// the same zone and the same mechanism, so the zone CNAME is ensured for both.
func TestProvisionDomainEdgeTakesTheZonePathForACaseVariant(t *testing.T) {
	for _, declared := range []string{"api.madfam.io", "API.Madfam.IO"} {
		t.Run(declared, func(t *testing.T) {
			var ensuredCNAMEFor string
			h, mock, cleanup := newStubbedCloudflareHandler(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/zones":
					writeStubJSON(t, w, http.StatusOK, map[string]interface{}{
						"success": true,
						"result": []map[string]interface{}{
							{"id": "z-madfam", "name": "madfam.io", "status": "active"},
						},
						"result_info": map[string]interface{}{"total_pages": 1},
					})
				case r.Method == http.MethodPost:
					t.Errorf("a custom hostname was registered for %s; it belongs to a zone we control", declared)
					writeStubJSON(t, w, http.StatusOK, map[string]interface{}{"success": true})
				case r.Method == http.MethodPut:
					// The record exists but points somewhere else, so the zone
					// path rewrites it. Reaching here IS the proof the zone
					// CNAME was ensured.
					ensuredCNAMEFor = declared
					writeStubJSON(t, w, http.StatusOK, map[string]interface{}{
						"success": true,
						"result": map[string]interface{}{
							"id": "rec-1", "type": "CNAME", "name": "api.madfam.io",
							"content": "tunnel.enclii.dev", "proxied": true,
						},
					})
				default:
					writeStubJSON(t, w, http.StatusOK, map[string]interface{}{
						"success": true,
						"result": []map[string]interface{}{
							{"id": "rec-1", "type": "CNAME", "name": "api.madfam.io",
								"content": "tunnel.enclii.dev", "proxied": true},
						},
						"result_info": map[string]interface{}{"total_pages": 1},
					})
				}
			})
			defer cleanup()

			h.tunnelRoutesService = newMockTunnelRoutesManager()
			projectID := uuid.New()

			// The zone-path ownership guard reads the hostname's owners.
			expectHostnameUnclaimed(mock, canonicalDomain(declared))
			// persistDomainProvisioningResult reloads the row; there is none.
			mock.ExpectQuery(`SELECT\s+id, service_id, environment_id, domain`).
				WithArgs(canonicalDomain(declared)).
				WillReturnRows(sqlmock.NewRows(customDomainTestColumns))

			result := h.provisionDomainEdge(context.Background(), canonicalDomain(declared), &types.Service{
				ID:        uuid.New(),
				ProjectID: projectID,
				Name:      "madfam-web",
			}, "production", 80, nil)

			if result.Mechanism != mechanismZoneCNAME {
				t.Errorf("mechanism = %q, want %q", result.Mechanism, mechanismZoneCNAME)
			}
			if result.Err != nil {
				t.Errorf("unexpected error: %v", result.Err)
			}
			if ensuredCNAMEFor == "" {
				t.Error("the zone CNAME was never ensured")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet sql expectations: %v", err)
			}
		})
	}
}

// The open question the review flagged: isAlphanumeric gated only the first and
// last byte of a label, so anything at all was legal in between — interior
// unicode (the IDN homoglyph case), and separators that are not legal in a
// hostname and would still be handed to Cloudflare and written into a tunnel
// ingress rule.
func TestValidateDomainRejectsNonLDHLabelInteriors(t *testing.T) {
	rejected := []struct {
		domain string
		why    string
	}{
		{"pаypal.com", "interior Cyrillic а reads identically to the ASCII spelling"},
		{"göogle.com", "interior non-ASCII"},
		{"a_b.example.com", "underscore is not legal in a hostname"},
		{"a b.example.com", "space"},
		{"a/b.example.com", "path separator"},
		{"a%2e.example.com", "percent escape"},
		{"a:b.example.com", "colon"},
	}

	for _, tt := range rejected {
		t.Run(tt.why, func(t *testing.T) {
			if err := validateDomain(tt.domain, true); err == nil {
				t.Errorf("validateDomain(%q) = nil, want a rejection (%s)", tt.domain, tt.why)
			}
		})
	}

	// An internationalised domain is declarable in its punycode form, which is
	// pure LDH, and hyphens stay legal everywhere they were before.
	for _, accepted := range []string{
		"xn--80ak6aa92e.com",
		"xn--fiqs8s.example.com",
		"my-app.example.com",
		"a--b.example.com",
	} {
		if err := validateDomain(accepted, true); err != nil {
			t.Errorf("validateDomain(%q) = %v, want nil", accepted, err)
		}
	}
}

// The suffix table gained the generic TLDs the estate actually registers on,
// so the nesting check is no longer inert for them.
func TestKnownSuffixesCoverTheEstatesGenericTLDs(t *testing.T) {
	tests := []struct {
		domain   string
		wantApex string
	}{
		{"blueprint.tube", "blueprint.tube"},
		{"api.blueprint.tube", "blueprint.tube"},
		{"almanac.solar", "almanac.solar"},
		{"api.selva.town", "selva.town"},
		{"ceq.lol", "ceq.lol"},
		{"forj.design", "forj.design"},
		{"nuit.one", "nuit.one"},
		{"primavera3d.pro", "primavera3d.pro"},
		{"penny.onl", "penny.onl"},
		{"example.academy", "example.academy"},
		// .pro carries professional second-level suffixes, so the apex under
		// them is three labels, not two.
		{"firm.cpa.pro", "firm.cpa.pro"},
		{"www.firm.cpa.pro", "firm.cpa.pro"},
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			apex, ok := registrableDomain(tt.domain)
			if !ok {
				t.Fatalf("registrableDomain(%q) could not determine an apex", tt.domain)
			}
			if apex != tt.wantApex {
				t.Errorf("registrableDomain(%q) = %q, want %q", tt.domain, apex, tt.wantApex)
			}
		})
	}

	// And the nesting check now actually fires under them.
	for _, nested := range []string{
		"a.b.blueprint.tube",
		"a.b.selva.town",
		"a.b.primavera3d.pro",
	} {
		if !isNestedSubdomain(nested) {
			t.Errorf("isNestedSubdomain(%q) = false; the suffix is now recognised so nesting must be visible", nested)
		}
	}
}
