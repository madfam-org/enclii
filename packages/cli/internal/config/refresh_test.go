package config

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeJWT builds an unsigned token with the given `exp`. The CLI never
// verifies signatures (the API does), so the shape is all that matters here.
func fakeJWT(t *testing.T, exp time.Time, marker string) string {
	t.Helper()
	enc := func(v any) string {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	return enc(map[string]string{"alg": "none"}) + "." +
		enc(map[string]any{"exp": exp.Unix(), "jti": marker}) + ".sig"
}

// refreshEnv gives a test its own home directory, a clean environment, a
// captured warning stream and a fresh "already warned" set.
func refreshEnv(t *testing.T) (home string, warnings *bytes.Buffer) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	for _, name := range []string{"ENCLII_API_TOKEN", "ENCLII_TOKEN", "ENCLII_PROFILE", "ENCLII_LOG_LEVEL", "ENCLII_OIDC_CLIENT_ID"} {
		t.Setenv(name, "")
	}
	warnings = &bytes.Buffer{}
	prevOut := warnOut
	warnOut = warnings
	warnedMu.Lock()
	prevWarned := warned
	warned = map[string]bool{}
	warnedMu.Unlock()
	t.Cleanup(func() {
		warnOut = prevOut
		warnedMu.Lock()
		warned = prevWarned
		warnedMu.Unlock()
		_ = SetProfile("")
	})
	return home, warnings
}

// tokenEndpoint serves /api/v1/oauth/token with a fixed answer and counts calls.
func tokenEndpoint(t *testing.T, status int, body string) (issuer string, calls *atomic.Int32) {
	t.Helper()
	calls = &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/oauth/token" {
			http.NotFound(w, r)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, calls
}

func TestTokenExpiry_PrefersTheTokensOwnExp(t *testing.T) {
	// The production disagreement: `expires_in` 3600 on a token valid 8 h.
	exp := time.Now().Add(8 * time.Hour).Truncate(time.Second)
	if got := TokenExpiry(fakeJWT(t, exp, "a"), 3600); !got.Equal(exp) {
		t.Errorf("TokenExpiry = %v, want the JWT exp %v", got, exp)
	}
	// A token that dies sooner than announced is not trusted past its exp.
	soon := time.Now().Add(10 * time.Minute).Truncate(time.Second)
	if got := TokenExpiry(fakeJWT(t, soon, "b"), 3600); !got.Equal(soon) {
		t.Errorf("TokenExpiry = %v, want the JWT exp %v", got, soon)
	}
}

