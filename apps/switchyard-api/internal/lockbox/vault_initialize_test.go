package lockbox

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInitializeSecretDataCASAndPreservesUnrelatedKeys(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing path", true: "existing path"}[exists], func(t *testing.T) {
			reads, writes := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/v1/secret/data/stage", r.URL.Path)
				require.Equal(t, "fixture-token", r.Header.Get("X-Vault-Token"))
				require.Equal(t, "fixture-namespace", r.Header.Get("X-Vault-Namespace"))
				if r.Method == http.MethodGet {
					reads++
					if !exists {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					_, _ = w.Write([]byte(`{"data":{"data":{"database_url":"preserved-fixture"},"metadata":{"version":6}}}`))
					return
				}
				writes++
				require.Equal(t, http.MethodPost, r.Method)
				var payload struct {
					Data    map[string]interface{} `json:"data"`
					Options map[string]int         `json:"options"`
				}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
				require.Equal(t, "fresh-fixture", payload.Data["redis_password"])
				if exists {
					require.Equal(t, 6, payload.Options["cas"])
					require.Equal(t, "preserved-fixture", payload.Data["database_url"])
				} else {
					require.Equal(t, 0, payload.Options["cas"])
					require.Len(t, payload.Data, 1)
				}
				if exists {
					_, _ = w.Write([]byte(`{"data":{"version":7}}`))
				} else {
					_, _ = w.Write([]byte(`{"data":{"version":1}}`))
				}
			}))
			defer server.Close()
			client := NewVaultClient(&VaultConfig{Address: server.URL, Token: "fixture-token", Namespace: "fixture-namespace", Enabled: true})
			version, err := client.InitializeSecretData(t.Context(), "secret/stage", map[string]interface{}{"redis_password": "fresh-fixture"})
			require.NoError(t, err)
			if exists {
				require.Equal(t, 7, version)
			} else {
				require.Equal(t, 1, version)
			}
			require.Equal(t, 1, reads)
			require.Equal(t, 1, writes)
		})
	}
}

func TestInitializeSecretDataUncertainWriteStatusesDoNotRetry(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, http.StatusTemporaryRedirect} {
		writes := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			writes++
			w.Header().Set("Location", "/must-not-follow")
			w.WriteHeader(status)
			_, _ = w.Write([]byte("sensitive-upstream-body"))
		}))
		client := NewVaultClient(&VaultConfig{Address: server.URL, Enabled: true})
		_, err := client.InitializeSecretData(t.Context(), "secret/stage", map[string]interface{}{"key": "fresh"})
		require.ErrorIs(t, err, ErrSecretInitializationOutcomeUnknown)
		require.NotContains(t, err.Error(), "sensitive-upstream-body")
		require.Equal(t, 1, writes)
		server.Close()
	}
}

func TestInitializeSecretDataRefusesExistingPartialGroup(t *testing.T) {
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes++
		}
		_, _ = w.Write([]byte(`{"data":{"data":{"redis_password":""},"metadata":{"version":4}}}`))
	}))
	defer server.Close()
	client := NewVaultClient(&VaultConfig{Address: server.URL, Enabled: true})
	_, err := client.InitializeSecretData(t.Context(), "secret/stage", map[string]interface{}{"redis_password": "new", "redis_url": "new"})
	require.ErrorIs(t, err, ErrSecretInitializationConflict)
	require.Equal(t, 0, writes)
}

