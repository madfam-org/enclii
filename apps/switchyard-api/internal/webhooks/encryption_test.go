package webhooks

import (
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	enc, err := NewEncryptor(key)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}

	for _, plaintext := range []string{
		"",
		"x",
		"whsec_abc123",
		"a much longer signing secret that fills more than a single AEAD block",
	} {
		blob, err := enc.EncryptString(plaintext)
		if err != nil {
			t.Fatalf("encrypt %q: %v", plaintext, err)
		}
		got, err := enc.DecryptString(blob)
		if err != nil {
			t.Fatalf("decrypt %q: %v", plaintext, err)
		}
		if got != plaintext {
			t.Fatalf("roundtrip mismatch: got %q, want %q", got, plaintext)
		}
	}
}

func TestEncrypt_ProducesUniqueCiphertext(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	enc, _ := NewEncryptor(key)

	a, _ := enc.EncryptString("same plaintext")
	b, _ := enc.EncryptString("same plaintext")
	if string(a) == string(b) {
		t.Fatalf("ciphertext collides — nonce not random?")
	}
}

func TestDecrypt_FailsOnTamper(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	enc, _ := NewEncryptor(key)

	blob, _ := enc.EncryptString("secret")
	blob[len(blob)-1] ^= 0xff // flip tag byte
	if _, err := enc.DecryptString(blob); err == nil {
		t.Fatalf("tampered ciphertext accepted")
	}
}

func TestNewEncryptor_RejectsWrongKeySize(t *testing.T) {
	for _, sz := range []int{0, 16, 31, 33, 64} {
		_, err := NewEncryptor(make([]byte, sz))
		if err == nil {
			t.Errorf("size %d accepted", sz)
		}
	}
}

func TestLoadMasterKey_ValidAndInvalid(t *testing.T) {
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	b64 := base64.StdEncoding.EncodeToString(raw)

	got, err := LoadMasterKey(b64)
	if err != nil {
		t.Fatalf("valid key rejected: %v", err)
	}
	if len(got) != 32 {
		t.Fatalf("want 32 bytes, got %d", len(got))
	}

	if _, err := LoadMasterKey(""); err == nil {
		t.Fatal("empty key accepted")
	}
	if _, err := LoadMasterKey("!not base64!"); err == nil {
		t.Fatal("invalid base64 accepted")
	}
	// 16-byte key base64-encoded — should fail the length check
	short := base64.StdEncoding.EncodeToString(make([]byte, 16))
	if _, err := LoadMasterKey(short); err == nil {
		t.Fatal("short key accepted")
	}
}
