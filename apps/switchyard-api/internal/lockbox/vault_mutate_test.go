package lockbox

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVaultMutationRetriesCASWithFreshTenantMap(t *testing.T) {
	version := 1
	reads := 0
	writes := 0
	data := map[string]interface{}{"feed_map": "other=preserved"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "fixture-key" {
			t.Error("missing authentication")
		}
		if r.Method == http.MethodGet {
			reads++
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": map[string]interface{}{"data": data, "metadata": map[string]int{"version": version}}})
			return
		}
		writes++
		var body struct {
			Data    map[string]interface{} `json:"data"`
			Options struct {
				CAS int `json:"cas"`
			} `json:"options"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if writes == 1 {
			data["feed_map"] = "other=preserved,concurrent=retained"
			version++
			w.WriteHeader(400)
			return
		}
		if body.Options.CAS != version {
			t.Errorf("stale CAS %d, expected %d", body.Options.CAS, version)
			w.WriteHeader(400)
			return
		}
		data = body.Data
		version++
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": map[string]int{"version": version}})
	}))
	defer server.Close()
	client := &VaultClient{address: server.URL, token: "fixture-key", httpClient: server.Client(), enabled: true}
	got, err := client.MutateSecretData(context.Background(), "secret/fixture", func(current map[string]interface{}) (map[string]interface{}, error) {
		return map[string]interface{}{"feed_map": current["feed_map"].(string) + ",new=added"}, nil
	})
	if err != nil || got != 3 || reads != 2 || writes != 2 {
		t.Fatalf("mutation did not retry with fresh state: version=%d reads=%d writes=%d err=%v", got, reads, writes, err)
	}
	if data["feed_map"] != "other=preserved,concurrent=retained,new=added" {
		t.Fatal("concurrent tenant map entry lost")
	}
	before := writes
	_, err = client.MutateSecretData(context.Background(), "secret/fixture", func(map[string]interface{}) (map[string]interface{}, error) { return nil, nil })
	if err != nil || writes != before {
		t.Fatal("no-op wrote a secret")
	}
}

// TestVaultMutationPreservesExistingKeysAndCASesOnCurrentVersion is the case the
// kalya feed custody write actually hits in production: an existing multi-key
// secret (secret/crea-map holds ~11 keys) gets one custody key added. The write
// must (i) preserve every existing key and (ii) compare-and-set on the version
// the read reported, not on 0. A regression to "replace" or "cas:0" would either
// destroy the other keys or fail on every existing secret.
func TestVaultMutationPreservesExistingKeysAndCASesOnCurrentVersion(t *testing.T) {
	const currentVersion = 5
	existing := map[string]interface{}{
		"internal_api_key":         "unrelated",
		"kalya_occupancy_feed_url": "stale",
		"other_tenant_secret":      "keep-me",
	}
	var sawCAS int
	var wrote map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": map[string]interface{}{"data": existing, "metadata": map[string]int{"version": currentVersion}}})
			return
		}
		var body struct {
			Data    map[string]interface{} `json:"data"`
			Options struct {
				CAS int `json:"cas"`
			} `json:"options"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		sawCAS = body.Options.CAS
		wrote = body.Data
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": map[string]int{"version": currentVersion + 1}})
	}))
	defer server.Close()
	client := &VaultClient{address: server.URL, token: "fixture-key", httpClient: server.Client(), enabled: true}

	version, err := client.MutateSecretData(context.Background(), "secret/crea-map", func(current map[string]interface{}) (map[string]interface{}, error) {
		return map[string]interface{}{"kalya_feed_custody_crea": "custody-value"}, nil
	})
	if err != nil {
		t.Fatalf("mutation failed: %v", err)
	}
	if version != currentVersion+1 {
		t.Fatalf("want version %d, got %d", currentVersion+1, version)
	}
	if sawCAS != currentVersion {
		t.Fatalf("want cas %d (the version the read reported), got %d", currentVersion, sawCAS)
	}
	for key, want := range existing {
		if wrote[key] != want {
			t.Fatalf("existing key %q was not preserved: got %v want %v", key, wrote[key], want)
		}
	}
	if wrote["kalya_feed_custody_crea"] != "custody-value" {
		t.Fatalf("custody key was not written: %v", wrote["kalya_feed_custody_crea"])
	}
}

// TestVaultMutationSurfacesNonCASRejection proves the masking is gone: a 400 that
// is NOT a stale CAS (here a cas-required mount rejecting the write) is returned
// as a VaultDiagnostic carrying Vault's own errors array, instead of being
// retried five times and buried under the generic "state changed" message.
func TestVaultMutationSurfacesNonCASRejection(t *testing.T) {
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": map[string]interface{}{"data": map[string]interface{}{"k": "v"}, "metadata": map[string]int{"version": 3}}})
			return
		}
		writes++
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"errors": []string{"check-and-set parameter required for this call"}})
	}))
	defer server.Close()
	client := &VaultClient{address: server.URL, token: "fixture-key", httpClient: server.Client(), enabled: true}

	_, err := client.MutateSecretData(context.Background(), "secret/fixture", func(map[string]interface{}) (map[string]interface{}, error) {
		return map[string]interface{}{"k": "v2"}, nil
	})
	if err == nil {
		t.Fatal("a non-CAS rejection must be surfaced, not swallowed")
	}
	var diag *VaultDiagnostic
	if !errors.As(err, &diag) {
		t.Fatalf("want a *VaultDiagnostic, got %T: %v", err, err)
	}
	if diag.Status != http.StatusBadRequest {
		t.Fatalf("want status 400, got %d", diag.Status)
	}
	if !strings.Contains(diag.Message, "check-and-set parameter required") {
		t.Fatalf("Vault diagnostic not surfaced: %q", diag.Message)
	}
	if writes != 1 {
		t.Fatalf("a deterministic rejection must not be retried; writes=%d", writes)
	}
}
