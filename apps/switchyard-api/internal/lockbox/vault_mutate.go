package lockbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// VaultDiagnostic is a Vault-originated error message that has been proven safe
// to show an operator: it is built only from Vault's own `errors`/`warnings`
// arrays (fixed strings such as "check-and-set parameter did not match the
// current version") plus a status code, never from the request payload. Callers
// that mutate credential-bearing paths can therefore surface a VaultDiagnostic
// verbatim while keeping any other, untrusted error opaque.
type VaultDiagnostic struct {
	Status  int
	Message string
}

func (e *VaultDiagnostic) Error() string {
	return fmt.Sprintf("Vault write rejected (status %d): %s", e.Status, e.Message)
}

// MutateSecretData uses KV-v2 compare-and-set. The callback is reevaluated after
// a competing write; it must have no external side effects. Returning nil means
// no change. This preserves other tenant map entries under concurrent updates.
func (v *VaultClient) MutateSecretData(ctx context.Context, path string, update func(map[string]interface{}) (map[string]interface{}, error)) (int, error) {
	if !v.enabled || strings.TrimSpace(path) == "" {
		return 0, fmt.Errorf("Vault mutation is not configured")
	}
	endpoint := fmt.Sprintf("%s/v1/%s", v.address, vaultKVv2DataPath(path))
	lastConflict := ""
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
			// Vault answers a stale compare-and-set with 400 (and 409 on some
			// versions); that is the one case we retry with a fresh read. A 400
			// for any other reason (cas-required with no cas, a mount that
			// rejects the request, a policy denial surfaced as 400) is
			// deterministic: retrying it four more times only buries the real
			// cause under the generic "state changed" exhaustion message.
			vaultErr := decodeVaultErrors(response.Body)
			_ = response.Body.Close()
			if isRetryableCASConflict(vaultErr) {
				lastConflict = vaultErr
				continue
			}
			// Surface Vault's own diagnostic. Its `errors` array carries a fixed
			// message, never an echo of the submitted `data`, so this cannot leak
			// the credential the payload was carrying.
			return 0, &VaultDiagnostic{Status: response.StatusCode, Message: vaultErr}
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			vaultErr := decodeVaultErrors(response.Body)
			_ = response.Body.Close()
			return 0, &VaultDiagnostic{Status: response.StatusCode, Message: vaultErr}
		}
		var written vaultKVWriteResponse
		err = json.NewDecoder(response.Body).Decode(&written)
		_ = response.Body.Close()
		if err != nil {
			return 0, fmt.Errorf("Vault write acknowledgement unavailable")
		}
		return written.Data.Version, nil
	}
	if lastConflict != "" {
		return 0, fmt.Errorf("Vault compare-and-set kept losing to a concurrent writer across all mutation attempts (%s); retry safely", lastConflict)
	}
	return 0, fmt.Errorf("Vault state changed during all mutation attempts; retry safely")
}

// decodeVaultErrors extracts Vault's structured `errors` array from a response
// body and joins it into a single line. Vault populates this array with its own
// fixed diagnostic strings (for example "check-and-set parameter did not match
// the current version"); it does not echo the request payload back, so the
// result is safe to put in an operator-facing error even when the failed write
// carried a credential. A body Vault did not shape as `{"errors":[...]}` is
// reported only as its length, never its bytes, so an unexpected upstream (a
// proxy error page, say) still cannot leak whatever it happened to contain.
func decodeVaultErrors(body io.Reader) string {
	raw, err := io.ReadAll(io.LimitReader(body, 1<<16))
	if err != nil || len(raw) == 0 {
		return "no Vault diagnostic returned"
	}
	var parsed struct {
		Errors   []string `json:"errors"`
		Warnings []string `json:"warnings"`
	}
	if json.Unmarshal(raw, &parsed) != nil {
		return fmt.Sprintf("unparseable Vault response (%d bytes)", len(raw))
	}
	messages := append(parsed.Errors, parsed.Warnings...)
	if len(messages) == 0 {
		return "Vault returned no error detail"
	}
	return strings.Join(messages, "; ")
}

// isRetryableCASConflict decides whether a 400/409 is the stale compare-and-set
// case (retry with a fresh read) or a deterministic rejection (surface it).
//
// Real Vault answers a stale CAS with an explicit "check-and-set ..." message,
// which is the unambiguous retry signal. It is also treated as retryable when
// Vault returned no diagnostic detail at all: a bare 400 with an empty body is
// almost always a lost CAS race, and the previous behaviour — retry every 400
// blindly — relied on that. What is NOT retried is a 400 that carries a
// specific, non-CAS message (a cas-required mount rejecting a missing cas, a
// mount misconfiguration, a policy denial surfaced as 400): those are
// deterministic, so retrying only buries the cause under the generic exhaustion
// error. Surfacing them is the whole point of this change.
func isRetryableCASConflict(vaultErr string) bool {
	lower := strings.ToLower(vaultErr)
	// The stale-version message is the retry signal. "check-and-set parameter
	// required for this call" (a cas-required mount rejecting the write) also
	// contains "check-and-set" but is deterministic, so match the version phrase
	// specifically rather than the whole family.
	if strings.Contains(lower, "check-and-set") && strings.Contains(lower, "did not match") {
		return true
	}
	switch vaultErr {
	case "no Vault diagnostic returned", "Vault returned no error detail":
		return true
	}
	return false
}
