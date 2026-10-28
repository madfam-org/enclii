package releaseidentity

import (
	"encoding/base64"
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	"github.com/go-jose/go-jose/v4"
)

// CVE-2026-116587: oversized attacker-controlled RSA JWKs must be rejected
// before private-key validation. Synthetic integers avoid expensive key work.
func TestJWKParsingBoundsRSAModulus(t *testing.T) {
	for _, tc := range []struct {
		name    string
		bits    uint
		private bool
		reject  bool
	}{
		{"public at supported limit", 4096, false, false},
		{"oversized public", 4097, false, true},
		{"oversized private before validation", 4097, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := new(big.Int).Lsh(big.NewInt(1), tc.bits-1)
			n.Add(n, big.NewInt(1))
			jwk := map[string]string{"kty": "RSA", "n": base64.RawURLEncoding.EncodeToString(n.Bytes()), "e": "AQAB"}
			if tc.private {
				jwk["d"], jwk["p"], jwk["q"] = "AQ", "EQ", "Ew"
			}
			b, err := json.Marshal(jwk)
			if err != nil {
				t.Fatal(err)
			}
			var key jose.JSONWebKey
			err = json.Unmarshal(b, &key)
			if tc.reject {
				if err == nil || !strings.Contains(err.Error(), "modulus too large") {
					t.Fatalf("RSA modulus must be rejected before key validation: %v", err)
				}
			} else if err != nil {
				t.Fatalf("supported RSA public JWK refused: %v", err)
			}
		})
	}
}
