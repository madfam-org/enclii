package webhooks

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// Encryption for webhook signing secrets at rest.
//
// Design notes:
//   - AES-256-GCM is chosen for FIPS-friendliness and stdlib support.
//   - The 32-byte master key is supplied via ENCLII_WEBHOOK_MASTER_KEY
//     (base64). When unset we fall back to a development-only derived
//     key so local tests pass; production startup should fail if the env
//     var is empty.
//   - Each ciphertext carries its own 12-byte nonce prepended to the
//     AEAD output.

// Encryptor holds the derived AEAD cipher. Construct via NewEncryptor.
type Encryptor struct {
	aead cipher.AEAD
}

// NewEncryptor builds an Encryptor from a 32-byte master key. The key
// is typically loaded from env via LoadMasterKey.
func NewEncryptor(masterKey []byte) (*Encryptor, error) {
	if len(masterKey) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes, got %d", len(masterKey))
	}
	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher.NewGCM: %w", err)
	}
	return &Encryptor{aead: gcm}, nil
}

// LoadMasterKey decodes a base64 32-byte key from the given string.
// Returns an error if the value is empty or wrong size.
func LoadMasterKey(b64 string) ([]byte, error) {
	if b64 == "" {
		return nil, errors.New("master key is empty (set ENCLII_WEBHOOK_MASTER_KEY)")
	}
	key, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("decode base64: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("master key must decode to 32 bytes, got %d", len(key))
	}
	return key, nil
}

// Encrypt seals plaintext with a fresh 96-bit nonce and returns
// nonce || ciphertext || tag.
func (e *Encryptor) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, e.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("read nonce: %w", err)
	}
	// Append in a single allocation: Seal writes ciphertext+tag into
	// the destination starting at the nonce slice so it comes out
	// contiguous.
	return e.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt opens a blob produced by Encrypt. Returns an error on any
// authentication failure.
func (e *Encryptor) Decrypt(blob []byte) ([]byte, error) {
	ns := e.aead.NonceSize()
	if len(blob) < ns {
		return nil, errors.New("ciphertext too short")
	}
	nonce, ct := blob[:ns], blob[ns:]
	return e.aead.Open(nil, nonce, ct, nil)
}

// EncryptString / DecryptString convenience wrappers for the common case
// of persisting a plaintext secret string.
func (e *Encryptor) EncryptString(s string) ([]byte, error) {
	return e.Encrypt([]byte(s))
}

func (e *Encryptor) DecryptString(blob []byte) (string, error) {
	b, err := e.Decrypt(blob)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
