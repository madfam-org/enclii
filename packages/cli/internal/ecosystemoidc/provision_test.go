package ecosystemoidc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func fixtureLoginSpec() JanuaClientSpec {
	return JanuaClientSpec{Name: "fixture-login", ClientKey: "fixture-login", Audience: "fixture-login", RedirectURIs: []string{"https://fixture.example.test/auth/callback"}, AllowedScopes: []string{"openid"}, GrantTypes: []string{"authorization_code"}}
}

// Nil transports deliberately panic if any Janua request or Vault intake is
// attempted. A plan must remain usable offline, even when rotation is requested.
func TestProvisionDryRunNeverContactsJanuaOrVault(t *testing.T) {
	for _, rotate := range []bool{false, true} {
		for _, pinned := range []string{"", "jnc_fixture_pinned"} {
			spec := fixtureLoginSpec()
			spec.ClientID = pinned
			reg := &Registry{Issuer: "https://auth.example.test", Platforms: map[string]Platform{
				"fixture": {IntakeTarget: "fixture/oidc", SessionIntakeTarget: "fixture/session", JanuaClient: spec},
			}}
			result, err := ProvisionPlatform(context.Background(), reg, nil, nil, ProvisionOptions{PlatformID: "fixture", DryRun: true, RotateIfMissing: rotate})
			if err != nil {
				t.Fatal(err)
			}
			if !result.DryRun || result.Created || result.RotatedSecret || result.IntakeID != "" || result.SessionIntakeID != "" {
				t.Fatalf("dry run claimed a mutation: %#v", result)
			}
			if result.JanuaClientID != pinned {
				t.Fatal("plan must not invent a remote client identity")
			}
			if !reflect.DeepEqual(result.KeysWritten, []string{"OIDC_CLIENT_ID", "OIDC_CLIENT_SECRET", "OIDC_ISSUER"}) {
				t.Fatalf("unexpected keys: %v", result.KeysWritten)
			}
			if !reflect.DeepEqual(result.SessionKeys, []string{"NEXTAUTH_SECRET", "SESSION_SECRET"}) {
				t.Fatalf("unexpected session keys: %v", result.SessionKeys)
			}
		}
	}
}