func TestTokenExpiry_OpaqueTokenUsesExpiresIn(t *testing.T) {
	for _, token := range []string{"opaque", "a.b.c", "x." + base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"u"}`)) + ".s"} {
		got := TokenExpiry(token, 3600)
		if d := time.Until(got); d < 59*time.Minute || d > 61*time.Minute {
			t.Errorf("TokenExpiry(%q) is %v away, want about one hour", token, d)
		}
	}
}

func TestReloadCredentials_RefreshRefusedIsReported(t *testing.T) {
	_, warnings := refreshEnv(t)
	issuer, calls := tokenEndpoint(t, http.StatusUnauthorized, `{"detail":"Authentication session is no longer active"}`)

	access := fakeJWT(t, time.Now().Add(-time.Minute), "expired-access")
	const refresh = "refresh-token-secret-value"
	if err := saveCredentials(&Credentials{
		AccessToken: access, RefreshToken: refresh, TokenType: "Bearer",
		ExpiresAt: time.Now().Add(-time.Minute), Issuer: issuer,
	}); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.APIToken != "" {
		t.Errorf("an expired token must not be offered, got one")
	}
	if cfg.RefreshError == nil {
		t.Fatal("RefreshError is nil: the refusal was discarded")
	}
	out := warnings.String()
	for _, want := range []string{"HTTP 401", "Authentication session is no longer active", "enclii login", `"default"`} {
		if !strings.Contains(out, want) {
			t.Errorf("warning %q does not contain %q", out, want)
		}
	}
	if strings.Count(out, "\n") != 1 {
		t.Errorf("want exactly one line, got %q", out)
	}
	for _, secret := range []string{access, refresh} {
		if strings.Contains(out, secret) || strings.Contains(cfg.RefreshError.Error(), secret) {
			t.Error("a token value reached the warning")
		}
	}

	// A second reload in the same process (root's --profile handling) tries
	// again but does not repeat the line.
	cfg.ReloadCredentials()
	if calls.Load() != 2 {
		t.Errorf("token endpoint calls = %d, want 2", calls.Load())
	}
	if strings.Count(warnings.String(), "\n") != 1 {
		t.Errorf("warning repeated: %q", warnings.String())
	}
}

func TestReloadCredentials_NamedProfileHintNamesTheProfile(t *testing.T) {
	_, warnings := refreshEnv(t)
	issuer, _ := tokenEndpoint(t, http.StatusBadRequest, `{"error":"invalid_grant","error_description":"Invalid refresh token"}`)
	if err := saveCredentialsFor("admin", &Credentials{
		AccessToken: "opaque", RefreshToken: "rt", ExpiresAt: time.Now().Add(-time.Hour), Issuer: issuer,
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENCLII_PROFILE", "admin")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	out := warnings.String()
	for _, want := range []string{"HTTP 400", "invalid_grant: Invalid refresh token", "enclii --profile admin login"} {
		if !strings.Contains(out, want) {
			t.Errorf("warning %q does not contain %q", out, want)
		}
	}
}

func TestReloadCredentials_RefusedRefreshKeepsATokenThatStillWorks(t *testing.T) {
	// Inside the refresh window but not expired: the command still runs with
	// the token it has, and its output is unchanged (no warning yet).
	_, warnings := refreshEnv(t)
	issuer, calls := tokenEndpoint(t, http.StatusUnauthorized, `{"detail":"Authentication session is no longer active"}`)
	access := fakeJWT(t, time.Now().Add(30*time.Second), "almost")
	if err := saveCredentials(&Credentials{
		AccessToken: access, RefreshToken: "rt", ExpiresAt: time.Now().Add(30 * time.Second), Issuer: issuer,
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || cfg.RefreshError == nil {
		t.Fatalf("calls = %d, RefreshError = %v; want one refused refresh", calls.Load(), cfg.RefreshError)
	}
	if cfg.APIToken != access {
		t.Error("the still-valid token was dropped")
	}
	if warnings.Len() != 0 {
		t.Errorf("unexpected warning while the token works: %q", warnings.String())
	}
}

func TestReloadCredentials_StoredOneHourExpiryIsCorrectedFromTheToken(t *testing.T) {
	// A credentials file written by an older CLI: the token is good for seven
	// more hours, the file says it expired a minute ago. No refresh, no login.
	_, warnings := refreshEnv(t)
	issuer, calls := tokenEndpoint(t, http.StatusInternalServerError, `{}`)
	exp := time.Now().Add(7 * time.Hour).Truncate(time.Second)
	access := fakeJWT(t, exp, "long-lived")
	if err := saveCredentials(&Credentials{
		AccessToken: access, RefreshToken: "rt", ExpiresAt: time.Now().Add(-time.Minute), Issuer: issuer,
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIToken != access {
		t.Error("a token the API still accepts was treated as expired")
	}
	if calls.Load() != 0 {
		t.Errorf("token endpoint calls = %d, want 0", calls.Load())
	}
	if cfg.RefreshError != nil || warnings.Len() != 0 {
		t.Errorf("RefreshError = %v, warnings = %q", cfg.RefreshError, warnings.String())
	}
	// The file now tells other readers (scripts) the truth.
	onDisk, err := loadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if !onDisk.ExpiresAt.Equal(exp) {
		t.Errorf("stored expires_at = %v, want %v", onDisk.ExpiresAt, exp)
	}
}

func TestReloadCredentials_SuccessfulRefreshStoresTheTokensOwnExpiry(t *testing.T) {
	_, warnings := refreshEnv(t)
	exp := time.Now().Add(8 * time.Hour).Truncate(time.Second)
	fresh := fakeJWT(t, exp, "fresh")
	issuer, _ := tokenEndpoint(t, http.StatusOK,
		fmt.Sprintf(`{"access_token":%q,"refresh_token":"rotated","token_type":"Bearer","expires_in":3600}`, fresh))
	if err := saveCredentials(&Credentials{
		AccessToken: fakeJWT(t, time.Now().Add(-time.Minute), "old"), RefreshToken: "rt",
		ExpiresAt: time.Now().Add(-time.Minute), Issuer: issuer,
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIToken != fresh || cfg.RefreshError != nil || warnings.Len() != 0 {
		t.Fatalf("refresh not applied cleanly: err=%v warnings=%q", cfg.RefreshError, warnings.String())
	}
	onDisk, err := loadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if onDisk.AccessToken != fresh || onDisk.RefreshToken != "rotated" || !onDisk.ExpiresAt.Equal(exp) {
		t.Errorf("stored credentials not updated: expires_at=%v", onDisk.ExpiresAt)
	}
}

func TestReloadCredentials_UnreachableIssuerIsReported(t *testing.T) {
	_, warnings := refreshEnv(t)
	// Port 1 on loopback: nothing listens there, the connection is refused.
	if err := saveCredentials(&Credentials{
		AccessToken: "opaque", RefreshToken: "rt", ExpiresAt: time.Now().Add(-time.Hour),
		Issuer: "http://127.0.0.1:1",
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RefreshError == nil || !strings.Contains(warnings.String(), "could not be reached") {
		t.Errorf("RefreshError = %v, warnings = %q", cfg.RefreshError, warnings.String())
	}
}

func TestProfileAPIToken_ExpiredProfileSaysWhyRefreshFailed(t *testing.T) {
	refreshEnv(t)
	issuer, _ := tokenEndpoint(t, http.StatusUnauthorized, `{"detail":"Authentication session is no longer active"}`)
	if err := saveCredentialsFor("admin", &Credentials{
		AccessToken: "opaque", RefreshToken: "rt", ExpiresAt: time.Now().Add(-time.Hour), Issuer: issuer,
	}); err != nil {
		t.Fatal(err)
	}
	_, err := ProfileAPIToken("admin")
	if err == nil {
		t.Fatal("want an error for an expired profile")
	}
	for _, want := range []string{"HTTP 401", "Authentication session is no longer active", "enclii --profile admin login"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}

func TestRefreshRefusal(t *testing.T) {
	tests := []struct {
		name, body, want string
	}{
		{"fastapi detail", `{"detail":"invalid_grant: Invalid refresh token"}`, "HTTP 400 (invalid_grant: Invalid refresh token)"},
		{"oauth error pair", `{"error":"invalid_grant","error_description":"expired"}`, "HTTP 400 (invalid_grant: expired)"},
		{"oauth error only", `{"error":"invalid_client"}`, "HTTP 400 (invalid_client)"},
		{"structured detail is not echoed", `{"detail":[{"loc":["body"],"msg":"x"}]}`, "HTTP 400"},
		{"not json", `<html>bad gateway</html>`, "HTTP 400"},
		{"empty", ``, "HTTP 400"},
		{"multi-line collapses", "{\"detail\":\"line one\\nline two\"}", "HTTP 400 (line one line two)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := refreshRefusal(400, []byte(tt.body)).Error()
			if !strings.HasSuffix(got, tt.want) {
				t.Errorf("got %q, want suffix %q", got, tt.want)
			}
			if strings.Contains(got, "\n") {
				t.Errorf("reason spans lines: %q", got)
			}
		})
	}

	t.Run("a token echoed by the server is redacted", func(t *testing.T) {
		got := refreshRefusal(400, []byte(`{"detail":"bad token tok-123 given"}`), "tok-123", "").Error()
		if strings.Contains(got, "tok-123") || !strings.Contains(got, "[redacted]") {
			t.Errorf("got %q", got)
		}
	})
	t.Run("long reasons are cut", func(t *testing.T) {
		got := refreshRefusal(500, []byte(`{"detail":"`+strings.Repeat("x", 1000)+`"}`)).Error()
		if len([]rune(got)) > maxRefreshReason+60 {
			t.Errorf("reason not bounded: %d runes", len([]rune(got)))
		}
	})
}
