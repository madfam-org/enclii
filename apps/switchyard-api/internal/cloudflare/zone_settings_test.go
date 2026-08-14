package cloudflare

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newSettingsTestClient(t *testing.T, h http.HandlerFunc) (*Client, func()) {
	t.Helper()
	server := httptest.NewServer(h)
	client, err := NewClient(&Config{
		APIToken:  "t",
		AccountID: "a",
		ZoneID:    "z",
		BaseURL:   server.URL,
	})
	if err != nil {
		server.Close()
		t.Fatalf("NewClient() error = %v", err)
	}
	return client, server.Close
}

// TestSetZoneSettingUsesPATCH pins the method.
//
// Cloudflare's /zones/{id}/settings/{setting} endpoint accepts PATCH and
// rejects PUT. This client had no patch helper before zone settings existed, so
// the obvious implementation reaches for `put` — which fails at runtime against
// the real API and passes any test that does not assert the method. That is
// exactly the shape of bug this area has been full of: a call that looks right,
// returns an error the caller swallows, and changes nothing.
func TestSetZoneSettingUsesPATCH(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody struct {
		Value any `json:"value"`
	}

	client, done := newSettingsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		writeJSON(t, w, http.StatusOK, APIResponse[ZoneSetting]{
			Success: true,
			Result:  ZoneSetting{ID: "always_use_https", Value: "on", Editable: true},
		})
	})
	defer done()

	got, err := client.SetZoneSetting(context.Background(), "zone123", "always_use_https", "on")
	if err != nil {
		t.Fatalf("SetZoneSetting() error = %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %s, want PATCH — Cloudflare rejects PUT on zone settings", gotMethod)
	}
	if gotPath != "/zones/zone123/settings/always_use_https" {
		t.Errorf("path = %s, want /zones/zone123/settings/always_use_https", gotPath)
	}
	if gotBody.Value != "on" {
		t.Errorf("body value = %v, want \"on\"", gotBody.Value)
	}
	if got.StringValue() != "on" {
		t.Errorf("returned value = %q, want \"on\"", got.StringValue())
	}
}

// TestSetZoneSettingReturnsCloudflaresView guards against reporting success for
// a change that did not take. Cloudflare silently coerces some values, so the
// caller must see the API's own view, never an echo of the input.
func TestSetZoneSettingReturnsCloudflaresView(t *testing.T) {
	client, done := newSettingsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		// Asked for "on"; the API reports "off".
		writeJSON(t, w, http.StatusOK, APIResponse[ZoneSetting]{
			Success: true,
			Result:  ZoneSetting{ID: "always_use_https", Value: "off", Editable: true},
		})
	})
	defer done()

	got, err := client.SetZoneSetting(context.Background(), "z", "always_use_https", "on")
	if err != nil {
		t.Fatalf("SetZoneSetting() error = %v", err)
	}
	if got.StringValue() != "off" {
		t.Fatalf("returned %q; the client must surface Cloudflare's value, not the requested one",
			got.StringValue())
	}
}

// TestZoneSettingErrorsAreSurfaced — an unsuccessful API response must be an
// error, not a zero-valued setting that reads as "off".
func TestZoneSettingErrorsAreSurfaced(t *testing.T) {
	client, done := newSettingsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, APIResponse[ZoneSetting]{
			Success: false,
			Errors:  []APIError{{Code: 1006, Message: "Invalid zone setting"}},
		})
	})
	defer done()

	if _, err := client.SetZoneSetting(context.Background(), "z", "nope", "on"); err == nil {
		t.Fatal("expected an error when success=false; a silent zero value reads as a real setting")
	}
	if _, err := client.GetZoneSetting(context.Background(), "z", "nope"); err == nil {
		t.Fatal("expected an error from GetZoneSetting when success=false")
	}
}

// TestZoneSettingRequiresIdentifiers — an empty zone id would produce the path
// /zones//settings/x, which Cloudflare answers with something unhelpful. Fail
// in our own code where the message can say what is missing.
func TestZoneSettingRequiresIdentifiers(t *testing.T) {
	client, done := newSettingsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("no HTTP request should be made when identifiers are missing")
	})
	defer done()

	ctx := context.Background()
	if _, err := client.SetZoneSetting(ctx, "", "always_use_https", "on"); err == nil {
		t.Error("empty zone id must error")
	}
	if _, err := client.SetZoneSetting(ctx, "z", "", "on"); err == nil {
		t.Error("empty setting id must error")
	}
	if _, err := client.GetZoneSetting(ctx, "", "always_use_https"); err == nil {
		t.Error("empty zone id must error on read")
	}
}

// TestStringValueHandlesNonStrings — Cloudflare types settings inconsistently
// ("on"/"off", "1.2", objects). A renderer that returned "" for anything
// non-string would make a mismatched object setting look unset, which is the
// difference between "needs changing" and "cannot be read".
func TestStringValueHandlesNonStrings(t *testing.T) {
	if got := (&ZoneSetting{Value: "on"}).StringValue(); got != "on" {
		t.Errorf("string value = %q, want on", got)
	}
	if got := (&ZoneSetting{Value: 1.2}).StringValue(); got == "" {
		t.Error("numeric value rendered empty; an unreadable setting must not look unset")
	}
	if got := (&ZoneSetting{Value: map[string]any{"enabled": true}}).StringValue(); got == "" {
		t.Error("object value rendered empty; an unreadable setting must not look unset")
	}
	if got := (&ZoneSetting{}).StringValue(); got != "" {
		t.Errorf("nil value = %q, want empty", got)
	}
	var nilSetting *ZoneSetting
	if got := nilSetting.StringValue(); got != "" {
		t.Errorf("nil receiver = %q, want empty", got)
	}
}

// TestHTTPSPostureIsCoherent — the posture is grouped because the settings are
// only meaningful together. `always_use_https` on with a weak TLS floor still
// leaves the connection downgradeable.
func TestHTTPSPostureIsCoherent(t *testing.T) {
	byID := map[string]ZoneSettingSpec{}
	for _, s := range HTTPSPosture {
		if s.Why == "" {
			t.Errorf("%s has no Why; a plan the operator cannot read is a plan they will approve blindly", s.ID)
		}
		byID[s.ID] = s
	}
	if byID["always_use_https"].Desired != "on" {
		t.Error("always_use_https must be desired on — it is the whole point of the posture")
	}
	if byID["min_tls_version"].Desired != "1.2" {
		t.Error("min_tls_version must be at least 1.2; 1.0/1.1 are deprecated")
	}
}
