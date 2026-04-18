package audit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestJanuaClient_NoToken_ReturnsNoRowsNoError(t *testing.T) {
	c := NewJanuaClient("https://api.janua.dev", "")
	events, err := c.Fetch(context.Background(), Query{Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if events != nil {
		t.Errorf("expected nil events when token missing, got %v", events)
	}
}

func TestJanuaClient_MapsResponse(t *testing.T) {
	seen := make(chan *http.Request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r
		body := map[string]any{
			"logs": []map[string]any{
				{
					"id":         "log-1",
					"action":     "login",
					"user_id":    "sub-alice",
					"user_email": "alice@x.com",
					"timestamp":  time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
					"ip_address": "10.0.0.1",
					"details":    map[string]any{"outcome": "success"},
				},
			},
			"total":    1,
			"has_more": false,
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer server.Close()

	c := NewJanuaClient(server.URL, "test-admin-token")
	events, err := c.Fetch(context.Background(), Query{
		Limit: 10,
		Actor: "sub-alice",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	ev := events[0]
	if ev.Source != SourceJanua || ev.Category != CategoryAuth {
		t.Errorf("source/category mis-mapped: %+v", ev)
	}
	if ev.Actor != "sub-alice" || ev.ActorEmail != "alice@x.com" {
		t.Errorf("actor mis-mapped: %+v", ev)
	}
	if ev.Action != "login" {
		t.Errorf("action mis-mapped: %q", ev.Action)
	}

	// Request must carry Bearer header and ?actor=.
	req := <-seen
	if req.Header.Get("Authorization") != "Bearer test-admin-token" {
		t.Errorf("bearer header missing/wrong: %q", req.Header.Get("Authorization"))
	}
	if req.URL.Query().Get("actor") != "sub-alice" {
		t.Errorf("actor param missing; url=%s", req.URL.String())
	}
}

func TestJanuaClient_HTTPErrorSurfaces(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("backend exploded"))
	}))
	defer server.Close()

	c := NewJanuaClient(server.URL, "t")
	_, err := c.Fetch(context.Background(), Query{Limit: 10})
	if err == nil {
		t.Fatal("expected error on 500 response")
	}
	if !strings.Contains(err.Error(), "unexpected status 500") {
		t.Errorf("error should mention status; got %q", err.Error())
	}
}

func TestNexusClient_EmptyBaseURLReturnsNoRows(t *testing.T) {
	c := NewNexusClient("", "token")
	events, err := c.Fetch(context.Background(), Query{Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if events != nil {
		t.Errorf("expected nil events when baseURL empty, got %v", events)
	}
}

func TestNexusClient_SkipsWhenCallerOnlyRequestsNonSelvaSources(t *testing.T) {
	c := NewNexusClient("http://unused", "token")
	events, err := c.Fetch(context.Background(), Query{
		Limit:   10,
		Sources: []string{SourceJanua, SourceSwitchyard},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if events != nil {
		t.Errorf("expected nil events when only non-selva sources requested, got %v", events)
	}
}

func TestNexusClient_MapsUnifiedEventsThroughUnchanged(t *testing.T) {
	seen := make(chan *http.Request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r
		body := map[string]any{
			"events": []map[string]any{
				{
					"timestamp": time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
					"actor":     "sub-alice",
					"source":    "selva_secret",
					"category":  "secret",
					"action":    "write",
					"target":    "prod/karafiel/karafiel-secrets:STRIPE_SECRET_KEY",
					"outcome":   "success",
					"details":   map[string]any{"rationale": "rotation"},
				},
			},
			"next_cursor": nil,
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer server.Close()

	c := NewNexusClient(server.URL, "worker-token")
	events, err := c.Fetch(context.Background(), Query{
		Limit:   10,
		Sources: []string{SourceSelvaSecret},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	ev := events[0]
	if ev.Source != SourceSelvaSecret || ev.Category != CategorySecret {
		t.Errorf("source/category mis-mapped: %+v", ev)
	}
	req := <-seen
	if req.Header.Get("Authorization") != "Bearer worker-token" {
		t.Errorf("bearer header missing/wrong: %q", req.Header.Get("Authorization"))
	}
	// Source query must be narrowed to selva_secret.
	if got := req.URL.Query()["source"]; len(got) != 1 || got[0] != SourceSelvaSecret {
		t.Errorf("source query param mis-forwarded: %v", got)
	}
}
