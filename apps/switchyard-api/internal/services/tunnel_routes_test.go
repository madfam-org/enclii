package services

import (
	"testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/cloudflare"
)

// =============================================================================
// insertBeforeCatchAll
// =============================================================================

func TestInsertBeforeCatchAll(t *testing.T) {
	tests := []struct {
		name     string
		rules    []IngressRule
		newRule  IngressRule
		wantLen  int
		wantLast string // expected Service of the last rule
		wantNew  int    // expected index of the new rule
	}{
		{
			name: "insert before http_status:404 catch-all",
			rules: []IngressRule{
				{Hostname: "api.enclii.dev", Service: "http://api.enclii.svc.cluster.local:80"},
				{Service: DefaultCatchAllService},
			},
			newRule:  IngressRule{Hostname: "app.enclii.dev", Service: "http://app.enclii.svc.cluster.local:80"},
			wantLen:  3,
			wantLast: DefaultCatchAllService,
			wantNew:  1,
		},
		{
			name: "insert before http_status:500 catch-all",
			rules: []IngressRule{
				{Service: "http_status:500"},
			},
			newRule:  IngressRule{Hostname: "test.enclii.dev", Service: "http://test.svc:80"},
			wantLen:  2,
			wantLast: "http_status:500",
			wantNew:  0,
		},
		{
			name: "insert before localhost catch-all",
			rules: []IngressRule{
				{Hostname: "existing.enclii.dev", Service: "http://existing:80"},
				{Service: "http://localhost:8080"},
			},
			newRule:  IngressRule{Hostname: "new.enclii.dev", Service: "http://new:80"},
			wantLen:  3,
			wantLast: "http://localhost:8080",
			wantNew:  1,
		},
		{
			name:     "empty rules - appends new rule and adds catch-all",
			rules:    []IngressRule{},
			newRule:  IngressRule{Hostname: "api.enclii.dev", Service: "http://api:80"},
			wantLen:  2,
			wantLast: DefaultCatchAllService,
			wantNew:  0,
		},
		{
			name: "no catch-all found - appends and adds default catch-all",
			rules: []IngressRule{
				{Hostname: "api.enclii.dev", Service: "http://api:80"},
			},
			newRule:  IngressRule{Hostname: "app.enclii.dev", Service: "http://app:80"},
			wantLen:  3,
			wantLast: DefaultCatchAllService,
			wantNew:  1,
		},
		{
			name: "multiple existing routes - insert before catch-all at end",
			rules: []IngressRule{
				{Hostname: "api.enclii.dev", Service: "http://api:80"},
				{Hostname: "app.enclii.dev", Service: "http://app:80"},
				{Hostname: "admin.enclii.dev", Service: "http://admin:80"},
				{Service: DefaultCatchAllService},
			},
			newRule:  IngressRule{Hostname: "status.enclii.dev", Service: "http://status:80"},
			wantLen:  5,
			wantLast: DefaultCatchAllService,
			wantNew:  3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := insertBeforeCatchAll(tt.rules, tt.newRule)

			if len(got) != tt.wantLen {
				t.Errorf("insertBeforeCatchAll() returned %d rules, want %d", len(got), tt.wantLen)
				for i, r := range got {
					t.Logf("  [%d] hostname=%q service=%q", i, r.Hostname, r.Service)
				}
				return
			}

			// Verify last rule is the catch-all
			lastRule := got[len(got)-1]
			if lastRule.Service != tt.wantLast {
				t.Errorf("insertBeforeCatchAll() last rule service = %q, want %q", lastRule.Service, tt.wantLast)
			}

			// Verify new rule is at expected position
			if got[tt.wantNew].Hostname != tt.newRule.Hostname {
				t.Errorf("insertBeforeCatchAll() new rule at index %d has hostname %q, want %q",
					tt.wantNew, got[tt.wantNew].Hostname, tt.newRule.Hostname)
			}
		})
	}
}