func TestInitializeSecretDataNoRetryAndNoVaultBodyDisclosure(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusConflict, http.StatusForbidden} {
		reads, writes := 0, 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				reads++
				_, _ = w.Write([]byte(`{"data":{"data":{},"metadata":{"version":8}}}`))
				return
			}
			writes++
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"errors":["sensitive-vault-response"]}`))
		}))
		client := NewVaultClient(&VaultConfig{Address: server.URL, Enabled: true})
		_, err := client.InitializeSecretData(t.Context(), "secret/stage", map[string]interface{}{"key": "fresh"})
		require.Error(t, err)
		require.NotContains(t, err.Error(), "sensitive-vault-response")
		if status != http.StatusForbidden {
			require.ErrorIs(t, err, ErrSecretInitializationConflict)
		}
		require.Equal(t, 1, reads)
		require.Equal(t, 1, writes)
		server.Close()
	}
}

func TestInitializeSecretDataInvalidSnapshotNeverWrites(t *testing.T) {
	for _, snapshot := range []string{
		`{`, `{"data":{"data":{}}}`,
		`{"data":{"metadata":{"version":2}}}`,
		`{"data":{"data":null,"metadata":{"version":2}}}`,
		`{"data":{"data":{},"metadata":{"version":2,"destroyed":true}}}`,
		`{"data":{"data":{},"metadata":{"version":2}}} trailing`,
		`{"data":{"data":{},"metadata":{"version":2}}} {}`,
		`{"data":{"data":{},"metadata":{"version":2}}} {`,
	} {
		writes := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				writes++
			}
			_, _ = w.Write([]byte(snapshot))
		}))
		client := NewVaultClient(&VaultConfig{Address: server.URL, Enabled: true})
		_, err := client.InitializeSecretData(t.Context(), "secret/stage", map[string]interface{}{"key": "fresh"})
		require.Error(t, err)
		require.Equal(t, 0, writes)
		server.Close()
	}
}

func TestInitializeSecretDataPreservesExactNumbersAndAcceptsExplicitEmptyObject(t *testing.T) {
	for _, data := range []string{`{}`, `{"counter":9007199254740993,"nested":{"nonce":9223372036854775807}}`} {
		reads, writes := 0, 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				reads++
				_, _ = w.Write([]byte(`{"data":{"data":` + data + `,"metadata":{"version":6}}}`))
				return
			}
			writes++
			var payload struct {
				Data    map[string]interface{} `json:"data"`
				Options map[string]int         `json:"options"`
			}
			decoder := json.NewDecoder(r.Body)
			decoder.UseNumber()
			require.NoError(t, decoder.Decode(&payload))
			require.Equal(t, 6, payload.Options["cas"])
			require.Equal(t, "fresh", payload.Data["new_key"])
			if data == `{}` {
				require.Len(t, payload.Data, 1)
			} else {
				require.Equal(t, json.Number("9007199254740993"), payload.Data["counter"])
				require.Equal(t, json.Number("9223372036854775807"), payload.Data["nested"].(map[string]interface{})["nonce"])
			}
			_, _ = w.Write([]byte("{\"data\":{\"version\":7}}\n  "))
		}))
		client := NewVaultClient(&VaultConfig{Address: server.URL, Enabled: true})
		version, err := client.InitializeSecretData(t.Context(), "secret/stage", map[string]interface{}{"new_key": "fresh"})
		require.NoError(t, err)
		require.Equal(t, 7, version)
		require.Equal(t, 1, reads)
		require.Equal(t, 1, writes)
		server.Close()
	}
}

func TestInitializeSecretDataTrailingWriteReceiptIsUnknownWithoutRetry(t *testing.T) {
	for _, receipt := range []string{`{"data":{"version":1}} trailing`, `{"data":{"version":1}} {}`, `{"data":{"version":1}} {`} {
		writes := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			writes++
			_, _ = w.Write([]byte(receipt))
		}))
		client := NewVaultClient(&VaultConfig{Address: server.URL, Enabled: true})
		_, err := client.InitializeSecretData(t.Context(), "secret/stage", map[string]interface{}{"new_key": "fresh"})
		require.ErrorIs(t, err, ErrSecretInitializationOutcomeUnknown)
		require.Equal(t, 1, writes)
		server.Close()
	}
}

func TestInitializeSecretDataLostWriteReceiptIsUnknownAndNotRetried(t *testing.T) {
	reads, writes := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			reads++
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writes++
		// The write may have succeeded, but the body cannot establish its version.
		_, _ = w.Write([]byte(`{`))
	}))
	defer server.Close()
	client := NewVaultClient(&VaultConfig{Address: server.URL, Enabled: true})
	_, err := client.InitializeSecretData(t.Context(), "secret/stage", map[string]interface{}{"key": "fresh"})
	require.ErrorIs(t, err, ErrSecretInitializationOutcomeUnknown)
	require.Equal(t, 1, reads)
	require.Equal(t, 1, writes)
}
