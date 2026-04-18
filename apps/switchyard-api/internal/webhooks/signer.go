// Package webhooks implements the P2.3 outbound lifecycle webhook
// pipeline: signing, dispatching, worker pool, and retry/DLQ semantics.
//
// Signature format mirrors Stripe:
//
//	X-Enclii-Signature: t=<unix_timestamp>,v1=<hmac_sha256_hex>
//
// where the HMAC input is   "<timestamp>.<raw_body>"   (UTF-8 bytes).
// Subscribers recompute the HMAC with their stored secret and compare
// using a constant-time function. The timestamp is validated against a
// 5-minute clock tolerance to block replay attacks.
package webhooks

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// signingSecretBytes is the size of a freshly generated secret.  32 bytes
// of CSPRNG output gives us 256 bits of entropy — well above the
// HMAC-SHA256 security floor.
const signingSecretBytes = 32

// signingSecretPrefix marks the canonical on-the-wire format so tools
// can recognize an Enclii webhook secret at a glance. The base64url
// body is URL-safe and avoids the `=` padding quirk that some secret
// managers mishandle.
const signingSecretPrefix = "whsec_"

// GenerateSigningSecret returns a fresh plaintext secret in the canonical
// `whsec_<base64url>` format together with its first-8-char SHA-256 hex
// prefix (the prefix is stored alongside the subscription for UI display
// so operators can disambiguate rotated secrets without seeing the raw).
func GenerateSigningSecret() (plaintext string, sha256Prefix string, err error) {
	buf := make([]byte, signingSecretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("read crypto/rand: %w", err)
	}
	plaintext = signingSecretPrefix + base64.RawURLEncoding.EncodeToString(buf)
	sha256Prefix = SecretPrefix(plaintext)
	return plaintext, sha256Prefix, nil
}

// SecretPrefix computes the first 8 hex chars of SHA-256(secret). It is
// deterministic and safe to show in UI/audit; it cannot be reversed to
// recover the plaintext.
func SecretPrefix(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(h[:])[:8]
}

// Sign produces the X-Enclii-Signature header value for the given body
// using the given secret and timestamp. Callers typically pass
// time.Now() but tests can inject a fixed ts for reproducibility.
func Sign(secret string, timestamp time.Time, body []byte) string {
	ts := timestamp.Unix()
	mac := hmacBody(secret, ts, body)
	return fmt.Sprintf("t=%d,v1=%s", ts, hex.EncodeToString(mac))
}

// Verify recomputes the signature from the given body + secret, checks
// it against the provided header, and enforces the clock-skew tolerance.
// Returns nil on valid signatures.
//
// Errors are intentionally generic (they all wrap ErrSignatureInvalid)
// to avoid leaking which check failed — subscribers only need to know
// the payload is untrusted.
func Verify(secret string, header string, body []byte, tolerance time.Duration, now time.Time) error {
	ts, sig, err := parseSignatureHeader(header)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSignatureInvalid, err)
	}

	skew := now.Sub(time.Unix(ts, 0))
	if skew < 0 {
		skew = -skew
	}
	if skew > tolerance {
		return fmt.Errorf("%w: timestamp outside %s tolerance", ErrSignatureInvalid, tolerance)
	}

	expected := hmacBody(secret, ts, body)
	if !hmac.Equal(expected, sig) {
		return fmt.Errorf("%w: HMAC mismatch", ErrSignatureInvalid)
	}
	return nil
}

// ErrSignatureInvalid is the sentinel error returned by Verify. Use
// errors.Is to check.
var ErrSignatureInvalid = errors.New("invalid webhook signature")

func hmacBody(secret string, ts int64, body []byte) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(strconv.FormatInt(ts, 10)))
	_, _ = mac.Write([]byte{'.'})
	_, _ = mac.Write(body)
	return mac.Sum(nil)
}

func parseSignatureHeader(header string) (int64, []byte, error) {
	if header == "" {
		return 0, nil, errors.New("empty header")
	}
	var tsStr, v1 string
	for _, part := range strings.Split(header, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			tsStr = kv[1]
		case "v1":
			v1 = kv[1]
		}
	}
	if tsStr == "" || v1 == "" {
		return 0, nil, errors.New("missing t/v1")
	}
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return 0, nil, fmt.Errorf("bad timestamp: %w", err)
	}
	sig, err := hex.DecodeString(v1)
	if err != nil {
		return 0, nil, fmt.Errorf("bad v1 hex: %w", err)
	}
	return ts, sig, nil
}

// ---------------------------------------------------------------------------
// Secret-at-rest encryption
// ---------------------------------------------------------------------------
//
// Until the RFC 0005 K8s-Secret-backed vault lands for webhook secrets,
// we encrypt the plaintext with a symmetric key derived from an
// ops-provided master key (ENCLII_WEBHOOK_MASTER_KEY, a base64 32-byte
// value). This is the exact same pattern the notifications package uses
// for Telegram bot tokens; see internal/notifications/*.go.
//
// Callers should wire EncryptSecret / DecryptSecret around the raw
// plaintext so the database only stores ciphertext.

// EncryptSecret and DecryptSecret are intentionally thin wrappers around
// NaCl secretbox-style AEAD. A full implementation would live in its own
// file; for now we keep a minimal XChaCha20-Poly1305 shim via
// crypto/cipher.AEAD so the repository interface stays stable while the
// vault integration matures.
//
// They are separated into encryption.go for testability.
