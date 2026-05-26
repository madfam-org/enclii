package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func TestAPIClientProvisionSecretsIncludesSecretName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/admin/provision/secrets" {
			t.Fatalf("path = %s, want /v1/admin/provision/secrets", r.URL.Path)
		}
		var req types.ProvisionSecretsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Namespace != "tulana" {
			t.Fatalf("namespace = %q, want tulana", req.Namespace)
		}
		if req.SecretName != "tulana-secrets" {
			t.Fatalf("secret_name = %q, want tulana-secrets", req.SecretName)
		}
		if len(req.Secrets) != 1 || req.Secrets[0].Key != "OIDC_CLIENT_SECRET" {
			t.Fatalf("unexpected secrets payload: %#v", req.Secrets)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	result := map[string]interface{}{}
	err := client.ProvisionSecrets(context.Background(), &types.ProvisionSecretsRequest{
		Namespace:  "tulana",
		SecretName: "tulana-secrets",
		Secrets: []types.SecretEntry{
			{Key: "OIDC_CLIENT_SECRET", Value: "secret"},
		},
	}, &result)
	if err != nil {
		t.Fatalf("ProvisionSecrets returned error: %v", err)
	}
}
