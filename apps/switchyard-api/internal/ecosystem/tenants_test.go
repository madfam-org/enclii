package ecosystem

import "testing"

func TestTenantFromDomain(t *testing.T) {
	cases := map[string]TenantID{
		"enclii.dev":          TenantEnclii,
		"api.enclii.dev":      TenantEnclii,
		"janua.dev":           TenantJanua,
		"auth.madfam.io":      TenantMADFAM,
		"unknown.example.com": TenantOther,
	}
	for domain, want := range cases {
		if got := TenantFromDomain(domain); got != want {
			t.Fatalf("TenantFromDomain(%q) = %q, want %q", domain, got, want)
		}
	}
}

func TestDefaultSenderForTenant(t *testing.T) {
	if got := DefaultSenderForTenant(TenantEnclii); got != "noreply@enclii.dev" {
		t.Fatalf("got %q", got)
	}
}
