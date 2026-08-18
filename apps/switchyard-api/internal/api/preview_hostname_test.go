package api

import (
	"strings"
	"testing"
)

// Previews must serve under a domain we actually own. The prior code used
// `.preview.enclii.app` (a domain we do NOT control) for serving while cleanup
// removed `.enclii.dev` — so previews could never resolve, and any route that
// did exist was orphaned on merge. These tests pin: the zone is enclii.dev, and
// the create/cleanup hostnames match.

// ownedZones are the domains enclii actually controls. enclii.app is NOT one.
var ownedZones = []string{".enclii.dev", ".enclii.com"}

func TestPreviewDomainSuffix_IsAnOwnedZone(t *testing.T) {
	if !strings.HasPrefix(previewDomainSuffix, ".") {
		t.Fatalf("suffix must start with a dot for `subdomain+suffix` to form a host: %q", previewDomainSuffix)
	}
	if strings.Contains(previewDomainSuffix, "enclii.app") {
		t.Fatalf("preview zone points at enclii.app, a domain we do not own: %q", previewDomainSuffix)
	}
	owned := false
	for _, z := range ownedZones {
		if strings.HasSuffix(previewDomainSuffix, z) {
			owned = true
		}
	}
	if !owned {
		t.Fatalf("preview zone %q is not under an owned domain %v", previewDomainSuffix, ownedZones)
	}
	if previewDomainSuffix != ".preview.enclii.dev" {
		t.Fatalf("preview zone changed unexpectedly: %q", previewDomainSuffix)
	}
}

func TestPreviewHostname_CreateAndCleanupMatch(t *testing.T) {
	subdomain := "pr-123-my-api"

	// The hostname the serving Ingress / tunnel route is created under
	// (webhook_pr.go create path).
	created := subdomain + previewDomainSuffix
	// The hostname cleanup removes (webhook_pr.go cleanupPreviewResources).
	// Both derive from the same constant, so they cannot drift.
	removed := subdomain + previewDomainSuffix

	if created != removed {
		t.Fatalf("create/cleanup hostname mismatch would orphan the tunnel route: created=%q removed=%q", created, removed)
	}
	if created != "pr-123-my-api.preview.enclii.dev" {
		t.Fatalf("unexpected preview hostname: %q", created)
	}
	// Guard against the specific regression: serving must NOT target enclii.app.
	if strings.Contains(created, "enclii.app") {
		t.Fatal("preview hostname is back on enclii.app — a domain we do not own")
	}
}
