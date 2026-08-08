package services

import (
	"strings"
	"testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/cloudflare"
)

// GAP: the tunnel ingress store was never migrated.
//
// Migration 034 lowercases custom_domains and junctions, but the cloudflared
// ingress rules live in a ConfigMap or in Cloudflare's tunnel configuration
// API, where no migration can reach them — and every comparison against them
// was byte-exact. A pre-existing mixed-case rule would therefore have survived
// RemoveRoute, which could not see it, and then gained a DUPLICATE from
// AddRoute, which also could not see it. Two rules for one hostname, resolved
// by list order.
//
// The corpus is clean today (0 uppercase across 91 declared hostnames), so this
// is closed by canonicalising the comparisons rather than by rewriting data.

func TestCanonicalHostname(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"app.client.com", "app.client.com"},
		{"App.Client.com", "app.client.com"},
		{"APP.CLIENT.COM", "app.client.com"},
		{"  App.Client.com  ", "app.client.com"},
		{"", ""},
	} {
		if got := CanonicalHostname(tc.in); got != tc.want {
			t.Errorf("CanonicalHostname(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// CanonicalHostname must agree with api.canonicalDomain. They are deliberately
// separate (services cannot import api) and answer for different stores, but a
// hostname that canonicalises one way in the database and another in the tunnel
// config is the whole bug class over again.
func TestCanonicalHostnameMatchesTheDatabaseCanonicalisation(t *testing.T) {
	for _, in := range []string{"App.Client.com", " API.Madfam.IO ", "release.victim.com"} {
		// api.canonicalDomain is strings.ToLower(strings.TrimSpace(domain)).
		want := strings.ToLower(strings.TrimSpace(in))
		if got := CanonicalHostname(in); got != want {
			t.Errorf("CanonicalHostname(%q) = %q, want %q to match api.canonicalDomain", in, got, want)
		}
	}
}

// RemoveRoute's filter: a mixed-case rule names the same host and must be
// removed. Missing it leaves a hostname pointing at a torn-down service.
func TestFilterOutHostnameRemovesACaseVariantRule(t *testing.T) {
	rules := []IngressRule{
		{Hostname: "Release.Victim.com", Service: "http://victim.victim.svc.cluster.local:80"},
		{Hostname: "other.example.com", Service: "http://other.other.svc.cluster.local:80"},
		{Service: DefaultCatchAllService},
	}

	kept, found := filterOutHostname(rules, "release.victim.com")
	if !found {
		t.Fatal("the mixed-case rule was not found; RemoveRoute would report success having removed nothing")
	}
	for _, rule := range kept {
		if strings.EqualFold(rule.Hostname, "release.victim.com") {
			t.Errorf("rule %q survived removal", rule.Hostname)
		}
	}
	if len(kept) != 2 {
		t.Errorf("kept %d rules, want 2", len(kept))
	}
}

// AddRoute's existence check: finding the mixed-case rule is what stops a
// second rule being appended for the same hostname.
func TestIndexOfHostnameFindsACaseVariantRule(t *testing.T) {
	rules := []IngressRule{
		{Hostname: "other.example.com"},
		{Hostname: "Release.Victim.com"},
		{Service: DefaultCatchAllService},
	}

	if got := indexOfHostname(rules, "release.victim.com"); got != 1 {
		t.Errorf("indexOfHostname = %d, want 1. Reading the rule as absent appends a duplicate: "+
			"two ingress rules for one hostname, and which one serves is list order.", got)
	}
	if got := indexOfHostname(rules, "absent.example.com"); got != -1 {
		t.Errorf("indexOfHostname for an absent host = %d, want -1", got)
	}
}

// The Cloudflare-API implementation is a parallel code path and has to agree,
// exactly as insertBeforeCatchAll and insertBeforeCatchAllCF do.
func TestCloudflareRouteHelpersAreCaseInsensitiveToo(t *testing.T) {
	rules := []cloudflare.TunnelIngressRule{
		{Hostname: "other.example.com"},
		{Hostname: "Release.Victim.com"},
		{Service: DefaultCatchAllService},
	}

	if got := indexOfHostnameCF(rules, "release.victim.com"); got != 1 {
		t.Errorf("indexOfHostnameCF = %d, want 1", got)
	}

	kept, found := filterOutHostnameCF(rules, "RELEASE.VICTIM.COM")
	if !found {
		t.Fatal("filterOutHostnameCF did not find the case-variant rule")
	}
	if len(kept) != 2 {
		t.Errorf("kept %d rules, want 2", len(kept))
	}
}
