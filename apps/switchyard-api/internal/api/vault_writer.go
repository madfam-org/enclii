package api

import "context"

// VaultSecretWriter is the narrow Vault surface required by audited ops.
type VaultSecretWriter interface {
	IsEnabled() bool
	MergeSecretData(ctx context.Context, path string, updates map[string]interface{}) (int, error)
	// GetSecretData reads every key at a KV v2 path. Provisioners that must be
	// idempotent need to see what is already there before they mint anything
	// new; a missing path reads as an empty map rather than an error.
	GetSecretData(ctx context.Context, path string) (map[string]interface{}, error)
}

// Optional: existing writers/mocks do not gain an administrative requirement.
type vaultTrainingCapabilityProbe interface {
	ProbeTrainingCustodyCapabilities(ctx context.Context) (map[string][]string, error)
}

// Initialization is an optional, compare-and-set write surface. Typed staging
// profiles fail closed when a writer cannot preserve existing custody atomically.
type vaultSecretInitializer interface {
	InitializeSecretData(ctx context.Context, path string, updates map[string]interface{}) (int, error)
}

// SetVaultClient wires the optional Vault writer used by audited secret
// backfill operations. Nil leaves Vault mutations disabled.
func (h *Handler) SetVaultClient(client VaultSecretWriter) {
	h.vaultClient = client
}
