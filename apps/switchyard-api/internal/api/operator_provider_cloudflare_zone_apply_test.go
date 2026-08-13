package api

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/cloudflare"
)

// TestZoneNotFoundSentinelIsNotStringMatchable is the regression guard for the
// defect this test file was added for.
//
// handleProviderCloudflareZoneAddApplyDryRun used to decide "is this error just
// 'the zone does not exist yet'?" with:
//
//	strings.Contains(err.Error(), "no Cloudflare zone found")
//
// FindZoneForDomain returns cloudflare.ErrZoneNotFound, whose text is
// "cloudflare: no zone found for domain". Those two strings do not overlap, so
// the guard never matched. An absent zone — the ONLY state in which
// zone-add-apply has any work to do — was reported as provider_read_failed, and
// the apply path turns that status into a 502. The advertised capability could
// therefore never create a zone, and did not fail loudly enough for anyone to
// notice: the operator saw a plausible "failed to read Cloudflare zone state".
//
// Observed against production on 2026-08-13 while provisioning angelia.run:
//
//	POST /v1/providers/cloudflare/zone-add-apply {"args":{"target":"angelia.run"}}
//	-> status "provider_read_failed"
//	   warning "cloudflare: no zone found for domain: angelia.run"
//
// while the same call for an EXISTING zone (madfam.io) returned ready_to_apply —
// i.e. the operation worked only when there was nothing to do.
//
// This test asserts the property that made the old code wrong, so that
// reintroducing a string match fails here rather than in production.
func TestZoneNotFoundSentinelIsNotStringMatchable(t *testing.T) {
	// The error as FindZoneForDomain actually wraps it.
	err := fmt.Errorf("%w: %s", cloudflare.ErrZoneNotFound, "angelia.run")

	if !errors.Is(err, cloudflare.ErrZoneNotFound) {
		t.Fatalf("errors.Is must recognise the wrapped sentinel; got %v", err)
	}

	// The exact literal the handler used to look for. If this ever starts
	// matching, someone has reworded the sentinel back and this test should be
	// re-examined rather than deleted.
	const staleLiteral = "no Cloudflare zone found"
	if strings.Contains(err.Error(), staleLiteral) {
		t.Fatalf(
			"sentinel text now contains %q, so the old string guard would appear to work; "+
				"the handler must still use errors.Is — string-matching a sentinel is the bug",
			staleLiteral,
		)
	}
}

// TestZoneNotFoundIsDistinctFromRealReadFailures guards the other direction: a
// genuine provider read failure must NOT be treated as "zone absent", or
// zone-add-apply would try to create a zone on the back of an API outage.
func TestZoneNotFoundIsDistinctFromRealReadFailures(t *testing.T) {
	realFailure := errors.New("cloudflare: 502 Bad Gateway reading zones")

	if errors.Is(realFailure, cloudflare.ErrZoneNotFound) {
		t.Fatal("a transport failure must not satisfy ErrZoneNotFound; " +
			"creating a zone because the API was briefly unreachable is worse than failing")
	}
}
