package api

import "context"

// VaultSecretWriter is the narrow Vault surface required by audited ops.
type VaultSecretWriter interface {
	IsEnabled() bool
	MergeSecretData(ctx context.Context, path string, updates map[string]interface{}) (int, error)
}

// SetVaultClient wires the optional Vault writer used by audited secret
// backfill operations. Nil leaves Vault mutations disabled.
func (h *Handler) SetVaultClient(client VaultSecretWriter) {
	h.vaultClient = client
}
