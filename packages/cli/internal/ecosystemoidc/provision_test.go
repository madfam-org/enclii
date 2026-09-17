package ecosystemoidc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// Nil transports deliberately panic if any Janua request or Vault intake is
// attempted. A plan must remain usable offline, even when rotation is requested.
func TestProvisionDryRunNeverContactsJanuaOrVault(t *testing.T) {
	for _, rotate := range []bool{false, true} {
		for _, pinned := range []string{"", "jnc_fixture_pinned"} {
			reg := &Registry{Issuer: "https://auth.example.test", Platforms: map[string]Platform{
				"fixture": {IntakeTarget: "fixture/oidc", SessionIntakeTarget: "fixture/session", JanuaClient: JanuaClientSpec{ClientID: pinned}},
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
	reg := &Registry{Platforms: map[string]Platform{"fixture": {IntakeTarget: "fixture/machine", IntakeKeyMap: map[string]string{"secret": "client_secret", "client": "client_id", "workspace": "private-literal"}}}}
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
	var posted map[string]interface{}
	rotated := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/oauth/clients/admin/all":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"clients": []remoteOAuthClient{}, "total": 0})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/oauth/clients":
			_ = json.NewDecoder(r.Body).Decode(&posted)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(remoteOAuthClient{ID: "uuid-1", ClientID: "jnc_public_1", Name: "studio", IsConfidential: false, IsActive: true, RedirectURIs: []string{"https://studio.example.test"}})
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
		"studio": {JanuaClient: JanuaClientSpec{Name: "studio", ClientKey: "studio-api", Audience: "studio-api", IsConfidential: falsePtr(), RedirectURIs: []string{"https://studio.example.test"}, AllowedScopes: []string{"openid"}, GrantTypes: []string{"authorization_code"}}},
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
	reg := &Registry{Issuer: "https://auth.example.test", Platforms: map[string]Platform{
		"mapped":  {IntakeTarget: "x/y", IntakeKeyMap: map[string]string{"cid": "client_id", "csec": "client_secret", "iss": "issuer"}, JanuaClient: JanuaClientSpec{IsConfidential: falsePtr()}},
		"default": {IntakeTarget: "x/z", JanuaClient: JanuaClientSpec{IsConfidential: falsePtr()}},
		"secret":  {IntakeTarget: "x/w", JanuaClient: JanuaClientSpec{}},
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
	reg := &Registry{Platforms: map[string]Platform{
		"public":       {JanuaClient: JanuaClientSpec{IsConfidential: falsePtr()}},
		"confidential": {JanuaClient: JanuaClientSpec{}},
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