func TestProvisionDryRunMappedKeysAndInvalidTargets(t *testing.T) {
	reg := &Registry{Platforms: map[string]Platform{"fixture": {IntakeTarget: "fixture/machine", IntakeKeyMap: map[string]string{"secret": "client_secret", "client": "client_id", "workspace": "private-literal"}, JanuaClient: fixtureLoginSpec()}}}
	result, err := ProvisionPlatform(context.Background(), reg, nil, nil, ProvisionOptions{PlatformID: "fixture", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.KeysWritten, []string{"client", "secret", "workspace"}) {
		t.Fatalf("unexpected keys: %v", result.KeysWritten)
	}
	if len(result.SessionKeys) != 0 {
		t.Fatal("invented session credentials")
	}
	for _, id := range []string{"missing", "empty"} {
		reg.Platforms["empty"] = Platform{}
		if _, err := ProvisionPlatform(context.Background(), reg, nil, nil, ProvisionOptions{PlatformID: id, DryRun: true}); err == nil {
			t.Fatal("invalid plan accepted")
		}
	}
}

func falsePtr() *bool { v := false; return &v }

// A public login client (is_confidential: false, no org) has no secret. The
// provisioner must register it and stop: never resolve or rotate a secret,
// never intake, and never require an intake target.
func TestProvisionPublicLoginClientRegistersAndStops(t *testing.T) {
	spec := fixtureLoginSpec()
	spec.Name = "studio"
	spec.ClientKey = "studio-api"
	spec.Audience = "studio-api"
	spec.IsConfidential = falsePtr()
	spec.RedirectURIs = []string{"https://studio.example.test"}
	var posted map[string]interface{}
	rotated := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/oauth/clients/admin/all":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"clients": []remoteOAuthClient{}, "total": 0})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/oauth/clients":
			_ = json.NewDecoder(r.Body).Decode(&posted)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(func() remoteOAuthClient { r := remoteFor(spec); r.ClientID = "jnc_public_1"; return r }())
		case r.Method == http.MethodPost:
			rotated = true
			w.WriteHeader(500)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	t.Cleanup(server.Close)
	janua := &JanuaClient{BaseURL: server.URL, AdminToken: "fixture", HTTP: server.Client()}

	reg := &Registry{Issuer: "https://auth.example.test", Platforms: map[string]Platform{
		"studio": {JanuaClient: spec},
	}}
	// nil submitter: any intake attempt panics the test.
	result, err := ProvisionPlatform(context.Background(), reg, janua, nil, ProvisionOptions{PlatformID: "studio", Reason: "test", RotateIfMissing: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created || result.JanuaClientID != "jnc_public_1" || !result.PublicLogin {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.RotatedSecret || rotated || result.IntakeID != "" || len(result.KeysWritten) != 0 {
		t.Fatalf("public client got a secret or an intake: %#v rotated=%t", result, rotated)
	}
	if posted["is_confidential"] != false {
		t.Fatalf("registered as confidential: %v", posted)
	}
	if uris, _ := posted["redirect_uris"].([]interface{}); len(uris) != 1 || uris[0] != "https://studio.example.test" {
		t.Fatalf("redirect_uris not sent: %v", posted["redirect_uris"])
	}
}

// With an intake target, a public client's id and issuer are delivered but a
// secret-sourced key is never written — not even empty.
func TestProvisionPublicLoginClientIntakeCarriesNoSecretKey(t *testing.T) {
	publicSpec := fixtureLoginSpec()
	publicSpec.IsConfidential = falsePtr()
	reg := &Registry{Issuer: "https://auth.example.test", Platforms: map[string]Platform{
		"mapped":  {IntakeTarget: "x/y", IntakeKeyMap: map[string]string{"cid": "client_id", "csec": "client_secret", "iss": "issuer"}, JanuaClient: publicSpec},
		"default": {IntakeTarget: "x/z", JanuaClient: publicSpec},
		"secret":  {IntakeTarget: "x/w", JanuaClient: fixtureLoginSpec()},
	}}
	for id, want := range map[string][]string{
		"mapped":  {"cid", "iss"},
		"default": {"OIDC_CLIENT_ID", "OIDC_ISSUER"},
		"secret":  {"OIDC_CLIENT_ID", "OIDC_CLIENT_SECRET", "OIDC_ISSUER"},
	} {
		result, err := ProvisionPlatform(context.Background(), reg, nil, nil, ProvisionOptions{PlatformID: id, DryRun: true})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(result.KeysWritten, want) {
			t.Fatalf("%s: keys %v, want %v", id, result.KeysWritten, want)
		}
	}
}

// A public client without an intake target is a valid plan; a confidential one
// without a target still is not.
func TestProvisionIntakeTargetOptionalOnlyForPublicLoginClients(t *testing.T) {
	publicSpec := fixtureLoginSpec()
	publicSpec.IsConfidential = falsePtr()
	reg := &Registry{Platforms: map[string]Platform{
		"public":       {JanuaClient: publicSpec},
		"confidential": {JanuaClient: fixtureLoginSpec()},
		"machine":      {JanuaClient: JanuaClientSpec{IsConfidential: falsePtr(), OrganizationID: "org"}},
	}}
	result, err := ProvisionPlatform(context.Background(), reg, nil, nil, ProvisionOptions{PlatformID: "public", DryRun: true})
	if err != nil || len(result.KeysWritten) != 0 || !result.PublicLogin {
		t.Fatalf("public plan rejected or invented keys: %v %#v", err, result)
	}
	for _, id := range []string{"confidential", "machine"} {
		if _, err := ProvisionPlatform(context.Background(), reg, nil, nil, ProvisionOptions{PlatformID: id, DryRun: true}); err == nil {
			t.Fatalf("%s without intake_target accepted", id)
		}
	}
}

func TestIntakeVerifiesOnlyExactRetainedTargetAndKeys(t *testing.T) {
	for _, failure := range []string{"", "wrong-target", "wrong-keys", "zero-version", "unavailable", "unknown-submit"} {
		t.Run(failure, func(t *testing.T) {
			posts, reads := 0, 0
			submitter := HTTPIntakeSubmitter{
				Post: func(_ context.Context, path string, body []byte) ([]byte, error) {
					posts++
					if path != "/v1/secrets/intake" {
						t.Fatal("unexpected write route")
					}
					var request map[string]interface{}
					if err := json.Unmarshal(body, &request); err != nil {
						t.Fatal(err)
					}
					if request["target"] != "fixture/custody" {
						t.Fatal("wrong declared target")
					}
					if failure == "unknown-submit" {
						return nil, fmt.Errorf("synthetic connection loss")
					}
					return []byte(`{"intake_id":"int_fixture"}`), nil
				},
				Get: func(_ context.Context, path string) ([]byte, error) {
					reads++
					if path != "/v1/secrets/intake/int_fixture" {
						t.Fatal("not the retained fixed ID")
					}
					if failure == "unavailable" {
						return nil, fmt.Errorf("synthetic unavailable")
					}
					target := "fixture/custody"
					keys := []string{"CLIENT", "CREDENTIAL"}
					version := 4
					if failure == "wrong-target" {
						target = "foreign/custody"
					}
					if failure == "wrong-keys" {
						keys = []string{"CLIENT"}
					}
					if failure == "zero-version" {
						version = 0
					}
					return json.Marshal(map[string]interface{}{"intake_id": "int_fixture", "target_id": target, "status": "ready", "keys_written": keys, "vault_version": version, "external_secret_refreshed": false})
				},
			}
			id, err := submitter.SubmitIntake(context.Background(), "fixture/custody", "synthetic fixture", map[string]string{"client": "jnc_synthetic", "credential": "synthetic-secret"})
			if failure == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("unverified custody accepted")
			}
			if posts != 1 {
				t.Fatal("intake was retried")
			}
			if failure == "unknown-submit" {
				if id != "" || reads != 0 {
					t.Fatal("unknown submit adopted an ID")
				}
			} else if id != "int_fixture" || reads != 1 {
				t.Fatal("known intake ID was lost or read more than once")
			}
		})
	}
}

func TestCreaIntakeUsesActualUppercaseReportedKeys(t *testing.T) {
	for _, platformID := range []string{"crea-sim", "crea-sim-mcp"} {
		for _, failure := range []string{"", "wrong-target", "duplicate", "colliding-case", "foreign", "lowercase-report"} {
			t.Run(platformID+"/"+failure, func(t *testing.T) {
				p := embedded(t).Platforms[platformID]
				values := buildIntakeValues("https://auth.example.test", "jnc_fixture_client", "synthetic-credential", p)
				posts, reads := 0, 0
				submitter := HTTPIntakeSubmitter{
					Post: func(_ context.Context, path string, body []byte) ([]byte, error) {
						posts++
						if path != "/v1/secrets/intake" {
							t.Fatal("wrong write route")
						}
						return []byte(`{"intake_id":"int_fixture"}`), nil
					},
					Get: func(_ context.Context, path string) ([]byte, error) {
						reads++
						if path != "/v1/secrets/intake/int_fixture" {
							t.Fatal("wrong retained ID")
						}
						keys := mapKeys(values)
						for i := range keys {
							keys[i] = strings.ToUpper(strings.TrimSpace(keys[i]))
						}
						target := p.IntakeTarget
						switch failure {
						case "wrong-target":
							target = "foreign/oidc"
						case "duplicate":
							keys[1] = keys[0]
						case "colliding-case":
							keys[1] = strings.ToLower(keys[0])
						case "foreign":
							keys[1] = "FOREIGN_CREDENTIAL"
						case "lowercase-report":
							keys = mapKeys(values)
						}
						// Exact server DTO fields; lowercase Vault properties are not keys_written.
						return json.Marshal(map[string]interface{}{"intake_id": "int_fixture", "target_id": target, "target_label": "synthetic training OAuth custody", "status": "ready", "vault_path": "secret/crea-sim-gateway", "namespace": "crea-sim", "external_secret": "crea-sim-gateway", "keys_written": keys, "vault_version": 4, "external_secret_refreshed": true, "created_at": "2026-10-13T00:00:00Z", "expires_at": "2026-10-13T01:00:00Z"})
					},
				}
				id, err := submitter.SubmitIntake(context.Background(), p.IntakeTarget, "synthetic native-shaped status", values)
				if failure == "" {
					if err != nil {
						t.Fatal("native uppercase report refused")
					}
				} else if err == nil {
					t.Fatal("foreign or noncanonical key report accepted")
				}
				if id != "int_fixture" || posts != 1 || reads != 1 {
					t.Fatal("confirmed ID lost or write retried")
				}
			})
		}
	}
}

type sessionStatusRefusal struct{ calls int }

func (s *sessionStatusRefusal) SubmitIntake(_ context.Context, _ string, _ string, _ map[string]string) (string, error) {
	s.calls++
	if s.calls == 1 {
		return "int_primary", nil
	}
	return "int_session", fmt.Errorf("synthetic retained receipt/status mismatch")
}
func TestSessionStatusFailureRetainsConfirmedReceipt(t *testing.T) {
	spec := humanFixtureSpec(t)
	provider := newProviderFixture(t, spec, nil)
	p := embedded(t).Platforms["crea-sim"]
	p.JanuaClient = spec
	p.SessionIntakeTarget = "fixture/session"
	submitter := &sessionStatusRefusal{}
	result, err := ProvisionPlatform(context.Background(), &Registry{Issuer: "https://auth.example.test", Platforms: map[string]Platform{"fixture": p}}, provider.client(false), submitter, ProvisionOptions{PlatformID: "fixture", Reason: "synthetic session custody"})
	if err == nil || result.IntakeID != "int_primary" || result.SessionIntakeID != "int_session" || submitter.calls != 2 {
		t.Fatal("known session receipt dropped on status refusal or retry occurred")
	}
	if len(result.SessionKeys) != 0 {
		t.Fatal("unverified session keys accredited")
	}
}

func TestIntakeRequestKeyCollisionsRefuseBeforeWrite(t *testing.T) {
	for _, keys := range [][]string{{"client", "CLIENT"}, {" client ", "CLIENT"}, {"client-key", "client_key"}, {"client__key", "CLIENT_KEY"}, {""}, {"\x00"}} {
		calls := 0
		submitter := HTTPIntakeSubmitter{Post: func(context.Context, string, []byte) ([]byte, error) {
			calls++
			return nil, fmt.Errorf("must not write")
		}}
		values := map[string]string{}
		for _, key := range keys {
			values[key] = "synthetic-value"
		}
		id, err := submitter.SubmitIntake(context.Background(), "fixture/keys", "synthetic collision refusal", values)
		if err == nil || id != "" || calls != 0 {
			t.Fatal("colliding or invalid request keys reached intake")
		}
	}
}

func TestIntakeKeyReportingUsesExactUpperTrimNotVaultAliases(t *testing.T) {
	for _, tc := range []struct {
		requested, reported string
		valid               bool
	}{
		{" client_id ", "CLIENT_ID", true},
		{"client-id", "CLIENT-ID", true},
		{"client-id", "CLIENT_ID", false},
		{"client_id", " CLIENT_ID ", false},
	} {
		posts := 0
		submitter := HTTPIntakeSubmitter{
			Post: func(context.Context, string, []byte) ([]byte, error) {
				posts++
				return []byte(`{"intake_id":"int_fixture"}`), nil
			},
			Get: func(context.Context, string) ([]byte, error) {
				return json.Marshal(map[string]interface{}{"intake_id": "int_fixture", "target_id": "fixture/keys", "status": "ready", "keys_written": []string{tc.reported}, "vault_version": 4})
			},
		}
		id, err := submitter.SubmitIntake(context.Background(), "fixture/keys", "synthetic key reporting", map[string]string{tc.requested: "synthetic-value"})
		if (err == nil) != tc.valid || id != "int_fixture" || posts != 1 {
			t.Fatal("report did not obey exact server canonicalization or write repeated")
		}
	}
}

func TestNativeShapedSessionStatusFailureRetainsBothIntakeIDs(t *testing.T) {
	spec := humanFixtureSpec(t)
	provider := newProviderFixture(t, spec, nil)
	p := embedded(t).Platforms["crea-sim"]
	p.JanuaClient = spec
	p.SessionIntakeTarget = "fixture/session"
	posts, reads := 0, 0
	submitter := HTTPIntakeSubmitter{
		Post: func(_ context.Context, path string, body []byte) ([]byte, error) {
			if path != "/v1/secrets/intake" {
				t.Fatal("wrong write route")
			}
			posts++
			var request struct {
				Target string `json:"target"`
			}
			if json.Unmarshal(body, &request) != nil {
				t.Fatal("invalid fixture request")
			}
			if posts == 1 && request.Target == p.IntakeTarget {
				return []byte(`{"intake_id":"int_primary"}`), nil
			}
			if posts == 2 && request.Target == p.SessionIntakeTarget {
				return []byte(`{"intake_id":"int_session"}`), nil
			}
			t.Fatal("extra or foreign intake write")
			return nil, nil
		},
		Get: func(_ context.Context, path string) ([]byte, error) {
			if path == "/v1/secrets/intake/targets" {
				return json.Marshal(map[string]interface{}{"targets": []map[string]string{{"id": p.IntakeTarget}, {"id": p.SessionIntakeTarget}}})
			}
			reads++
			if path == "/v1/secrets/intake/int_primary" {
				return json.Marshal(map[string]interface{}{"intake_id": "int_primary", "target_id": p.IntakeTarget, "status": "ready", "keys_written": []string{"CREA_SIM_JANUA_UI_ID", "CREA_SIM_JANUA_UI_SECRET"}, "vault_version": 4, "external_secret_refreshed": true})
			}
			if path == "/v1/secrets/intake/int_session" {
				return json.Marshal(map[string]interface{}{"intake_id": "int_session", "target_id": "foreign/session", "status": "ready", "keys_written": []string{"NEXTAUTH_SECRET", "SESSION_SECRET"}, "vault_version": 5, "external_secret_refreshed": true})
			}
			t.Fatal("wrong read-only receipt path")
			return nil, nil
		},
	}
	result, err := ProvisionPlatform(context.Background(), &Registry{Issuer: "https://auth.example.test", Platforms: map[string]Platform{"fixture": p}}, provider.client(false), submitter, ProvisionOptions{PlatformID: "fixture", Reason: "synthetic session native status"})
	if err == nil || result.IntakeID != "int_primary" || result.SessionIntakeID != "int_session" || posts != 2 || reads != 2 || len(result.SessionKeys) != 0 {
		t.Fatal("session mismatch lost a confirmed receipt, retried or accredited unverified keys")
	}
	if provider.posts != 1 || provider.rotations != 0 {
		t.Fatal("custody status failure recreated or rotated the provider client")
	}
}

// A refused intake identity (ADR-003: only the platform_admin operator may
// write) or an unknown target must stop the run before Janua creates a client
// or rotates a secret, or the one-time secret is lost with nowhere to land.
func TestIntakePreflightRefusalLeavesJanuaUntouched(t *testing.T) {
	cases := map[string]func(context.Context, string) ([]byte, error){
		"identity refused": func(context.Context, string) ([]byte, error) {
			return nil, fmt.Errorf("API error 403: platform_admin rank required (ADR-003)")
		},
		"target missing": func(context.Context, string) ([]byte, error) {
			return []byte(`{"targets":[{"id":"other/oidc"}]}`), nil
		},
		"listing unreadable": func(context.Context, string) ([]byte, error) {
			return []byte(`{"targets":[{"id":"a"}],"targets":[]}`), nil
		},
	}
	for name, get := range cases {
		t.Run(name, func(t *testing.T) {
			spec := humanFixtureSpec(t)
			provider := newProviderFixture(t, spec, nil)
			p := embedded(t).Platforms["crea-sim"]
			p.JanuaClient = spec
			submitter := HTTPIntakeSubmitter{
				Post: func(context.Context, string, []byte) ([]byte, error) {
					t.Fatal("intake written after a refused preflight")
					return nil, nil
				},
				Get: get,
			}
			_, err := ProvisionPlatform(context.Background(), &Registry{Issuer: "https://auth.example.test", Platforms: map[string]Platform{"fixture": p}}, provider.client(false), submitter, ProvisionOptions{PlatformID: "fixture", Reason: "synthetic preflight", RotateIfMissing: true})
			if err == nil || !strings.Contains(err.Error(), "before any Janua change") {
				t.Fatalf("preflight did not refuse first: %v", err)
			}
			if provider.posts != 0 || provider.rotations != 0 {
				t.Fatal("Janua was changed before the intake identity was proven")
			}
		})
	}
}
