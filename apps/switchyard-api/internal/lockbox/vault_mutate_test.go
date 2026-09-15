package lockbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
