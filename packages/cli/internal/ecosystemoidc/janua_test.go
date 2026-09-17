package ecosystemoidc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func pointer(s string) *string { return &s }
func machineSpec() JanuaClientSpec {
	return JanuaClientSpec{Name: "fixture-consumer", ClientKey: "fixture-consumer", Audience: "dhanam-api", OrganizationID: "fixture-org", AllowedScopes: []string{"dhanam:merchant-read"}, GrantTypes: []string{"client_credentials"}}
}
func machineRemote() remoteOAuthClient {
	return remoteOAuthClient{ID: "fixture-id", ClientID: "fixture-client", Name: "fixture-consumer", ClientKey: pointer("fixture-consumer"), Audience: pointer("dhanam-api"), OrganizationID: pointer("fixture-org"), IsConfidential: true, IsActive: true, AllowedScopes: []string{"dhanam:merchant-read"}, GrantTypes: []string{"client_credentials"}}
}
func inventoryClient(t *testing.T, clients []remoteOAuthClient) *JanuaClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("unexpected mutation: %s", r.Method)
			w.WriteHeader(500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"clients": clients, "total": len(clients)})
	}))
	t.Cleanup(server.Close)
	return &JanuaClient{BaseURL: server.URL, AdminToken: "fixture", HTTP: server.Client()}
}
func TestFindExistingNeverUsesAudienceAsIdentity(t *testing.T) {
	other := machineRemote()
	other.Name = "other-consumer"
	other.ClientKey = pointer("dhanam-api")
	spec := machineSpec()
	spec.ClientKey = "dhanam-api"
	got, err := inventoryClient(t, []remoteOAuthClient{other}).findExisting(context.Background(), spec)
	if err != nil || got != nil {
		t.Fatalf("audience collision selected: %v %v", got, err)
	}
}
func TestPinnedIdentityCannotFallBackToName(t *testing.T) {
	spec := machineSpec()
	spec.ClientID = "reviewed-pin"
	if _, err := inventoryClient(t, []remoteOAuthClient{machineRemote()}).findExisting(context.Background(), spec); err == nil {
		t.Fatal("pin ignored")
	}
}
func TestAmbiguousMachineIdentityFails(t *testing.T) {
	first, second := machineRemote(), machineRemote()
	second.ClientID = "duplicate"
	if _, err := inventoryClient(t, []remoteOAuthClient{first, second}).findExisting(context.Background(), machineSpec()); err == nil {
		t.Fatal("ambiguity ignored")
	}
}
func TestMachinePrivilegeDriftCannotMutate(t *testing.T) {
	changes := []func(*remoteOAuthClient){
		func(r *remoteOAuthClient) { r.IsActive = false },
		func(r *remoteOAuthClient) { r.OrganizationID = pointer("foreign-org") },
		func(r *remoteOAuthClient) { r.Audience = pointer("foreign-api") },
		func(r *remoteOAuthClient) { r.AllowedScopes = append(r.AllowedScopes, "admin") },
		func(r *remoteOAuthClient) { r.GrantTypes = append(r.GrantTypes, "authorization_code") },
		func(r *remoteOAuthClient) { r.IsConfidential = false },
		func(r *remoteOAuthClient) { r.RedirectURIs = []string{"https://example.test/callback"} },
	}
	for _, change := range changes {
		remote := machineRemote()
		change(&remote)
		if _, _, err := inventoryClient(t, []remoteOAuthClient{remote}).registerOrReconcile(context.Background(), machineSpec()); err == nil {
			t.Fatal("machine privilege drift accepted")
		}
	}
}
func TestMatchingMachineReusesOnlyReviewedIdentity(t *testing.T) {
	got, created, err := inventoryClient(t, []remoteOAuthClient{machineRemote()}).registerOrReconcile(context.Background(), machineSpec())
	if err != nil || created || got.ClientID != "fixture-client" {
		t.Fatalf("unexpected: %v %v %v", got, created, err)
	}
}

