package lockbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/secretsintake"
)

var ErrSecretInitializationConflict = errors.New("secret initialization conflict")
var ErrSecretInitializationOutcomeUnknown = errors.New("secret initialization outcome unknown")

// InitializeSecretData adds a wholly absent group to a KV v2 path with CAS.
// Any existing group key (even empty) refuses, unrelated keys are retained, and
// a concurrent write refuses without retry. No value is returned or logged.
func (v *VaultClient) InitializeSecretData(ctx context.Context, path string, updates map[string]interface{}) (int, error) {
	return v.initializeSecretData(ctx, path, updates, false)
}

// The typed backup wrapper shares this exact CAS implementation. Its responses
// are bounded before status/JSON handling; legacy groups keep their existing
// size compatibility (their unbounded-response debt is a separate change).
func (v *VaultClient) initializeSecretData(ctx context.Context, path string, updates map[string]interface{}, bounded bool) (int, error) {
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
	if bounded {
		isolated, closeTransport, err := v.backupIntakeClient()
		if err != nil {
			return 0, fmt.Errorf("backup intake transport unqualified")
		}
		defer closeTransport()
		client = *isolated
	}
	read, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, fmt.Errorf("create initialization read request")
	}
	v.setInitializationHeaders(read)
	read.Close = bounded
	resp, err := client.Do(read)
	if err != nil {
		return 0, fmt.Errorf("read initialization custody")
	}
	if bounded {
		raw, boundErr := readBoundedIntakeResponse(resp)
		if boundErr != nil {
			return 0, fmt.Errorf("read initialization custody")
		}
		defer clear(raw)
		resp.Body = io.NopCloser(bytes.NewReader(raw))
		if resp.StatusCode == http.StatusOK {
			parsed, err := decodeBackupCustodyJSON(raw)
			if err != nil {
				return 0, fmt.Errorf("invalid initialization custody snapshot")
			}
			data, _ := parsed["data"].(map[string]interface{})
			metadata, _ := data["metadata"].(map[string]interface{})
			if metadata["destroyed"] != false || metadata["deletion_time"] != "" {
				return 0, fmt.Errorf("invalid initialization custody snapshot")
			}
		}
	}
	var snapshot VaultSecretData
	switch resp.StatusCode {
	case http.StatusNotFound:
		// CAS 0 means create only. A deleted/destroyed existing version refuses.
		snapshot.Data.Data = make(map[string]interface{})
	case http.StatusOK:
		err = decodeInitializationJSON(resp.Body, &snapshot)
		if err != nil || snapshot.Data.Data == nil || snapshot.Data.Metadata.Version <= 0 || snapshot.Data.Metadata.Destroyed {
			_ = resp.Body.Close()
			return 0, fmt.Errorf("invalid initialization custody snapshot")
		}
	default:
		_ = resp.Body.Close()
		return 0, fmt.Errorf("initialization custody read refused")
	}
	_ = resp.Body.Close()
	if bounded {
		if snapshot.Data.Metadata.Version == math.MaxInt {
			return 0, fmt.Errorf("invalid initialization custody snapshot")
		}
		for key := range snapshot.Data.Data {
			normalized := secretsintake.NormalizeVaultSecretKey(key)
			if normalized == secretsintake.NormalizeVaultSecretKey(secretsintake.ArgocdBackupKey) || normalized == secretsintake.NormalizeVaultSecretKey(secretsintake.ArgocdBackupOriginKey) {
				return 0, ErrSecretInitializationConflict
			}
		}
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
	write.Close = bounded
	// Only the fixed backup wrapper retains this expected (not committed)
	// version after an ambiguous POST, permitting exact origin/version readback.
	// Legacy/staging callers keep their original zero-on-error semantics.
	unknownVersion := 0
	if bounded {
		unknownVersion = snapshot.Data.Metadata.Version + 1
	}
	resp, err = client.Do(write)
	if err != nil {
		return unknownVersion, ErrSecretInitializationOutcomeUnknown
	}
	if bounded {
		raw, boundErr := readBoundedIntakeResponse(resp)
		if boundErr != nil {
			return unknownVersion, ErrSecretInitializationOutcomeUnknown
		}
		defer clear(raw)
		resp.Body = io.NopCloser(bytes.NewReader(raw))
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if _, err := decodeBackupCustodyJSON(raw); err != nil {
				return unknownVersion, ErrSecretInitializationOutcomeUnknown
			}
		}
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
		return unknownVersion, ErrSecretInitializationOutcomeUnknown
	}
	var written vaultKVWriteResponse
	if err := decodeInitializationJSON(resp.Body, &written); err != nil || written.Data.Version != snapshot.Data.Metadata.Version+1 {
		return unknownVersion, ErrSecretInitializationOutcomeUnknown
	}
	return written.Data.Version, nil
}

// A complete snapshot is one JSON value. UseNumber keeps unrelated KV numbers
// exact when the whole map is encoded for CAS; float64 would silently round them.
func decodeInitializationJSON(body io.Reader, value interface{}) error {
	decoder := json.NewDecoder(body)
	decoder.UseNumber()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("invalid initialization JSON")
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("invalid initialization JSON")
	}
	return nil
}

func (v *VaultClient) setInitializationHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Vault-Token", v.token)
	if v.namespace != "" {
		req.Header.Set("X-Vault-Namespace", v.namespace)
	}
}
