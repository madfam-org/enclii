package cloudflare

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// HIGH-1: FindZoneForDomain used to answer "no Cloudflare zone found for
// domain X" for BOTH a genuine miss and every transport, HTTP, auth and
// pagination failure. Callers branch on that answer to pick a provisioning
// mechanism, so a 500 or a token blip during a deploy silently reclassified a
// MADFAM-owned domain as client-owned.
func TestFindZoneForDomainDistinguishesNotFoundFromFailure(t *testing.T) {
	t.Run("a genuine miss is ErrZoneNotFound", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, http.StatusOK, APIResponse[[]Zone]{
				Success:    true,
				Result:     []Zone{{ID: "z1", Name: "madfam.io", Status: "active"}},
				ResultInfo: &ResultInfo{TotalPages: 1},
			})
		}))
		defer server.Close()

		client := newTestClient(t, server)
		_, err := client.FindZoneForDomain(context.Background(), "cto.creatumundo.mx")
		if !errors.Is(err, ErrZoneNotFound) {
			t.Fatalf("err = %v, want ErrZoneNotFound", err)
		}
	})

	t.Run("a 500 is NOT ErrZoneNotFound", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal server error"))
		}))
		defer server.Close()

		client := newTestClient(t, server)
		_, err := client.FindZoneForDomain(context.Background(), "app.dhan.am")
		if err == nil {
			t.Fatal("expected an error")
		}
		if errors.Is(err, ErrZoneNotFound) {
			t.Fatalf("a 500 was reported as ErrZoneNotFound: %v", err)
		}
	})

	t.Run("a 429 is NOT ErrZoneNotFound", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, http.StatusTooManyRequests, APIResponse[interface{}]{
				Success: false,
				Errors:  []APIError{{Code: 10000, Message: "rate limited"}},
			})
		}))
		defer server.Close()

		client := newTestClient(t, server)
		_, err := client.FindZoneForDomain(context.Background(), "app.dhan.am")
		if err == nil {
			t.Fatal("expected an error")
		}
		if errors.Is(err, ErrZoneNotFound) {
			t.Fatalf("a 429 was reported as ErrZoneNotFound: %v", err)
		}
	})

	t.Run("an expired token is NOT ErrZoneNotFound", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, http.StatusUnauthorized, APIResponse[interface{}]{
				Success: false,
				Errors:  []APIError{{Code: 10000, Message: "Authentication error"}},
			})
		}))
		defer server.Close()

		client := newTestClient(t, server)
		_, err := client.FindZoneForDomain(context.Background(), "app.dhan.am")
		if errors.Is(err, ErrZoneNotFound) {
			t.Fatalf("an auth failure was reported as ErrZoneNotFound: %v", err)
		}
	})

	// ListZones filters status=active, so a zone that is pending activation or
	// has moved is absent from the normal listing. It is still our zone.
	t.Run("a pending zone is reported as not-active, not as absent", func(t *testing.T) {
		var calls int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.URL.Query().Get("status") == "active" {
				writeJSON(t, w, http.StatusOK, APIResponse[[]Zone]{
					Success:    true,
					Result:     []Zone{},
					ResultInfo: &ResultInfo{TotalPages: 1},
				})
				return
			}
			writeJSON(t, w, http.StatusOK, APIResponse[[]Zone]{
				Success:    true,
				Result:     []Zone{{ID: "z9", Name: "dhan.am", Status: "pending"}},
				ResultInfo: &ResultInfo{TotalPages: 1},
			})
		}))
		defer server.Close()

		client := newTestClient(t, server)
		_, err := client.FindZoneForDomain(context.Background(), "app.dhan.am")
		if err == nil {
			t.Fatal("expected an error for a zone that is not being served")
		}
		if errors.Is(err, ErrZoneNotFound) {
			t.Fatalf("a pending zone was reported as absent: %v", err)
		}

		var notActive *ZoneNotActiveError
		if !errors.As(err, &notActive) {
			t.Fatalf("err = %v, want *ZoneNotActiveError", err)
		}
		if notActive.ZoneName != "dhan.am" || notActive.Status != "pending" {
			t.Errorf("ZoneNotActiveError = %+v, want zone dhan.am status pending", notActive)
		}
		if calls < 2 {
			t.Errorf("unfiltered zone listing was never consulted (calls = %d)", calls)
		}
	})

	t.Run("an unfiltered listing failure is not a miss either", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("status") == "active" {
				writeJSON(t, w, http.StatusOK, APIResponse[[]Zone]{
					Success:    true,
					Result:     []Zone{},
					ResultInfo: &ResultInfo{TotalPages: 1},
				})
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("boom"))
		}))
		defer server.Close()

		client := newTestClient(t, server)
		_, err := client.FindZoneForDomain(context.Background(), "app.dhan.am")
		if err == nil {
			t.Fatal("expected an error")
		}
		if errors.Is(err, ErrZoneNotFound) {
			t.Fatalf("a failed unfiltered listing was reported as ErrZoneNotFound: %v", err)
		}
	})
}