func loginSpec() JanuaClientSpec {
	return JanuaClientSpec{Name: "studio", ClientKey: "studio-api", Audience: "studio-api", IsConfidential: falsePtr(), RedirectURIs: []string{"https://studio.example.test", "http://localhost:5173"}, AllowedScopes: []string{"openid", "profile", "email"}, GrantTypes: []string{"authorization_code", "refresh_token"}}
}

func loginRemote() remoteOAuthClient {
	return remoteOAuthClient{ID: "uuid-studio", ClientID: "jnc_studio", Name: "studio", ClientKey: pointer("studio-api"), Audience: pointer("studio-api"), IsConfidential: false, IsActive: true, RedirectURIs: []string{"https://studio.example.test", "http://localhost:5173"}, AllowedScopes: []string{"openid", "profile", "email"}, GrantTypes: []string{"authorization_code", "refresh_token"}}
}

// adminServer serves the inventory and records PATCHes; any other mutation fails the test.
func adminServer(t *testing.T, clients []remoteOAuthClient, patches *[]map[string]interface{}) *JanuaClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/oauth/clients/admin/all":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"clients": clients, "total": len(clients)})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/oauth/clients/uuid-studio":
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			*patches = append(*patches, body)
			updated := clients[0]
			if v, ok := body["redirect_uris"].([]interface{}); ok {
				updated.RedirectURIs = nil
				for _, u := range v {
					updated.RedirectURIs = append(updated.RedirectURIs, u.(string))
				}
			}
			_ = json.NewEncoder(w).Encode(updated)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	t.Cleanup(server.Close)
	return &JanuaClient{BaseURL: server.URL, AdminToken: "fixture", HTTP: server.Client()}
}

func TestAdminPathReconcilesDriftedLoginClient(t *testing.T) {
	stale := loginRemote()
	stale.RedirectURIs = []string{"https://old.example.test/auth/callback"}
	stale.AllowedScopes = []string{"openid"}
	var patches []map[string]interface{}
	remote, created, err := adminServer(t, []remoteOAuthClient{stale}, &patches).registerOrReconcile(context.Background(), loginSpec())
	if err != nil || created {
		t.Fatalf("reconcile failed: %v created=%t", err, created)
	}
	if len(patches) != 1 {
		t.Fatalf("expected one PATCH, got %d", len(patches))
	}
	if _, ok := patches[0]["redirect_uris"]; !ok {
		t.Fatalf("redirect_uris not reconciled: %v", patches[0])
	}
	if _, ok := patches[0]["allowed_scopes"]; !ok {
		t.Fatalf("allowed_scopes not reconciled: %v", patches[0])
	}
	if _, ok := patches[0]["grant_types"]; ok {
		t.Fatalf("grant_types were in sync and must not be patched: %v", patches[0])
	}
	if !sameStrings(remote.reconciled, []string{"allowed_scopes", "redirect_uris"}) {
		t.Fatalf("reconciled fields not reported: %v", remote.reconciled)
	}
	if !sameStrings(remote.RedirectURIs, loginSpec().RedirectURIs) {
		t.Fatalf("returned client still stale: %v", remote.RedirectURIs)
	}
}

func TestAdminPathLeavesInSyncLoginClientAlone(t *testing.T) {
	var patches []map[string]interface{}
	remote, created, err := adminServer(t, []remoteOAuthClient{loginRemote()}, &patches).registerOrReconcile(context.Background(), loginSpec())
	if err != nil || created || len(patches) != 0 || len(remote.reconciled) != 0 {
		t.Fatalf("in-sync client was touched: err=%v created=%t patches=%v reconciled=%v", err, created, patches, remote.reconciled)
	}
}

func TestAdminPathRefusesConfidentialityFlipAndInactiveClients(t *testing.T) {
	flipped := loginRemote()
	flipped.IsConfidential = true
	inactive := loginRemote()
	inactive.IsActive = false
	for _, remote := range []remoteOAuthClient{flipped, inactive} {
		var patches []map[string]interface{}
		if _, _, err := adminServer(t, []remoteOAuthClient{remote}, &patches).registerOrReconcile(context.Background(), loginSpec()); err == nil {
			t.Fatal("refusal expected")
		}
		if len(patches) != 0 {
			t.Fatalf("refused client was patched: %v", patches)
		}
	}
}
