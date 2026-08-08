package cloudflare

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// BROKEN-1: bestZoneMatch compared zone names case-sensitively. Cloudflare
// returns zone names lowercased and nothing upstream lowercased the declared
// hostname, so `api.Madfam.io` matched no zone, FindZoneForDomain answered
// ErrZoneNotFound — the sentinel that means "Cloudflare CONFIRMED this domain
// is not ours" — and the provisioning decision moved a live MADFAM hostname
// onto the Cloudflare for SaaS path with no Cloudflare failure involved.
func TestFindZoneForDomainMatchesRegardlessOfCase(t *testing.T) {
	spellings := []string{
		"api.madfam.io",
		"api.Madfam.io",
		"API.MADFAM.IO",
		"api.MADFAM.io",
		"madfam.io",
		"MADFAM.IO",
	}

	for _, domain := range spellings {
		t.Run(domain, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, http.StatusOK, APIResponse[[]Zone]{
					Success: true,
					Result: []Zone{
						{ID: "z-madfam", Name: "madfam.io", Status: "active"},
						{ID: "z-dhanam", Name: "dhan.am", Status: "active"},
					},
					ResultInfo: &ResultInfo{TotalPages: 1},
				})
			}))
			defer server.Close()

			zone, err := newTestClient(t, server).FindZoneForDomain(context.Background(), domain)
			if err != nil {
				t.Fatalf("FindZoneForDomain(%q) error = %v; a case variant is the same hostname", domain, err)
			}
			if zone.ID != "z-madfam" {
				t.Errorf("zone = %q, want z-madfam", zone.ID)
			}
		})
	}
}

// The longest-suffix rule has to survive the case fold too: a case-varied
// hostname must still pick the most specific zone, not merely the first that
// matches.
func TestBestZoneMatchPrefersTheMostSpecificZoneAcrossCase(t *testing.T) {
	zones := []Zone{
		{ID: "z-broad", Name: "madfam.io"},
		{ID: "z-narrow", Name: "pravara.madfam.io"},
	}

	for _, domain := range []string{"api.pravara.madfam.io", "API.Pravara.MADFAM.io"} {
		match := bestZoneMatch(zones, domain)
		if match == nil {
			t.Fatalf("bestZoneMatch(%q) = nil, want the pravara zone", domain)
		}
		if match.ID != "z-narrow" {
			t.Errorf("bestZoneMatch(%q) = %q, want z-narrow", domain, match.ID)
		}
	}
}

// A Cloudflare zone name that is itself not lowercased must not defeat the
// match either — the fold is applied to both sides.
func TestBestZoneMatchFoldsTheZoneNameToo(t *testing.T) {
	zones := []Zone{{ID: "z-mixed", Name: "MadFam.IO"}}
	if match := bestZoneMatch(zones, "api.madfam.io"); match == nil || match.ID != "z-mixed" {
		t.Errorf("bestZoneMatch = %+v, want z-mixed", match)
	}
}

// BROKEN-1 residual: a listing page with no result_info used to break the
// pagination loop and return the pages read so far with a NIL error. For zones
// that turns a silent truncation into ErrZoneNotFound, which callers read as
// Cloudflare's positive confirmation that a domain is client-owned.
func TestListZonesRefusesASilentlyTruncatedListing(t *testing.T) {
	// 50 zones is exactly one full page, so "is there a page 2?" is a real
	// question and result_info is the only thing that answers it.
	fullPage := make([]Zone, 0, 50)
	for i := 0; i < 50; i++ {
		fullPage = append(fullPage, Zone{ID: "z", Name: "filler-" + string(rune('a'+i%26)) + ".example", Status: "active"})
	}

	t.Run("a full page with no result_info is an error, not a complete listing", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, http.StatusOK, APIResponse[[]Zone]{Success: true, Result: fullPage})
		}))
		defer server.Close()

		if _, err := newTestClient(t, server).ListZones(context.Background()); err == nil {
			t.Fatal("ListZones returned nil error for a possibly truncated listing")
		}
	})

	t.Run("the truncation never becomes a confirmed miss", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, http.StatusOK, APIResponse[[]Zone]{Success: true, Result: fullPage})
		}))
		defer server.Close()

		_, err := newTestClient(t, server).FindZoneForDomain(context.Background(), "api.madfam.io")
		if err == nil {
			t.Fatal("expected an error")
		}
		if errors.Is(err, ErrZoneNotFound) {
			t.Fatalf("a truncated zone listing was reported as ErrZoneNotFound: %v", err)
		}
		if !strings.Contains(err.Error(), "result_info") {
			t.Errorf("err = %v, want it to name the missing pagination metadata", err)
		}
	})

	// A SHORT page cannot be followed by another one, so the common response
	// shape with no result_info and fewer than per_page results still works.
	t.Run("a short page with no result_info is still a complete listing", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, http.StatusOK, APIResponse[[]Zone]{
				Success: true,
				Result:  []Zone{{ID: "z-madfam", Name: "madfam.io", Status: "active"}},
			})
		}))
		defer server.Close()

		zone, err := newTestClient(t, server).FindZoneForDomain(context.Background(), "api.madfam.io")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if zone.ID != "z-madfam" {
			t.Errorf("zone = %q, want z-madfam", zone.ID)
		}
	})
}

// Same rule on the custom-hostname listing, where a truncation reads as
// "nobody holds this hostname" — the input to the ownership check.
func TestListCustomHostnamesRefusesASilentlyTruncatedListing(t *testing.T) {
	fullPage := make([]CustomHostname, 0, 50)
	for i := 0; i < 50; i++ {
		fullPage = append(fullPage, CustomHostname{ID: "ch", Hostname: "filler.example.com"})
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, APIResponse[[]CustomHostname]{Success: true, Result: fullPage})
	}))
	defer server.Close()

	_, err := newTestClient(t, server).FindCustomHostname(context.Background(), "fallback-zone", "app.client.com")
	if err == nil {
		t.Fatal("FindCustomHostname reported a possibly truncated listing as a clean miss")
	}
}