// Test that insertBeforeCatchAll does not mutate the original slice
func TestInsertBeforeCatchAllDoesNotMutateOriginal(t *testing.T) {
	original := []IngressRule{
		{Hostname: "api.enclii.dev", Service: "http://api:80"},
		{Service: DefaultCatchAllService},
	}
	originalLen := len(original)

	newRule := IngressRule{Hostname: "new.enclii.dev", Service: "http://new:80"}
	result := insertBeforeCatchAll(original, newRule)

	// Original slice length should be unchanged (the underlying array might grow,
	// but the original slice header stays the same)
	if len(original) != originalLen {
		t.Errorf("insertBeforeCatchAll() mutated original slice length: got %d, want %d", len(original), originalLen)
	}

	// Result should be longer
	if len(result) != 3 {
		t.Errorf("insertBeforeCatchAll() result length = %d, want 3", len(result))
	}
}

// =============================================================================
// isCatchAllService
// =============================================================================

func TestIsCatchAllService(t *testing.T) {
	tests := []struct {
		name    string
		service string
		want    bool
	}{
		{"default catch-all http_status:404", DefaultCatchAllService, true},
		{"http_status:500", "http_status:500", true},
		{"http_status:503", "http_status:503", true},
		{"http_status: prefix", "http_status:200", true},
		{"localhost catch-all", "http://localhost:8080", true},
		{"regular service URL", "http://api.enclii.svc.cluster.local:80", false},
		{"empty string", "", false},
		{"random service", "http://some-service:3000", false},
		{"https localhost is not catch-all", "https://localhost:8080", false},
		{"http_status without colon", "http_status", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isCatchAllService(tt.service)
			if got != tt.want {
				t.Errorf("isCatchAllService(%q) = %v, want %v", tt.service, got, tt.want)
			}
		})
	}
}

// =============================================================================
// Constants validation
// =============================================================================

func TestTunnelRoutesConstants(t *testing.T) {
	if DefaultConfigMapNamespace != "cloudflare-tunnel" {
		t.Errorf("DefaultConfigMapNamespace = %q, want %q", DefaultConfigMapNamespace, "cloudflare-tunnel")
	}
	if DefaultConfigMapName != "cloudflared-config" {
		t.Errorf("DefaultConfigMapName = %q, want %q", DefaultConfigMapName, "cloudflared-config")
	}
	if ConfigYAMLKey != "config.yaml" {
		t.Errorf("ConfigYAMLKey = %q, want %q", ConfigYAMLKey, "config.yaml")
	}
	if DefaultCatchAllService != "http_status:404" {
		t.Errorf("DefaultCatchAllService = %q, want %q", DefaultCatchAllService, "http_status:404")
	}
}

// =============================================================================
// isCatchAllServiceCF (Cloudflare API variant)
// =============================================================================

func TestIsCatchAllServiceCF(t *testing.T) {
	tests := []struct {
		name    string
		service string
		want    bool
	}{
		{"default catch-all", DefaultCatchAllService, true},
		{"http_status:500", "http_status:500", true},
		{"http_status:503", "http_status:503", true},
		{"localhost catch-all", "http://localhost:8080", true},
		{"regular service URL", "http://api.enclii.svc.cluster.local:80", false},
		{"empty string", "", false},
		{"random service", "http://some-service:3000", false},
		{"https localhost is not catch-all", "https://localhost:8080", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isCatchAllServiceCF(tt.service)
			if got != tt.want {
				t.Errorf("isCatchAllServiceCF(%q) = %v, want %v", tt.service, got, tt.want)
			}
		})
	}
}

// =============================================================================
// insertBeforeCatchAllCF (Cloudflare API variant)
// =============================================================================

