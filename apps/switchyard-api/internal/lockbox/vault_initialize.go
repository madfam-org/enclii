package lockbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

var ErrSecretInitializationConflict = errors.New("secret initialization conflict")
var ErrSecretInitializationOutcomeUnknown = errors.New("secret initialization outcome unknown")

// InitializeSecretData adds a wholly absent group to a KV v2 path with CAS.
// Any existing group key (even empty) refuses, unrelated keys are retained, and
// a concurrent write refuses without retry. No value is returned or logged.
func (v *VaultClient) InitializeSecretData(ctx context.Context, path string, updates map[string]interface{}) (int, error) {
	if !v.enabled || strings.TrimSpace(path) == "" || len(updates) == 0 {
		return 0, fmt.Errorf("invalid secret initialization request")
	}
	for key := range updates {
		if strings.TrimSpace(key) == "" {
			return 0, fmt.Errorf("empty secret initialization key")
		}
	}
	endpoint := fmt.Sprintf("%s/v1/%s", v.address, vaultKVv2DataPath(path))
	// Do not replay a credential-bearing POST at a redirect destination. A
	// per-operation copy preserves the shared client's policy and concurrency.
	client := *v.httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	read, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, fmt.Errorf("create initialization read request")
	}
	v.setInitializationHeaders(read)
	resp, err := client.Do(read)
	if err != nil {
		return 0, fmt.Errorf("read initialization custody")
	}
	var snapshot VaultSecretData
	switch resp.StatusCode {
	case http.StatusNotFound:
		// CAS 0 means create only. A deleted/destroyed existing version refuses.
	case http.StatusOK:
		err = json.NewDecoder(resp.Body).Decode(&snapshot)
		if err != nil || snapshot.Data.Metadata.Version <= 0 || snapshot.Data.Metadata.Destroyed {
			_ = resp.Body.Close()
			return 0, fmt.Errorf("invalid initialization custody snapshot")
		}
	default:
		_ = resp.Body.Close()
		return 0, fmt.Errorf("initialization custody read refused")
	}
	_ = resp.Body.Close()
	if snapshot.Data.Data == nil {
		snapshot.Data.Data = make(map[string]interface{})
	}
	for key, value := range updates {
		if _, exists := snapshot.Data.Data[key]; exists {
			return 0, ErrSecretInitializationConflict
		}
		snapshot.Data.Data[key] = value
	}
	payload, err := json.Marshal(map[string]interface{}{
		"data": snapshot.Data.Data, "options": map[string]int{"cas": snapshot.Data.Metadata.Version},
	})
	if err != nil {
		return 0, fmt.Errorf("encode initialization request")
	}
	write, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, fmt.Errorf("create initialization write request")
	}
	v.setInitializationHeaders(write)
	resp, err = client.Do(write)
	if err != nil {
		return 0, ErrSecretInitializationOutcomeUnknown
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusConflict {
		return 0, ErrSecretInitializationConflict
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return 0, fmt.Errorf("initialization write refused")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Gateway/server failures may arrive after Vault committed the CAS.
		return 0, ErrSecretInitializationOutcomeUnknown
	}
	var written vaultKVWriteResponse
	if err := json.NewDecoder(resp.Body).Decode(&written); err != nil || written.Data.Version != snapshot.Data.Metadata.Version+1 {
		return 0, ErrSecretInitializationOutcomeUnknown
	}
	return written.Data.Version, nil
}

func (v *VaultClient) setInitializationHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Vault-Token", v.token)
	if v.namespace != "" {
		req.Header.Set("X-Vault-Namespace", v.namespace)
	}
}
