package api

import (
	"testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/cloudflare"
)

// allow_pending_zone lets dns-apply stage records into a zone the registrar
// has not delegated yet (pre-cutover mirroring, e.g. creatumundo.mx ahead of
// its Wix→Porkbun transfer). Staging must be an explicit, auditable choice:
// only the literal "true" enables it. Anything truthy-looking but different
// stays strict, so a typo cannot quietly bypass the DNS-authority guard.
func TestCloudflareDNSApplyAllowPendingIsStrict(t *testing.T) {
	cases := map[string]bool{
		"true":   true,
		"TRUE":   true,
		" true ": true,
		"":       false,
		"false":  false,
		"1":      false,
		"yes":    false,
		"on":     false,
		"truthy": false,
	}
	for input, want := range cases {
		req := operatorOperationRequest{Args: map[string]string{"allow_pending_zone": input}}
		if got := cloudflareDNSApplyAllowPending(req); got != want {
			t.Fatalf("allow_pending_zone=%q parsed as %v, want %v", input, got, want)
		}
	}
	if cloudflareDNSApplyAllowPending(operatorOperationRequest{}) {
		t.Fatal("absent args must stay strict")
	}
}

// A non-active zone must be impossible to miss in the response: a staged
// write that reads like live DNS is how a cutover gets called done while
// nothing serves.
func TestCloudflareDNSApplyPendingWarnings(t *testing.T) {
	if w := cloudflareDNSApplyPendingWarnings(&cloudflare.Zone{Name: "creatumundo.mx", Status: "pending"}); len(w) != 1 {
		t.Fatalf("pending zone must warn, got %v", w)
	}
	if w := cloudflareDNSApplyPendingWarnings(&cloudflare.Zone{Name: "madfam.io", Status: "active"}); w != nil {
		t.Fatalf("active zone must not warn, got %v", w)
	}
	if w := cloudflareDNSApplyPendingWarnings(nil); w != nil {
		t.Fatalf("nil zone must not warn, got %v", w)
	}
}
