package api

import "testing"

func TestPorkbunDNSApplyIntentFromRequestDerivesApexAndRelativeName(t *testing.T) {
	req := operatorOperationRequest{
		Scope: map[string]string{"project": "phyne", "service": "app"},
		Args:  map[string]string{"target": "app.phyne.app", "content": "example.cfargotunnel.com"},
	}

	intent := porkbunDNSApplyIntentFromRequest(req, "default.cfargotunnel.com")

	if intent.Target != "app.phyne.app" {
		t.Fatalf("target = %q, want app.phyne.app", intent.Target)
	}
	if intent.Domain != "phyne.app" {
		t.Fatalf("domain = %q, want phyne.app", intent.Domain)
	}
	if intent.Name != "app" {
		t.Fatalf("name = %q, want app", intent.Name)
	}
	if intent.RecordType != "CNAME" {
		t.Fatalf("record type = %q, want CNAME", intent.RecordType)
	}
	if intent.Content != "example.cfargotunnel.com" {
		t.Fatalf("content = %q, want example.cfargotunnel.com", intent.Content)
	}
	if intent.TTL != "600" {
		t.Fatalf("ttl = %q, want 600", intent.TTL)
	}
}

func TestPorkbunDNSApplyIntentAllowsExplicitApexAndName(t *testing.T) {
	req := operatorOperationRequest{
		Args: map[string]string{
			"target":  "crm.madfam.io",
			"domain":  "madfam.io",
			"name":    "crm",
			"type":    "CNAME",
			"content": "tenant-router.example.net",
			"ttl":     "300",
		},
	}

	intent := porkbunDNSApplyIntentFromRequest(req, "default.cfargotunnel.com")

	if intent.Domain != "madfam.io" || intent.Name != "crm" || intent.TTL != "300" {
		t.Fatalf("unexpected explicit intent: %+v", intent)
	}
	if invalid := validatePorkbunDNSApplyIntent(intent); invalid != "" {
		t.Fatalf("intent should be valid: %s", invalid)
	}
}

func TestPorkbunNameserverParsingNormalizesAndDedupes(t *testing.T) {
	got := parseNameservers("NS1.EXAMPLE.COM., ns2.example.com ns1.example.com")
	want := []string{"ns1.example.com", "ns2.example.com"}

	if len(got) != len(want) {
		t.Fatalf("nameservers = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("nameservers[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestPorkbunRecordNameMatchesFullAndRelativeNames(t *testing.T) {
	intent := porkbunDNSApplyIntent{Target: "app.phyne.app", Domain: "phyne.app", Name: "app", RecordType: "CNAME"}

	if !porkbunRecordNameMatches("app", intent) {
		t.Fatal("relative record name should match")
	}
	if !porkbunRecordNameMatches("app.phyne.app", intent) {
		t.Fatal("fully qualified record name should match")
	}
	if porkbunRecordNameMatches("www.phyne.app", intent) {
		t.Fatal("different record name should not match")
	}
}

func TestSameStringSetIgnoresOrderAndCase(t *testing.T) {
	left := []string{"NS2.EXAMPLE.COM.", "ns1.example.com"}
	right := []string{"ns1.example.com.", "ns2.example.com"}

	if !sameStringSet(left, right) {
		t.Fatalf("sets should match: %#v %#v", left, right)
	}
}