// The same sentinel gates zone auto-creation: EnsureZoneForDomain treated any
// lookup error as "missing" and created a zone, so a listing that merely timed
// out would take over a domain nobody asked us to take over.
func TestEnsureZoneForDomainOnlyCreatesOnAConfirmedMiss(t *testing.T) {
	t.Run("does not create a zone when the lookup failed", func(t *testing.T) {
		var created bool
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				created = true
				writeJSON(t, w, http.StatusOK, APIResponse[Zone]{
					Success: true, Result: Zone{ID: "new", Name: "dhan.am"},
				})
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal server error"))
		}))
		defer server.Close()

		client := newTestClient(t, server)
		if _, err := client.EnsureZoneForDomain(context.Background(), "app.dhan.am"); err == nil {
			t.Fatal("expected the failed lookup to be surfaced")
		}
		if created {
			t.Error("a zone was created off the back of a failed lookup")
		}
	})

	t.Run("creates a zone on a confirmed miss", func(t *testing.T) {
		var created bool
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				created = true
				writeJSON(t, w, http.StatusOK, APIResponse[Zone]{
					Success: true, Result: Zone{ID: "new", Name: "creatumundo.mx"},
				})
				return
			}
			writeJSON(t, w, http.StatusOK, APIResponse[[]Zone]{
				Success: true, Result: []Zone{}, ResultInfo: &ResultInfo{TotalPages: 1},
			})
		}))
		defer server.Close()

		client := newTestClient(t, server)
		zone, err := client.EnsureZoneForDomain(context.Background(), "cto.creatumundo.mx")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !created {
			t.Error("the zone was not created for a confirmed miss")
		}
		if zone.Name != "creatumundo.mx" {
			t.Errorf("zone = %q, want creatumundo.mx", zone.Name)
		}
	})
}