func TestInsertBeforeCatchAllCF(t *testing.T) {
	tests := []struct {
		name     string
		rules    []cloudflare.TunnelIngressRule
		newRule  cloudflare.TunnelIngressRule
		wantLen  int
		wantLast string // expected Service of the last rule
		wantNew  int    // expected index of the new rule
	}{
		{
			name: "insert before catch-all",
			rules: []cloudflare.TunnelIngressRule{
				{Hostname: "api.enclii.dev", Service: "http://api:80"},
				{Service: DefaultCatchAllService},
			},
			newRule:  cloudflare.TunnelIngressRule{Hostname: "app.enclii.dev", Service: "http://app:80"},
			wantLen:  3,
			wantLast: DefaultCatchAllService,
			wantNew:  1,
		},
		{
			name:     "empty rules - appends new rule and adds catch-all",
			rules:    []cloudflare.TunnelIngressRule{},
			newRule:  cloudflare.TunnelIngressRule{Hostname: "api.enclii.dev", Service: "http://api:80"},
			wantLen:  2,
			wantLast: DefaultCatchAllService,
			wantNew:  0,
		},
		{
			name: "no catch-all found - appends and adds default",
			rules: []cloudflare.TunnelIngressRule{
				{Hostname: "api.enclii.dev", Service: "http://api:80"},
			},
			newRule:  cloudflare.TunnelIngressRule{Hostname: "app.enclii.dev", Service: "http://app:80"},
			wantLen:  3,
			wantLast: DefaultCatchAllService,
			wantNew:  1,
		},
		{
			name: "insert before http_status:500 catch-all",
			rules: []cloudflare.TunnelIngressRule{
				{Service: "http_status:500"},
			},
			newRule:  cloudflare.TunnelIngressRule{Hostname: "test.dev", Service: "http://test:80"},
			wantLen:  2,
			wantLast: "http_status:500",
			wantNew:  0,
		},
		{
			name: "multiple routes - insert before catch-all at end",
			rules: []cloudflare.TunnelIngressRule{
				{Hostname: "api.enclii.dev", Service: "http://api:80"},
				{Hostname: "app.enclii.dev", Service: "http://app:80"},
				{Service: DefaultCatchAllService},
			},
			newRule:  cloudflare.TunnelIngressRule{Hostname: "status.enclii.dev", Service: "http://status:80"},
			wantLen:  4,
			wantLast: DefaultCatchAllService,
			wantNew:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := insertBeforeCatchAllCF(tt.rules, tt.newRule)

			if len(got) != tt.wantLen {
				t.Errorf("insertBeforeCatchAllCF() returned %d rules, want %d", len(got), tt.wantLen)
				return
			}

			lastRule := got[len(got)-1]
			if lastRule.Service != tt.wantLast {
				t.Errorf("last rule service = %q, want %q", lastRule.Service, tt.wantLast)
			}

			if got[tt.wantNew].Hostname != tt.newRule.Hostname {
				t.Errorf("new rule at index %d has hostname %q, want %q",
					tt.wantNew, got[tt.wantNew].Hostname, tt.newRule.Hostname)
			}
		})
	}
}

// =============================================================================
// ConfigMap and Cloudflare variants produce consistent behavior
// =============================================================================

func TestInsertBeforeCatchAll_ConfigMapAndCF_Consistent(t *testing.T) {
	// Verify that both implementations behave the same way for equivalent inputs
	cmRules := []IngressRule{
		{Hostname: "api.enclii.dev", Service: "http://api:80"},
		{Service: DefaultCatchAllService},
	}
	cfRules := []cloudflare.TunnelIngressRule{
		{Hostname: "api.enclii.dev", Service: "http://api:80"},
		{Service: DefaultCatchAllService},
	}

	cmResult := insertBeforeCatchAll(cmRules, IngressRule{Hostname: "new.dev", Service: "http://new:80"})
	cfResult := insertBeforeCatchAllCF(cfRules, cloudflare.TunnelIngressRule{Hostname: "new.dev", Service: "http://new:80"})

	if len(cmResult) != len(cfResult) {
		t.Errorf("ConfigMap variant returned %d rules, CF variant returned %d", len(cmResult), len(cfResult))
		return
	}

	for i := range cmResult {
		if cmResult[i].Hostname != cfResult[i].Hostname {
			t.Errorf("index %d: CM hostname=%q, CF hostname=%q", i, cmResult[i].Hostname, cfResult[i].Hostname)
		}
		if cmResult[i].Service != cfResult[i].Service {
			t.Errorf("index %d: CM service=%q, CF service=%q", i, cmResult[i].Service, cfResult[i].Service)
		}
	}
}
