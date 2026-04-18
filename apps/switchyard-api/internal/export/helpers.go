package export

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// jsonMarshalIndent is a tiny shim so callers in service.go don't have to
// import encoding/json themselves for the one index.json write. Keeps the
// public surface of the package cleaner.
func jsonMarshalIndent(v interface{}) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	return b, nil
}

// hashSHA256 returns "sha256:" + hex of the bytes. Mirrors the format
// used by everything else in this package (Part.SHA256, ManifestEntry).
func hashSHA256(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
