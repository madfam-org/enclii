package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/lockbox"
	"github.com/stretchr/testify/require"
)

func TestSigningAuthorityIntakeUsesCreateOnlyCAS(t *testing.T) {
	for _, target := range []struct{ id, path, key string }{
		{"karafiel/hcm-signing-bindings", "/v1/secret/data/karafiel", "hcm_signing_bindings"},
		{"symbiosis-hcm/signing-bindings", "/v1/secret/data/symbiosis-hcm", "company_signers"},
	} {
		for _, state := range []string{"missing-path", "unrelated", "empty", "empty-map", "same-map", "different-map", "cas-400", "cas-409"} {
			t.Run(target.id+"/"+state, func(t *testing.T) {
				const supplied = `{"fixture-org":"fixture-human"}`
				reads, writes := 0, 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					require.Equal(t, target.path, r.URL.Path)
					if r.Method == http.MethodGet {
						reads++
						if state == "missing-path" {
							w.WriteHeader(http.StatusNotFound)
							return
						}
						data := map[string]interface{}{"unrelated": "preserved-fixture"}
						switch state {
						case "empty":
							data[target.key] = ""
						case "empty-map":
							data[target.key] = "{}"
						case "same-map":
							data[target.key] = supplied
						case "different-map":
							data[target.key] = `{"other-org":"other-human"}`
						}
						require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{"data": map[string]interface{}{"data": data, "metadata": map[string]int{"version": 6}}}))
						return
					}
					writes++
					require.Equal(t, http.MethodPost, r.Method)
					var payload struct {
						Data    map[string]interface{} `json:"data"`
						Options map[string]int         `json:"options"`
					}
					require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
					require.Equal(t, supplied, payload.Data[target.key])
					version := 7
					if state == "missing-path" {
						require.Zero(t, payload.Options["cas"])
						require.Len(t, payload.Data, 1)
						version = 1
					} else {
						require.Equal(t, 6, payload.Options["cas"])
						require.Equal(t, "preserved-fixture", payload.Data["unrelated"])
					}
					if state == "cas-400" || state == "cas-409" {
						status := http.StatusBadRequest
						if state == "cas-409" {
							status = http.StatusConflict
						}
						w.WriteHeader(status)
						return
					}
					require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{"data": map[string]int{"version": version}}))
				}))
				defer server.Close()
				h := &Handler{vaultClient: lockbox.NewVaultClient(&lockbox.VaultConfig{Address: server.URL, Enabled: true})}
				body, err := json.Marshal(secretIntakeSubmitRequest{Target: target.id, Values: map[string]string{target.key: supplied}, Reason: "synthetic create-only authority"})
				require.NoError(t, err)
				response := postIntake(t, h, string(body))
				require.Equal(t, 1, reads)
				if state == "missing-path" || state == "unrelated" {
					require.Equal(t, http.StatusAccepted, response.Code)
					require.Equal(t, 1, writes)
				} else {
					require.Equal(t, http.StatusConflict, response.Code)
					require.Contains(t, response.Body.String(), "inspect and review the existing binding")
					if state == "cas-400" || state == "cas-409" {
						require.Equal(t, 1, writes, "a competing write must not retry")
					} else {
						require.Zero(t, writes, "existing properties must never be overwritten")
					}
				}
				require.NotContains(t, response.Body.String(), "fixture-human")
				require.NotContains(t, response.Body.String(), "other-human")
			})
		}
	}
}

func TestSigningAuthorityIntakeNeverFallsBackToMerge(t *testing.T) {
	for _, target := range []struct{ id, key string }{
		{"karafiel/hcm-signing-bindings", "hcm_signing_bindings"},
		{"symbiosis-hcm/signing-bindings", "company_signers"},
	} {
		writer := &mergeOnlyWriter{}
		body, err := json.Marshal(secretIntakeSubmitRequest{Target: target.id, Values: map[string]string{target.key: "{}"}, Reason: "synthetic authority"})
		require.NoError(t, err)
		response := postIntake(t, &Handler{vaultClient: writer}, string(body))
		require.Equal(t, http.StatusServiceUnavailable, response.Code)
		require.False(t, writer.merged)
	}
}
