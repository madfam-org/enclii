package lockbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// MutateSecretData uses KV-v2 compare-and-set. The callback is reevaluated after
// a competing write; it must have no external side effects. Returning nil means
// no change. This preserves other tenant map entries under concurrent updates.
func (v *VaultClient) MutateSecretData(ctx context.Context, path string, update func(map[string]interface{}) (map[string]interface{}, error)) (int, error) {
	if !v.enabled || strings.TrimSpace(path) == "" {
		return 0, fmt.Errorf("Vault mutation is not configured")
	}
	endpoint := fmt.Sprintf("%s/v1/%s", v.address, vaultKVv2DataPath(path))
	for attempt := 0; attempt < 5; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return 0, err
		}
		request.Header.Set("X-Vault-Token", v.token)
		if v.namespace != "" {
			request.Header.Set("X-Vault-Namespace", v.namespace)
		}
		response, err := v.httpClient.Do(request)
		if err != nil {
			return 0, fmt.Errorf("Vault read unavailable")
		}
		var current VaultSecretData
		if response.StatusCode == http.StatusOK {
			err = json.NewDecoder(response.Body).Decode(&current)
		} else if response.StatusCode != http.StatusNotFound {
			err = fmt.Errorf("Vault read returned status %d", response.StatusCode)
		}
		_ = response.Body.Close()
		if err != nil {
			return 0, fmt.Errorf("Vault state could not be read")
		}
		data := current.Data.Data
		if data == nil {
			data = map[string]interface{}{}
		}
		updates, err := update(data)
		if err != nil {
			return 0, err
		}
		if updates == nil {
			return current.Data.Metadata.Version, nil
		}
		for key, value := range updates {
			data[key] = value
		}
		payload, err := json.Marshal(map[string]interface{}{"data": data, "options": map[string]int{"cas": current.Data.Metadata.Version}})
		if err != nil {
			return 0, fmt.Errorf("Vault mutation could not be encoded")
		}
		request, err = http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return 0, err
		}
		request.Header.Set("X-Vault-Token", v.token)
		request.Header.Set("Content-Type", "application/json")
		if v.namespace != "" {
			request.Header.Set("X-Vault-Namespace", v.namespace)
		}
		response, err = v.httpClient.Do(request)
		if err != nil {
			return 0, fmt.Errorf("Vault write unavailable")
		}
		if response.StatusCode == http.StatusBadRequest || response.StatusCode == http.StatusConflict {
			// Vault uses 400 for a stale CAS. Do not expose the error body: it is
			// adjacent to credential material. Other 400s exhaust the bounded retry.
			_ = response.Body.Close()
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			_ = response.Body.Close()
			return 0, fmt.Errorf("Vault write returned status %d", response.StatusCode)
		}
		var written vaultKVWriteResponse
		err = json.NewDecoder(response.Body).Decode(&written)
		_ = response.Body.Close()
		if err != nil {
			return 0, fmt.Errorf("Vault write acknowledgement unavailable")
		}
		return written.Data.Version, nil
	}
	return 0, fmt.Errorf("Vault state changed during all mutation attempts; retry safely")
}
