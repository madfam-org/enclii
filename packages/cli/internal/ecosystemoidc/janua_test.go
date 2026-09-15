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
