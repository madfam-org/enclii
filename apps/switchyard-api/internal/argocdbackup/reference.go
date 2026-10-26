package argocdbackup

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// Reference contains only validated ciphertext metadata. Its object key is
// derived from the fixed backup namespace and authenticated context, never a
// caller-supplied URL, path, bucket, version, image or profile selector.
type Reference struct {
	context Context
	digest  [32]byte
	size    int64
}

func ReferenceFromReceipt(c Context, r PublicReceipt) (Reference, error) {
	if _, err := c.aad(); err != nil || r.Schema != "enclii-argocd-backup-preparation-v1" || r.Phase != "PREPARATION_ONLY" || r.NativeReady ||
		r.OperationID != c.OperationID || r.BackupID != c.BackupID || r.Bytes < 29 || r.Bytes > MaxCiphertextBytes || len(r.CiphertextSHA256) != 64 {
		return Reference{}, ErrHold
	}
	raw, err := hex.DecodeString(r.CiphertextSHA256)
	if err != nil || hex.EncodeToString(raw) != r.CiphertextSHA256 {
		return Reference{}, ErrHold
	}
	ref := Reference{context: c, size: int64(r.Bytes)}
	copy(ref.digest[:], raw)
	return ref, nil
}
func (s Sealed) Reference() (Reference, error) {
	r, err := Receipt(s)
	if err != nil {
		return Reference{}, ErrHold
	}
	return ReferenceFromReceipt(s.context, r)
}
func (r Reference) valid() bool {
	_, err := r.context.aad()
	return err == nil && r.size >= 29 && r.size <= MaxCiphertextBytes
}
func (r Reference) ObjectKey() (string, error) {
	if !r.valid() {
		return "", ErrHold
	}
	return "argocd-backups/v1/" + r.context.NamespaceUID + "/" + r.context.OperationID + "/" + r.context.BackupID + ".aes256gcm", nil
}
func (r Reference) Size() int64 {
	if !r.valid() {
		return 0
	}
	return r.size
}
func (r Reference) ChecksumBase64() string {
	if !r.valid() {
		return ""
	}
	return base64.StdEncoding.EncodeToString(r.digest[:])
}

// Matches compares exact bytes against a previously sealed ciphertext receipt.
// No observed-content digest, object body or private-value hash is returned.
func (r Reference) Matches(ciphertext []byte) bool {
	return r.valid() && int64(len(ciphertext)) == r.size && sha256.Sum256(ciphertext) == r.digest
}