// HIGH-3(a): EnsureCustomHostname matched on hostname alone and handed back
// whatever registration it found on the shared fallback-origin zone. The
// caller's ownership check has to run before any adoption, including the
// adoption that resolves a lost create race.
func TestEnsureCustomHostnameRespectsTheAdoptionGuard(t *testing.T) {
	refusal := errors.New("refusing to claim custom hostname app.client.com: it belongs to another project")

	t.Run("refuses to adopt an existing registration", func(t *testing.T) {
		var createAttempted bool
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				createAttempted = true
			}
			writeJSON(t, w, http.StatusOK, APIResponse[[]CustomHostname]{
				Success: true,
				Result: []CustomHostname{{
					ID:       "ch-owned-by-someone-else",
					Hostname: "app.client.com",
					Status:   CustomHostnameStatusActive,
					SSL:      CustomHostnameSSL{Status: CustomHostnameSSLActive},
				}},
				ResultInfo: &ResultInfo{TotalPages: 1},
			})
		}))
		defer server.Close()

		client := newTestClient(t, server)
		hostname, created, err := client.EnsureCustomHostname(
			context.Background(), "fallback-zone", "app.client.com",
			&CreateCustomHostnameOptions{
				AdoptGuard: func(existing *CustomHostname) error {
					if existing.ID != "ch-owned-by-someone-else" {
						t.Errorf("guard saw %+v, want the existing registration", existing)
					}
					return refusal
				},
			})

		if !errors.Is(err, refusal) {
			t.Fatalf("err = %v, want the guard's refusal", err)
		}
		if hostname != nil {
			t.Errorf("hostname = %+v, want nil: nothing may be adopted", hostname)
		}
		if created {
			t.Error("created = true, want false")
		}
		if createAttempted {
			t.Error("a create was attempted after the guard refused")
		}
	})

	t.Run("refuses to adopt the winner of a lost create race", func(t *testing.T) {
		var listCalls int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				listCalls++
				if listCalls == 1 {
					writeJSON(t, w, http.StatusOK, APIResponse[[]CustomHostname]{
						Success: true, ResultInfo: &ResultInfo{TotalPages: 1},
					})
					return
				}
				writeJSON(t, w, http.StatusOK, APIResponse[[]CustomHostname]{
					Success: true,
					Result: []CustomHostname{{
						ID: "ch-raced", Hostname: "app.client.com",
					}},
					ResultInfo: &ResultInfo{TotalPages: 1},
				})
				return
			}
			writeJSON(t, w, http.StatusConflict, APIResponse[CustomHostname]{
				Success: false,
				Errors:  []APIError{{Code: 1406, Message: "custom hostname already exists"}},
			})
		}))
		defer server.Close()

		var guardCalls int
		client := newTestClient(t, server)
		_, _, err := client.EnsureCustomHostname(
			context.Background(), "fallback-zone", "app.client.com",
			&CreateCustomHostnameOptions{
				AdoptGuard: func(*CustomHostname) error {
					guardCalls++
					return refusal
				},
			})

		if !errors.Is(err, refusal) {
			t.Fatalf("err = %v, want the guard's refusal", err)
		}
		if guardCalls != 1 {
			t.Errorf("guard calls = %d, want 1 (the race-resolution adoption)", guardCalls)
		}
	})

	t.Run("a guard that consents adopts as before", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, http.StatusOK, APIResponse[[]CustomHostname]{
				Success: true,
				Result: []CustomHostname{{
					ID: "ch-ours", Hostname: "app.client.com",
				}},
				ResultInfo: &ResultInfo{TotalPages: 1},
			})
		}))
		defer server.Close()

		client := newTestClient(t, server)
		hostname, created, err := client.EnsureCustomHostname(
			context.Background(), "fallback-zone", "app.client.com",
			&CreateCustomHostnameOptions{AdoptGuard: func(*CustomHostname) error { return nil }})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if created {
			t.Error("created = true, want false for an existing registration")
		}
		if hostname.ID != "ch-ours" {
			t.Errorf("ID = %q, want ch-ours", hostname.ID)
		}
	})
}

func TestNewClientHonoursBaseURLOverride(t *testing.T) {
	var reached bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		writeJSON(t, w, http.StatusOK, APIResponse[[]Zone]{
			Success: true, ResultInfo: &ResultInfo{TotalPages: 1},
		})
	}))
	defer server.Close()

	client, err := NewClient(&Config{
		APIToken:  "t",
		AccountID: "a",
		ZoneID:    "z",
		BaseURL:   server.URL + "/",
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if !strings.HasPrefix(client.baseURL, server.URL) || strings.HasSuffix(client.baseURL, "/") {
		t.Errorf("baseURL = %q, want the trimmed override", client.baseURL)
	}
	if _, err := client.ListZones(context.Background()); err != nil {
		t.Fatalf("ListZones() error = %v", err)
	}
	if !reached {
		t.Error("the override was not used")
	}
}
